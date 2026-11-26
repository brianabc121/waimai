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
	// GoogleRoutesKey is a server key for the Routes API: the planned route
	// of a rider's trip. Empty keeps the straight-line estimate; "stub" is a
	// stand-in for tests and demos.
	GoogleRoutesKey string

	// Menu translation (docs/I18N_DESIGN.md §6.5). TranslateProvider is
	// claude-haiku-5-5 (the default when an Anthropic key is set),
	// claude-sonnet-5-5, google, off, or stub (tests: marks the text instead
	// of translating it).
	TranslateProvider  string
	AnthropicAPIKey    string
	GoogleTranslateKey string
	EmbedWorker        bool
	StaticDir          string
	PublicBaseURL      string // customer-facing origin, for order links in SMS
	// StoreSiteDomain is the domain under which each store has its own page,
	// <store_no>.<StoreSiteDomain> (docs/ORDER_TYPES_DESIGN.md §5).
	StoreSiteDomain string

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

	// Chat goes through an OpenIM server (docs/CHAT_DESIGN.md). An empty
	// OpenIMAPIURL turns chat off.
	OpenIMAPIURL  string // reached from this server, e.g. http://openim-server:10002
	OpenIMSecret  string // OpenIM's share.secret
	OpenIMAdminID string // OpenIM's administrator account
	// Where browsers reach OpenIM: a full address or a path on this site.
	OpenIMPublicWS  string
	OpenIMPublicAPI string
	// OpenIMHookKey is part of the address OpenIM calls back, since its
	// callbacks carry no signature.
	OpenIMHookKey string
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
		Addr:               env("ADDR", ":8080"),
		DatabaseURL:        env("DATABASE_URL", "postgres://postgres@localhost:5432/waimai?sslmode=disable"),
		AppSecret:          env("APP_SECRET", "dev-secret-change-me"),
		CookieSecure:       envBool("COOKIE_SECURE", false),
		OTPDevEcho:         envBool("OTP_DEV_ECHO", false),
		EmbedWorker:        envBool("EMBED_WORKER", false),
		StaticDir:          env("STATIC_DIR", ""),
		GoogleMapsKey:      env("GOOGLE_MAPS_API_KEY", ""),
		GoogleRoutesKey:    env("GOOGLE_ROUTES_API_KEY", ""),
		TranslateProvider:  env("TRANSLATE_PROVIDER", ""),
		AnthropicAPIKey:    env("ANTHROPIC_API_KEY", ""),
		GoogleTranslateKey: env("GOOGLE_TRANSLATE_API_KEY", ""),

		VAPIDPublicKey:    env("VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey:   env("VAPID_PRIVATE_KEY", ""),
		VAPIDSubject:      env("VAPID_SUBJECT", ""),
		FCMServiceAccount: env("FCM_SERVICE_ACCOUNT", ""),

		PublicBaseURL:      env("PUBLIC_BASE_URL", ""),
		StoreSiteDomain:    env("STORE_SITE_DOMAIN", ""),
		SMSProvider:        env("SMS_PROVIDER", "log"),
		SMSWebhookURL:      env("SMS_WEBHOOK_URL", ""),
		SMSWebhookAuth:     env("SMS_WEBHOOK_AUTH", ""),
		TwilioAccountSID:   env("TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken:    env("TWILIO_AUTH_TOKEN", ""),
		TwilioFrom:         env("TWILIO_FROM", ""),
		TwilioMessagingSID: env("TWILIO_MESSAGING_SERVICE_SID", ""),

		OpenIMAPIURL:    strings.TrimRight(env("OPENIM_API_URL", ""), "/"),
		OpenIMSecret:    env("OPENIM_SECRET", ""),
		OpenIMAdminID:   env("OPENIM_ADMIN_ID", "imAdmin"),
		OpenIMPublicWS:  env("OPENIM_PUBLIC_WS", "/im/ws"),
		OpenIMPublicAPI: strings.TrimRight(env("OPENIM_PUBLIC_API", "/im/api"), "/"),
		OpenIMHookKey:   env("OPENIM_HOOK_KEY", ""),
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
