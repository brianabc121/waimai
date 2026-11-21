// Package sms holds SMS provider clients. Each satisfies app.SMSSender.
package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var client = &http.Client{Timeout: 15 * time.Second}

func check(resp *http.Response) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	return fmt.Errorf("sms provider returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// Webhook posts {"to": "...", "message": "..."} to any HTTPS endpoint. It
// covers local SMS gateways and aggregators that accept a simple JSON call.
type Webhook struct {
	URL        string
	AuthHeader string // sent as the Authorization header when set
}

func (w Webhook) Send(ctx context.Context, phone, message string) error {
	body, _ := json.Marshal(map[string]string{"to": phone, "message": message})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.AuthHeader != "" {
		req.Header.Set("Authorization", w.AuthHeader)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return check(resp)
}

// Twilio sends through the Twilio Messages API. Set either From (a sender
// number or alphanumeric ID) or MessagingServiceSID.
type Twilio struct {
	AccountSID          string
	AuthToken           string
	From                string
	MessagingServiceSID string
	BaseURL             string // overridden in tests; defaults to https://api.twilio.com
}

func (t Twilio) Send(ctx context.Context, phone, message string) error {
	base := t.BaseURL
	if base == "" {
		base = "https://api.twilio.com"
	}
	form := url.Values{"To": {phone}, "Body": {message}}
	if t.MessagingServiceSID != "" {
		form.Set("MessagingServiceSid", t.MessagingServiceSID)
	} else {
		form.Set("From", t.From)
	}
	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", base, url.PathEscape(t.AccountSID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(t.AccountSID, t.AuthToken)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return check(resp)
}
