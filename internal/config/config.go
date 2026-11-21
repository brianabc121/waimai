// Package config reads runtime settings from environment variables.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Addr              string
	DatabaseURL       string
	ListenDatabaseURL string // direct (non-pooled) connection used for LISTEN
	AppSecret         string
	CookieSecure      bool
	OTPDevEcho        bool
	EmbedWorker       bool
	StaticDir         string
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
		Addr:         env("ADDR", ":8080"),
		DatabaseURL:  env("DATABASE_URL", "postgres://postgres@localhost:5432/waimai?sslmode=disable"),
		AppSecret:    env("APP_SECRET", "dev-secret-change-me"),
		CookieSecure: envBool("COOKIE_SECURE", false),
		OTPDevEcho:   envBool("OTP_DEV_ECHO", false),
		EmbedWorker:  envBool("EMBED_WORKER", false),
		StaticDir:    env("STATIC_DIR", ""),
	}
	c.ListenDatabaseURL = env("LISTEN_DATABASE_URL", c.DatabaseURL)
	return c
}
