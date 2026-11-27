// Command migrate applies database migrations once and exits.
package main

import (
	"context"
	"log"

	"github.com/brianabc121/waimai/internal/config"
	"github.com/brianabc121/waimai/internal/db"
)

func main() {
	cfg := config.Load()
	// Migrations hold a session lock, so they use the direct connection when
	// the main one goes through a pooler.
	ctx := context.Background()
	if err := db.Migrate(ctx, cfg.MigrateDatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Print("migrations applied")
	// The app's restricted role (00085) logs in with APP_DB_PASSWORD.
	if cfg.AppDBPassword != "" {
		if err := db.SetAppRolePassword(ctx, cfg.MigrateDatabaseURL, cfg.AppDBPassword); err != nil {
			log.Fatalf("app role: %v", err)
		}
		log.Print("waimai_app can log in")
	}
}
