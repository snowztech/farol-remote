package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
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
	"strings"
	"sync"
	"time"
)

const googleTokenURL = "https://oauth2.googleapis.com/token"
const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

type fcmPusher struct {
	email    string
	key      *rsa.PrivateKey
	client   *http.Client
	tokenURL string
	endpoint string

	mu      sync.Mutex
	bearer  string
	expires time.Time
}

// The service account belongs on the relay only, never in the Android app.
func newFCMPusher() (*fcmPusher, error) {
	credential := os.Getenv("FCM_SERVICE_ACCOUNT_JSON")
	if credential == "" {
		return nil, nil
	}
	var account struct {
		Type       string `json:"type"`
		Project    string `json:"project_id"`
		Email      string `json:"client_email"`
		PrivateKey string `json:"private_key"`
	}
	if err := json.Unmarshal([]byte(credential), &account); err != nil || account.Type != "service_account" || account.Project == "" || account.Email == "" {
		return nil, errors.New("FCM_SERVICE_ACCOUNT_JSON is not a service account credential")
	}
	block, _ := pem.Decode([]byte(account.PrivateKey))
	if block == nil {
		return nil, errors.New("FCM_SERVICE_ACCOUNT_JSON has no private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("FCM_SERVICE_ACCOUNT_JSON has an invalid private key")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("FCM_SERVICE_ACCOUNT_JSON needs an RSA private key")
	}
	return &fcmPusher{
		email: account.Email, key: key,
		client:   &http.Client{Timeout: 10 * time.Second},
		tokenURL: googleTokenURL,
		endpoint: "https://fcm.googleapis.com/v1/projects/" + url.PathEscape(account.Project) + "/messages:send",
	}, nil
}

var googleErrorStatus = regexp.MustCompile(`^[A-Z_]{1,40}$`)

func (p *fcmPusher) send(ctx context.Context, request pushRequest, token, room string) error {
	if request.Activity != "" || !validFCMToken(token) || len(request.Sealed) > 2000 || !sealedText.MatchString(request.Sealed) {
		return errors.New("not a notification this relay sends")
	}
	if _, known := notices[request.Notice]; !known {
		return errors.New("not a notification this relay sends")
	}
	priority := "NORMAL"
	if request.Notice == "waiting" {
		priority = "HIGH"
	}
	body, _ := json.Marshal(map[string]any{"message": map[string]any{
		"token":   token,
		"data":    map[string]string{"notice": request.Notice, "sealed": request.Sealed, "host": room},
		"android": map[string]string{"priority": priority},
	}})
	bearer, err := p.accessToken(ctx)
	if err != nil {
		return err
	}
	call, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	call.Header.Set("Authorization", "Bearer "+bearer)
	call.Header.Set("Content-Type", "application/json")
	answer, err := p.client.Do(call)
	if err != nil {
		return safeGoogleError("firebase could not be reached", err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		var failure struct {
			Error struct {
				Status string `json:"status"`
			} `json:"error"`
		}
		json.NewDecoder(io.LimitReader(answer.Body, 4096)).Decode(&failure)
		if googleErrorStatus.MatchString(failure.Error.Status) {
			return fmt.Errorf("firebase refused: %s %s", answer.Status, failure.Error.Status)
		}
		return fmt.Errorf("firebase refused: %s", answer.Status)
	}
	return nil
}

// Registration tokens are opaque to us, but cannot contain control characters
// or be large enough to turn a push request into an abuse of the HTTP client.
func validFCMToken(token string) bool {
	if len(token) < 16 || len(token) > 4096 {
		return false
	}
	for i := 0; i < len(token); i++ {
		if token[i] < '!' || token[i] > '~' {
			return false
		}
	}
	return true
}

func (p *fcmPusher) accessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bearer != "" && time.Now().Before(p.expires) {
		return p.bearer, nil
	}
	now := time.Now()
	encode := base64.RawURLEncoding.EncodeToString
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iss": p.email, "scope": fcmScope, "aud": p.tokenURL,
		"iat": now.Unix(), "exp": now.Add(55 * time.Minute).Unix(),
	})
	unsigned := encode(header) + "." + encode(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("firebase authorization could not be signed")
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {unsigned + "." + encode(signature)}}
	call, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	call.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	answer, err := p.client.Do(call)
	if err != nil {
		return "", safeGoogleError("firebase authorization could not be reached", err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		return "", fmt.Errorf("firebase authorization refused: %s", answer.Status)
	}
	var granted struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(answer.Body, 4096)).Decode(&granted); err != nil || granted.AccessToken == "" || granted.ExpiresIn <= 60 {
		return "", errors.New("firebase authorization returned no usable token")
	}
	p.bearer = granted.AccessToken
	p.expires = time.Now().Add(time.Duration(granted.ExpiresIn-60) * time.Second)
	return p.bearer, nil
}

func safeGoogleError(message string, err error) error {
	// url.Error includes the request URL. Never let credentials or tokens into logs.
	var failed *url.Error
	if errors.As(err, &failed) {
		err = failed.Err
	}
	return fmt.Errorf("%s: %w", message, err)
}
