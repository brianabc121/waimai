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
	if err := db.Migrate(context.Background(), cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Print("migrations applied")
}
