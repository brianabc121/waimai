// Command admin creates the platform administrator, or resets its password.
// The platform administrator signs in at /login and creates merchants.
//
//	ADMIN_USERNAME=admin ADMIN_PASSWORD='a long password' admin
package main

import (
	"context"
	"log"
	"os"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/config"
	"github.com/brianabc121/waimai/internal/db"
)

func main() {
	username, password := os.Getenv("ADMIN_USERNAME"), os.Getenv("ADMIN_PASSWORD")
	if username == "" {
		username = "admin"
	}
	if len(password) < 10 {
		log.Fatal("set ADMIN_PASSWORD to a password of at least 10 characters")
	}
	cfg := config.Load()
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()
	if err := app.New(pool, cfg.AppSecret).EnsurePlatformAdmin(ctx, username, password); err != nil {
		log.Fatalf("admin: %v", err)
	}
	log.Printf("platform administrator %q is ready", username)
}
