package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sync"
	"time"
)

// What a notification says. The Mac only picks which one, so the relay can never be made to show a phone any text it likes.
var notices = map[string]string{
	"waiting": "An agent is waiting for you.",
	"done":    "An agent finished.",
}

// A pusher asks Apple to notify a phone. It needs the key Apple gives the app's developer, which is why a Mac cannot do this itself.
type pusher struct {
	key    *ecdsa.PrivateKey
	keyID  string
	teamID string
	topic  string
	// Apple's two servers: one for apps from the App Store and TestFlight, one for apps run from Xcode.
	production, sandbox string
	client              *http.Client

	mu     sync.Mutex
	bearer string
	signed time.Time
}

// newPusher reads the key from the environment. Without one the relay still relays, and sends no notifications.
func newPusher() (*pusher, error) {
	text, keyID, teamID := os.Getenv("APNS_KEY"), os.Getenv("APNS_KEY_ID"), os.Getenv("APNS_TEAM_ID")
	if text == "" || keyID == "" || teamID == "" {
		return nil, nil
	}
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, errors.New("APNS_KEY is not the text of a .p8 file")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("APNS_KEY: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("APNS_KEY is not the kind of key Apple gives for notifications")
	}
	topic := os.Getenv("APNS_TOPIC")
	if topic == "" {
		topic = "sh.farol.ios"
	}
	return &pusher{
		key: key, keyID: keyID, teamID: teamID, topic: topic,
		production: "https://api.push.apple.com", sandbox: "https://api.sandbox.push.apple.com",
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

type pushRequest struct {
	Token   string `json:"token"`
	Notice  string `json:"notice"`
	Sandbox string `json:"sandbox"`
	// Which session it is about, sealed by the Mac for the phone. The relay cannot open it, and only passes it on.
	Sealed string `json:"sealed"`
	// Set for a Live Activity instead of a notification: "start", "update" or "end". The relay writes the rest from the two counts.
	Activity string `json:"activity"`
	Waiting  int    `json:"waiting"`
	Working  int    `json:"working"`
	// An agent just started waiting. Only then is it worth Apple's budget for urgent Live Activity pushes.
	Urgent bool `json:"urgent"`
}

var activityEvents = map[string]bool{"start": true, "update": true, "end": true}

var sealedText = regexp.MustCompile(`^[A-Za-z0-9+/=]*$`)

var deviceToken = regexp.MustCompile(`^[0-9a-f]{64,200}$`)

// send asks Apple to show one notification. What a Mac sent is checked first, since anyone can connect as a Mac.
func (p *pusher) send(ctx context.Context, asked []byte) error {
	var request pushRequest
	if err := json.Unmarshal(asked, &request); err != nil {
		return err
	}
	if !deviceToken.MatchString(request.Token) || len(request.Sealed) > 2000 || !sealedText.MatchString(request.Sealed) {
		return errors.New("not a notification this relay sends")
	}
	payload, kind, topic, priority := map[string]any{}, "alert", p.topic, "10"
	if request.Activity != "" {
		if !activityEvents[request.Activity] || request.Waiting < 0 || request.Working < 0 || request.Waiting > 999 || request.Working > 999 {
			return errors.New("not a notification this relay sends")
		}
		// Only the two counts are in the clear. The names ride sealed, and the phone's widget opens them.
		state := map[string]any{"waiting": request.Waiting, "working": request.Working}
		if request.Sealed != "" {
			state["sealed"] = request.Sealed
		}
		aps := map[string]any{"timestamp": time.Now().Unix(), "event": request.Activity, "content-state": state}
		if request.Activity == "end" {
			// Stays on the lock screen a few minutes with its last state, then goes.
			aps["dismissal-date"] = time.Now().Add(5 * time.Minute).Unix()
		} else {
			// If updates stop, say the Mac slept, iOS shows it as out of date instead of a "working" that no longer is.
			aps["stale-date"] = time.Now().Add(15 * time.Minute).Unix()
		}
		if request.Activity == "start" {
			// Started with the app closed. iOS needs to know which kind to make, and an alert to light the screen with.
			// No sound: a waiting agent already gets its own notification.
			words := "An agent is working."
			if request.Waiting > 0 {
				words = notices["waiting"]
			}
			aps["attributes-type"], aps["attributes"] = "SessionsActivity", map[string]any{}
			aps["alert"] = map[string]string{"title": "Farol", "body": words}
		}
		payload["aps"], kind, topic = aps, "liveactivity", p.topic+".push-type.liveactivity"
		// Apple caps urgent Live Activity pushes per hour. The rest can wait for the phone's next wake. A start is never held back.
		if !request.Urgent && request.Activity != "start" {
			priority = "5"
		}
	} else {
		words, known := notices[request.Notice]
		if !known {
			return errors.New("not a notification this relay sends")
		}
		aps := map[string]any{"alert": map[string]string{"title": "Farol", "body": words}, "sound": "default"}
		payload["aps"] = aps
		if request.Sealed != "" {
			// Lets the app open what is sealed and show the session's name. If it cannot, the words above are shown.
			aps["mutable-content"] = 1
			payload["farol"] = request.Sealed
		}
	}
	body, _ := json.Marshal(payload)
	host := p.production
	if request.Sandbox != "" {
		host = p.sandbox
	}
	call, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/3/device/"+request.Token, bytes.NewReader(body))
	if err != nil {
		return err
	}
	bearer, err := p.token()
	if err != nil {
		return err
	}
	call.Header.Set("authorization", "bearer "+bearer)
	call.Header.Set("apns-topic", topic)
	call.Header.Set("apns-push-type", kind)
	call.Header.Set("apns-priority", priority)
	answer, err := p.client.Do(call)
	if err != nil {
		// The error names the address it called, which ends in the phone's token. That has no place in a log.
		var failed *url.Error
		if errors.As(err, &failed) {
			err = failed.Err
		}
		return fmt.Errorf("apple could not be reached: %w", err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		// Apple says why in a word, like BadDeviceToken, which is what tells a wrong key from a stale phone.
		var why struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(io.LimitReader(answer.Body, 1024)).Decode(&why)
		return fmt.Errorf("apple refused: %s %s", answer.Status, why.Reason)
	}
	return nil
}

// token signs who we are for Apple. Apple wants the same one reused, and refuses one older than an hour.
func (p *pusher) token() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bearer != "" && time.Since(p.signed) < 40*time.Minute {
		return p.bearer, nil
	}
	encode := base64.RawURLEncoding.EncodeToString
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": p.keyID})
	claims, _ := json.Marshal(map[string]any{"iss": p.teamID, "iat": time.Now().Unix()})
	unsigned := encode(header) + "." + encode(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, p.key, digest[:])
	if err != nil {
		return "", err
	}
	// The two numbers side by side, each padded to 32 bytes, which is how a token carries this kind of signature.
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	p.bearer, p.signed = unsigned+"."+encode(signature), time.Now()
	return p.bearer, nil
}
