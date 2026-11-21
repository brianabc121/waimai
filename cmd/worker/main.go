// Command worker runs background jobs: accept-timeout cancellation and cleanup.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/config"
	"github.com/brianabc121/waimai/internal/db"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()
	log.Print("worker started")
	a, err := app.NewFromConfig(pool, cfg)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	a.RunWorker(ctx, log.Printf)
}
