package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const androidToken = "registration:token_for_android_12345"

func androidRequest(notice, sealed string) []byte {
	asked, _ := json.Marshal(pushRequest{
		Token: hex.EncodeToString([]byte(fcmTokenPrefix + androidToken)), Notice: notice, Sealed: sealed,
	})
	return asked
}

func firebase(t *testing.T) (*fcmPusher, chan map[string]any, *atomic.Int32) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	calls := make(chan map[string]any, 8)
	grants := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			grants.Add(1)
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Errorf("authorization request: %s %s", r.Method, r.Header.Get("Content-Type"))
			}
			body, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(body))
			parts := strings.Split(form.Get("assertion"), ".")
			if len(parts) != 3 {
				t.Error("no signed assertion")
			} else {
				unsigned := parts[0] + "." + parts[1]
				digest := sha256.Sum256([]byte(unsigned))
				signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
				if rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature) != nil {
					t.Error("assertion signature is invalid")
				}
				claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
				if !strings.Contains(string(claims), fcmScope) || !strings.Contains(string(claims), serverURL(r)+"/token") {
					t.Error("assertion has wrong scope or audience")
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"access_token":"short-lived-access","expires_in":3600}`))
		case "/v1/projects/farol-android/messages:send":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer short-lived-access" {
				t.Error("FCM request is not authorized")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			calls <- body
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return &fcmPusher{
		email: "relay@farol-android.iam.gserviceaccount.com", key: key,
		client: server.Client(), tokenURL: server.URL + "/token",
		endpoint: server.URL + "/v1/projects/farol-android/messages:send",
	}, calls, grants
}

func serverURL(r *http.Request) string { return "http://" + r.Host }

func TestAndroidPushUsesFCMDataAndKeepsAPNsSeparate(t *testing.T) {
	fcm, calls, grants := firebase(t)
	apns, appleCalls := apple(t)
	push := &pushers{apns: apns, fcm: fcm}
	for _, notice := range []string{"waiting", "done"} {
		if err := push.send(context.Background(), androidRequest(notice, "AQID+/8="), someRoom); err != nil {
			t.Fatal(err)
		}
		message := (<-calls)["message"].(map[string]any)
		data := message["data"].(map[string]any)
		if message["token"] != androidToken || data["notice"] != notice || data["sealed"] != "AQID+/8=" || data["host"] != someRoom || message["notification"] != nil {
			t.Fatalf("wrong FCM message: %#v", message)
		}
		priority := "NORMAL"
		if notice == "waiting" {
			priority = "HIGH"
		}
		if message["android"].(map[string]any)["priority"] != priority {
			t.Fatalf("wrong priority: %#v", message)
		}
	}
	if grants.Load() != 1 || len(appleCalls) != 0 {
		t.Fatalf("OAuth grants %d, Apple calls %d", grants.Load(), len(appleCalls))
	}
	if err := push.send(context.Background(), request("waiting", ""), someRoom); err != nil {
		t.Fatal(err)
	}
	if len(appleCalls) != 1 || len(calls) != 0 {
		t.Fatal("APNs push did not stay on Apple's path")
	}
}

func TestAndroidPushRefusesUnknownContentAndLiveActivities(t *testing.T) {
	fcm, calls, grants := firebase(t)
	push := &pushers{fcm: fcm}
	for _, asked := range []pushRequest{
		{Token: hex.EncodeToString([]byte(fcmTokenPrefix + androidToken)), Notice: "buy now"},
		{Token: hex.EncodeToString([]byte(fcmTokenPrefix + androidToken)), Activity: "start"},
		{Token: hex.EncodeToString([]byte(fcmTokenPrefix + androidToken)), Notice: "waiting", Sealed: "not base64!"},
		{Token: hex.EncodeToString([]byte(fcmTokenPrefix + "short")), Notice: "waiting"},
	} {
		payload, _ := json.Marshal(asked)
		if push.send(context.Background(), payload, someRoom) == nil {
			t.Fatalf("accepted invalid push: %#v", asked)
		}
	}
	if len(calls) != 0 || grants.Load() != 0 {
		t.Fatal("invalid push reached Google")
	}
}

func TestAndroidPushWithoutFirebaseDoesNotAffectAPNs(t *testing.T) {
	apns, calls := apple(t)
	push := &pushers{apns: apns}
	if err := push.send(context.Background(), request("done", ""), someRoom); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatal("Apple was not called")
	}
	err := push.send(context.Background(), androidRequest("waiting", ""), someRoom)
	if err == nil || strings.Contains(err.Error(), androidToken) || len(calls) != 1 {
		t.Fatalf("Android without Firebase: %v", err)
	}
}

func TestAndroidPushesDoNotUseTheAppleRateLimit(t *testing.T) {
	fcm, fcmCalls, _ := firebase(t)
	apns, appleCalls := apple(t)
	relay := newRelay(&pushers{apns: apns, fcm: fcm})
	server := httptest.NewServer(relay.handler())
	t.Cleanup(server.Close)
	mac := dial(t, "ws"+strings.TrimPrefix(server.URL, "http"), "mac")
	for i := 0; i < pushesPerMinute; i++ {
		mac.send(append([]byte{framePush}, androidRequest("waiting", "")...))
	}
	mac.send(append([]byte{framePush}, request("waiting", "")...))
	select {
	case <-appleCalls:
	case <-time.After(2 * time.Second):
		t.Fatal("Android pushes exhausted Apple's notification limit")
	}
	// The relay sends asynchronously. Consume every Firebase request before the
	// fake server closes, or its bounded call channel can block HTTP handlers.
	allCalls := time.After(5 * time.Second)
	for i := 0; i < pushesPerMinute; i++ {
		select {
		case <-fcmCalls:
		case <-allCalls:
			t.Fatalf("only %d of %d Firebase pushes arrived", i, pushesPerMinute)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for relay.sent.Load() != int64(pushesPerMinute+1) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := relay.sent.Load(); got != int64(pushesPerMinute+1) {
		t.Errorf("only %d of %d pushes completed", got, pushesPerMinute+1)
	}
}

func TestFirebaseFailureNeverLogsRegistrationToken(t *testing.T) {
	fcm, _, _ := firebase(t)
	fcm.endpoint = "http://127.0.0.1:1/" + androidToken
	err := (&pushers{fcm: fcm}).send(context.Background(), androidRequest("waiting", ""), someRoom)
	if err == nil || strings.Contains(err.Error(), androidToken) {
		t.Fatalf("error exposed the token: %v", err)
	}
}
