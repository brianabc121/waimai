package app

import (
	"context"
	"errors"
	"log"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brianabc121/waimai/internal/config"
)

// NewFromConfig wires an App the same way for api and worker: the keys saved
// in the back office over the ones in the environment. Saved keys that do
// not work together are set aside with a warning rather than keeping the
// server down, so that they can be corrected in the back office.
func NewFromConfig(pool *pgxpool.Pool, cfg config.Config) (*App, error) {
	a := New(pool, cfg.AppSecret)
	a.OTPDevEcho = cfg.OTPDevEcho
	a.PublicBaseURL = cfg.PublicBaseURL
	a.base = cfg
	if cfg.OpenIMAPIURL != "" {
		// The secret signs every chat sign-in and the key guards the callbacks:
		// with a short or default one anybody could write as anybody.
		if len(cfg.OpenIMSecret) < 16 || len(cfg.OpenIMHookKey) < 16 {
			if cfg.OpenIMSecret != "" && isLocalURL(cfg.OpenIMAPIURL) {
				log.Print("WARNING: OPENIM_SECRET or OPENIM_HOOK_KEY is short; fine on a development machine only.")
			} else {
				return nil, errors.New("OPENIM_API_URL is set: OPENIM_SECRET and OPENIM_HOOK_KEY must each be at least 16 characters (openssl rand -hex 24)")
			}
		}
		a.IM = &OpenIM{API: cfg.OpenIMAPIURL, Secret: cfg.OpenIMSecret, AdminID: cfg.OpenIMAdminID}
	}
	err := a.ReloadSettings(context.Background())
	if err == nil {
		return a, nil
	}
	in, envErr := buildIntegrations(cfg)
	if envErr != nil {
		return nil, envErr
	}
	log.Printf("WARNING: settings: keys saved in the back office are not in use: %v", err)
	a.install(in, "")
	return a, nil
}

// isLocalURL reports whether an address is on this machine.
func isLocalURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}
