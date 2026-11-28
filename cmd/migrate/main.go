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
	// The app's restricted role (00085). Its rights were just granted again
	// by Migrate, so a role created by hand after the first deploy works too.
	exists, err := db.AppRoleExists(ctx, cfg.MigrateDatabaseURL)
	if err != nil {
		log.Fatalf("app role: %v", err)
	}
	if !exists {
		if cfg.AppDBPassword != "" {
			log.Fatal("app role: APP_DB_PASSWORD is set but the role waimai_app does not exist and this user may not create it; " +
				"create a user named waimai_app in the database console and run migrate again")
		}
		return
	}
	log.Print("waimai_app: rights granted")
	// It logs in with APP_DB_PASSWORD.
	if cfg.AppDBPassword != "" {
		if err := db.SetAppRolePassword(ctx, cfg.MigrateDatabaseURL, cfg.AppDBPassword); err != nil {
			// A managed database may keep passwords to its console.
			log.Printf("app role: could not set the password (%v); set the same password for waimai_app in the database console", err)
			return
		}
		log.Print("waimai_app can log in")
	}
}
