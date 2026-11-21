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
	if err := db.Migrate(context.Background(), cfg.ListenDatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Print("migrations applied")
}
