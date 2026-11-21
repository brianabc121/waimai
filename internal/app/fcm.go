package app

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// FCM sends reminders to the Android and iPhone apps through Firebase Cloud
// Messaging (HTTP v1). It signs in with the service account of the Firebase
// project and keeps the access token until shortly before it expires.
type FCM struct {
	projectID string
	email     string
	key       *rsa.PrivateKey
	tokenURL  string
	sendURL   string
	client    *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewFCM reads a Firebase service account: either the JSON itself or the path
// of the file that holds it.
func NewFCM(account string) (*FCM, error) {
	account = strings.TrimSpace(account)
	if !strings.HasPrefix(account, "{") {
		b, err := os.ReadFile(account)
		if err != nil {
			return nil, fmt.Errorf("fcm: service account file: %w", err)
		}
		account = string(b)
	}
	var sa struct {
		ProjectID   string `json:"project_id"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal([]byte(account), &sa); err != nil {
		return nil, fmt.Errorf("fcm: service account is not JSON: %w", err)
	}
	if sa.ProjectID == "" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, errors.New("fcm: service account needs project_id, client_email and private_key")
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(sa.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("fcm: private key: %w", err)
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return &FCM{projectID: sa.ProjectID, email: sa.ClientEmail, key: key, tokenURL: sa.TokenURI,
		sendURL: "https://fcm.googleapis.com/v1/projects/" + sa.ProjectID + "/messages:send",
		client:  &http.Client{Timeout: 15 * time.Second}}, nil
}

// SetSendURL points the sender somewhere else; tests use it.
func (f *FCM) SetSendURL(u string) { f.sendURL = u }

func (f *FCM) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token != "" && time.Until(f.expires) > time.Minute {
		return f.token, nil
	}
	now := time.Now()
	assertion, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": f.email, "scope": "https://www.googleapis.com/auth/firebase.messaging", "aud": f.tokenURL,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}).SignedString(f.key)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &out) != nil || out.AccessToken == "" {
		return "", fmt.Errorf("fcm: sign-in answered %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	f.token, f.expires = out.AccessToken, now.Add(time.Duration(out.ExpiresIn)*time.Second)
	return f.token, nil
}

func (f *FCM) Send(ctx context.Context, d Device, m PushMessage) error {
	token, err := f.accessToken(ctx)
	if err != nil {
		return err
	}
	// The notification block lets the phone show the reminder while the app is
	// closed; the data block tells the app where to go when it is tapped.
	msg := map[string]any{"message": map[string]any{
		"token":        d.Token,
		"notification": map[string]string{"title": m.Title, "body": m.Body},
		"data":         map[string]string{"url": m.URL, "tag": m.Tag},
		"android": map[string]any{"priority": "HIGH", "ttl": "600s",
			"notification": map[string]any{"channel_id": "deliveries", "tag": m.Tag, "sound": "default"}},
		"apns": map[string]any{"headers": map[string]string{"apns-priority": "10"},
			"payload": map[string]any{"aps": map[string]any{"sound": "default", "thread-id": m.Tag}}},
	}}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.sendURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusNotFound || bytes.Contains(answer, []byte("UNREGISTERED")):
		return ErrPushGone // the app was removed or its token was replaced
	case resp.StatusCode == http.StatusUnauthorized:
		f.mu.Lock()
		f.token = ""
		f.mu.Unlock()
	}
	return fmt.Errorf("fcm: answered %d: %s", resp.StatusCode, truncate(string(answer), 200))
}
