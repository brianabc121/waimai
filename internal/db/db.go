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

// SetAppRolePassword lets the application's restricted role waimai_app
// (migration 00085) log in with password. It needs a role that may alter
// roles: the books' owner when it created waimai_app, or a superuser.
func SetAppRolePassword(ctx context.Context, url, password string) error {
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	var quoted string
	if err := sqlDB.QueryRowContext(ctx, `SELECT quote_literal($1)`, password).Scan(&quoted); err != nil {
		return err
	}
	_, err = sqlDB.ExecContext(ctx, `ALTER ROLE waimai_app LOGIN PASSWORD `+quoted)
	return err
}
