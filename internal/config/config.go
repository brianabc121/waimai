// Package config reads runtime settings from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr              string
	DatabaseURL       string
	ListenDatabaseURL string // direct (non-pooled) connection: LISTEN and migrations
	AppSecret         string
	CookieSecure      bool
	OTPDevEcho        bool
	// GoogleMapsKey is the browser key for the delivery map. Empty turns the
	// map features off; "stub" serves a tile-less map for tests and demos.
	GoogleMapsKey string
	EmbedWorker   bool
	StaticDir     string
	PublicBaseURL string // customer-facing origin, for order links in SMS

	// Web push for the staff workbench in a browser. Empty keys turn it off.
	// Make a pair with: admin vapid
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string // mailto: or https: contact for the push services
	// FCMServiceAccount is the Firebase service account for push to the
	// Android and iPhone apps: the JSON itself or the path of its file.
	FCMServiceAccount string

	SMSProvider        string // log (default), webhook, twilio
	SMSWebhookURL      string
	SMSWebhookAuth     string
	TwilioAccountSID   string
	TwilioAuthToken    string
	TwilioFrom         string
	TwilioMessagingSID string
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}

func Load() Config {
	c := Config{
		Addr:          env("ADDR", ":8080"),
		DatabaseURL:   env("DATABASE_URL", "postgres://postgres@localhost:5432/waimai?sslmode=disable"),
		AppSecret:     env("APP_SECRET", "dev-secret-change-me"),
		CookieSecure:  envBool("COOKIE_SECURE", false),
		OTPDevEcho:    envBool("OTP_DEV_ECHO", false),
		EmbedWorker:   envBool("EMBED_WORKER", false),
		StaticDir:     env("STATIC_DIR", ""),
		GoogleMapsKey: env("GOOGLE_MAPS_API_KEY", ""),

		VAPIDPublicKey:    env("VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey:   env("VAPID_PRIVATE_KEY", ""),
		VAPIDSubject:      env("VAPID_SUBJECT", ""),
		FCMServiceAccount: env("FCM_SERVICE_ACCOUNT", ""),

		PublicBaseURL:      env("PUBLIC_BASE_URL", ""),
		SMSProvider:        env("SMS_PROVIDER", "log"),
		SMSWebhookURL:      env("SMS_WEBHOOK_URL", ""),
		SMSWebhookAuth:     env("SMS_WEBHOOK_AUTH", ""),
		TwilioAccountSID:   env("TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken:    env("TWILIO_AUTH_TOKEN", ""),
		TwilioFrom:         env("TWILIO_FROM", ""),
		TwilioMessagingSID: env("TWILIO_MESSAGING_SERVICE_SID", ""),
	}
	// DIRECT_DATABASE_URL (older name: LISTEN_DATABASE_URL) is only needed when
	// DATABASE_URL goes through a transaction-mode pooler.
	c.ListenDatabaseURL = env("DIRECT_DATABASE_URL", env("LISTEN_DATABASE_URL", c.DatabaseURL))
	if c.ListenDatabaseURL != c.DatabaseURL {
		c.DatabaseURL = PoolerSafe(c.DatabaseURL)
	}
	return c
}

// PoolerSafe makes a connection URL usable through a transaction-mode pooler
// (PgBouncer and the "pooled" endpoints of managed databases). Such a pooler
// hands every transaction to whichever server connection is free, so named
// prepared statements, which pgx caches per connection by default, end up on
// the wrong connection ("prepared statement already exists / does not exist").
// cache_describe keeps typed parameters but never leaves a statement behind.
// A mode already chosen in the URL is kept.
func PoolerSafe(url string) string {
	if strings.Contains(url, "default_query_exec_mode=") {
		return url
	}
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	return url + sep + "default_query_exec_mode=cache_describe"
}
