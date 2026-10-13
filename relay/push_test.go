package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const someToken = "ab01ff00ab01ff00ab01ff00ab01ff00ab01ff00ab01ff00ab01ff00ab01ff00"

type asked struct {
	path, bearer, topic, body string
}

// apple stands in for Apple's servers, and records what it was asked.
func apple(t *testing.T) (*pusher, chan asked) {
	t.Helper()
	calls := make(chan asked, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls <- asked{r.URL.Path, r.Header.Get("authorization"), r.Header.Get("apns-topic"), string(body)}
	}))
	t.Cleanup(server.Close)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &pusher{
		key: key, keyID: "KEY", teamID: "TEAM", topic: "sh.farol.ios",
		production: server.URL + "/production", sandbox: server.URL + "/sandbox", client: server.Client(),
	}, calls
}

func request(notice, sandbox string) []byte {
	payload, _ := json.Marshal(pushRequest{Token: someToken, Notice: notice, Sandbox: sandbox})
	return payload
}

func TestAsksAppleToNotifyThePhone(t *testing.T) {
	p, calls := apple(t)
	if err := p.send(context.Background(), request("waiting", "")); err != nil {
		t.Fatal(err)
	}
	got := <-calls
	if got.path != "/production/3/device/"+someToken || got.topic != "sh.farol.ios" {
		t.Fatalf("asked %+v", got)
	}
	if !strings.Contains(got.body, "An agent is waiting for you.") {
		t.Fatalf("said %s", got.body)
	}
	// Three parts joined by dots, signed with the key.
	if !strings.HasPrefix(got.bearer, "bearer ") || strings.Count(got.bearer, ".") != 2 {
		t.Fatalf("signed as %q", got.bearer)
	}
}

func TestAnAppRunFromXcodeIsReachedOnApplesOtherServer(t *testing.T) {
	p, calls := apple(t)
	if err := p.send(context.Background(), request("done", "1")); err != nil {
		t.Fatal(err)
	}
	if got := <-calls; !strings.HasPrefix(got.path, "/sandbox/") || !strings.Contains(got.body, "An agent finished.") {
		t.Fatalf("asked %+v", got)
	}
}

func TestOnlySendsItsOwnWordsToRealTokens(t *testing.T) {
	p, calls := apple(t)
	for _, payload := range [][]byte{
		request("buy now", ""),
		[]byte(`{"token":"../../other","notice":"waiting"}`),
		[]byte(`not json`),
	} {
		if err := p.send(context.Background(), payload); err == nil {
			t.Fatalf("%s was sent", payload)
		}
	}
	if len(calls) != 0 {
		t.Fatal("Apple was asked")
	}
}

func TestAMacAsksThroughItsConnection(t *testing.T) {
	p, calls := apple(t)
	server := httptest.NewServer(newRelay(p).handler())
	t.Cleanup(server.Close)
	mac := dial(t, "ws"+strings.TrimPrefix(server.URL, "http"), "mac")
	mac.send(append([]byte{framePush}, request("waiting", "")...))
	select {
	case got := <-calls:
		if !strings.HasSuffix(got.path, someToken) {
			t.Fatalf("asked %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Apple was never asked")
	}
}

func TestWithoutAKeyNothingIsSent(t *testing.T) {
	base := serve(t)
	mac := dial(t, base, "mac")
	phone := dial(t, base, "phone")
	mac.expect(numbered(frameJoined, 1, nil))
	mac.send(append([]byte{framePush}, request("waiting", "")...))
	// The connection carries on as if nothing was asked.
	phone.expect([]byte{frameOnline})
	phone.send(append([]byte{frameData}, "still here"...))
	mac.expect(numbered(frameData, 1, []byte("still here")))
}

func TestAFailureNeverPutsTheTokenInTheLog(t *testing.T) {
	p, _ := apple(t)
	// Nothing listens here, so the call fails the way a network problem would.
	p.production = "http://127.0.0.1:1"
	err := p.send(context.Background(), request("waiting", ""))
	if err == nil || strings.Contains(err.Error(), someToken) {
		t.Fatalf("error was %v", err)
	}
}

func TestSaysWhyAppleRefused(t *testing.T) {
	p, _ := apple(t)
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"reason":"BadDeviceToken"}`))
	}))
	t.Cleanup(refusing.Close)
	p.production = refusing.URL
	err := p.send(context.Background(), request("waiting", ""))
	if err == nil || !strings.Contains(err.Error(), "BadDeviceToken") || strings.Contains(err.Error(), someToken) {
		t.Fatalf("error was %v", err)
	}
}

func TestPassesOnWhatIsSealedWithoutReadingIt(t *testing.T) {
	p, calls := apple(t)
	asked, _ := json.Marshal(pushRequest{Token: someToken, Notice: "waiting", Sealed: "AQID+/8="})
	if err := p.send(context.Background(), asked); err != nil {
		t.Fatal(err)
	}
	got := <-calls
	if !strings.Contains(got.body, `"farol":"AQID+/8="`) || !strings.Contains(got.body, `"mutable-content":1`) {
		t.Fatalf("said %s", got.body)
	}
	bad, _ := json.Marshal(pushRequest{Token: someToken, Notice: "waiting", Sealed: `","aps":{}`})
	if err := p.send(context.Background(), bad); err == nil {
		t.Fatal("something that is not sealed bytes was passed on")
	}
}
