package app

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/brianabc121/waimai/internal/config"
	"github.com/brianabc121/waimai/internal/sms"
)

// NewFromConfig wires an App the same way for api and worker.
func NewFromConfig(pool *pgxpool.Pool, cfg config.Config) (*App, error) {
	a := New(pool, cfg.AppSecret)
	a.OTPDevEcho = cfg.OTPDevEcho
	a.PublicBaseURL = cfg.PublicBaseURL
	sender, err := sms.FromConfig(cfg)
	if err != nil {
		return nil, err
	}
	if sender != nil {
		a.SMS = sender
	}
	return a, nil
}
