package app

import (
	"context"
	"log"

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
