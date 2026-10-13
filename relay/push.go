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
	"net/http"
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
}

var deviceToken = regexp.MustCompile(`^[0-9a-f]{64,200}$`)

// send asks Apple to show one notification. What a Mac sent is checked first, since anyone can connect as a Mac.
func (p *pusher) send(ctx context.Context, payload []byte) error {
	var request pushRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return err
	}
	words, known := notices[request.Notice]
	if !known || !deviceToken.MatchString(request.Token) {
		return errors.New("not a notification this relay sends")
	}
	body, _ := json.Marshal(map[string]any{
		"aps": map[string]any{"alert": map[string]string{"title": "Farol", "body": words}, "sound": "default"},
	})
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
	call.Header.Set("apns-topic", p.topic)
	call.Header.Set("apns-push-type", "alert")
	answer, err := p.client.Do(call)
	if err != nil {
		return err
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		return fmt.Errorf("apple answered %s", answer.Status)
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
