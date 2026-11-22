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
	a.MapsOff = cfg.GoogleMapsKey == ""
	switch cfg.GoogleRoutesKey {
	case "":
	case "stub":
		a.Router = StubRouter{}
	default:
		a.Router = GoogleRoutes{Key: cfg.GoogleRoutesKey}
	}
	sender, err := sms.FromConfig(cfg)
	if err != nil {
		return nil, err
	}
	if sender != nil {
		a.SMS = sender
	}
	a.Push = map[string]PushSender{}
	if cfg.VAPIDPublicKey != "" && cfg.VAPIDPrivateKey != "" {
		subject := cfg.VAPIDSubject
		if subject == "" {
			subject = cfg.PublicBaseURL
		}
		a.Push["web"] = WebPush{PublicKey: cfg.VAPIDPublicKey, PrivateKey: cfg.VAPIDPrivateKey, Subject: subject}
	}
	if cfg.FCMServiceAccount != "" {
		fcm, err := NewFCM(cfg.FCMServiceAccount)
		if err != nil {
			return nil, err
		}
		a.Push["android"], a.Push["ios"] = fcm, fcm
	}
	return a, nil
}
