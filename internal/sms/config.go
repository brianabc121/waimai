package sms

import (
	"context"
	"fmt"

	"github.com/brianabc121/waimai/internal/config"
)

// Sender mirrors app.SMSSender so this package does not import app.
type Sender interface {
	Send(ctx context.Context, phone, message string) error
}

// FromConfig builds the configured provider. It returns nil for "log", which
// leaves the app's built-in logging sender in place.
func FromConfig(cfg config.Config) (Sender, error) {
	switch cfg.SMSProvider {
	case "", "log":
		return nil, nil
	case "webhook":
		if cfg.SMSWebhookURL == "" {
			return nil, fmt.Errorf("SMS_PROVIDER=webhook needs SMS_WEBHOOK_URL")
		}
		return Webhook{URL: cfg.SMSWebhookURL, AuthHeader: cfg.SMSWebhookAuth}, nil
	case "twilio":
		if cfg.TwilioAccountSID == "" || cfg.TwilioAuthToken == "" || (cfg.TwilioFrom == "" && cfg.TwilioMessagingSID == "") {
			return nil, fmt.Errorf("SMS_PROVIDER=twilio needs TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN and TWILIO_FROM or TWILIO_MESSAGING_SERVICE_SID")
		}
		return Twilio{AccountSID: cfg.TwilioAccountSID, AuthToken: cfg.TwilioAuthToken, From: cfg.TwilioFrom,
			MessagingServiceSID: cfg.TwilioMessagingSID}, nil
	default:
		return nil, fmt.Errorf("unknown SMS_PROVIDER %q", cfg.SMSProvider)
	}
}
