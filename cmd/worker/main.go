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
	go a.WatchSettings(ctx, log.Printf)
	// the mall's own texts, in case they changed before translation was set up
	if err := a.QueueTranslatePlatform(ctx); err != nil {
		log.Printf("translate platform: %v", err)
	}
	a.RunWorker(ctx, log.Printf, false)
}
