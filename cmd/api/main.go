// Command api serves the JSON API and SSE.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/brianabc121/waimai/internal/app"
	"github.com/brianabc121/waimai/internal/config"
	"github.com/brianabc121/waimai/internal/db"
	"github.com/brianabc121/waimai/internal/httpapi"
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

	a, err := app.NewFromConfig(pool, cfg)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.OTPDevEcho {
		log.Print("WARNING: OTP_DEV_ECHO is on; verification codes are returned in API responses. Never use in production.")
	}
	if cfg.AppSecret == "dev-secret-change-me" {
		log.Print("WARNING: APP_SECRET is the development default.")
	}

	hub := httpapi.NewHub()
	go hub.Listen(ctx, cfg.ListenDatabaseURL)
	if cfg.EmbedWorker {
		go a.RunWorker(ctx, log.Printf)
	}

	srv := &http.Server{Addr: cfg.Addr, Handler: httpapi.NewServer(a, cfg, hub).Router(),
		ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		// Stop taking requests; open SSE streams end and clients reconnect.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("api listening on %s", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
