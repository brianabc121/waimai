package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// WebPush sends reminders to browsers with the standard Web Push protocol.
// The key pair identifies this server to the browsers' push services.
type WebPush struct {
	PublicKey  string
	PrivateKey string
	Subject    string // a mailto: or https: contact the push service can reach
	Client     *http.Client
}

// NewVAPIDKeys makes a key pair for WebPush, as (private, public).
func NewVAPIDKeys() (string, string, error) { return webpush.GenerateVAPIDKeys() }

func (w WebPush) Send(ctx context.Context, d Device, m PushMessage) error {
	var sub webpush.Subscription
	if err := json.Unmarshal([]byte(d.Token), &sub); err != nil || sub.Endpoint == "" {
		return ErrPushGone
	}
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	client := w.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := webpush.SendNotificationWithContext(ctx, body, &sub, &webpush.Options{
		HTTPClient: client, Subscriber: w.Subject, VAPIDPublicKey: w.PublicKey, VAPIDPrivateKey: w.PrivateKey,
		TTL: 600, Urgency: webpush.UrgencyHigh,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrPushGone
	case resp.StatusCode >= 300:
		return fmt.Errorf("web push: the push service answered %d", resp.StatusCode)
	}
	return nil
}
