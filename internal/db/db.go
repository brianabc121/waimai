// Package db owns the connection pool and embedded migrations.
package db

import (
	"context"
	"database/sql"
	"embed"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// migrateLockKey is the advisory lock that serialises concurrent migrators.
const migrateLockKey = 727301

// Migrate applies all embedded migrations. It is run by a one-off process
// (cmd/migrate), never by api or worker at startup. A session advisory lock
// makes two migrators that start at the same time (two deploy jobs, or test
// packages running in parallel) take turns instead of colliding.
func Migrate(ctx context.Context, url string) error {
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	lock, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer lock.Close()
	if _, err := lock.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrateLockKey); err != nil {
		return err
	}
	defer lock.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", migrateLockKey) //nolint:errcheck
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	// Several branches add migrations at the same time, so a lower number can
	// arrive after a higher one was applied; it is applied then, not refused.
	return goose.UpContext(ctx, sqlDB, "migrations", goose.WithAllowMissing())
}
