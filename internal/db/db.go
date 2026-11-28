// Package db owns the connection pool and embedded migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

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
	if err := goose.UpContext(ctx, sqlDB, "migrations", goose.WithAllowMissing()); err != nil {
		return err
	}
	return grantAppRole(ctx, sqlDB)
}

// appRoleGrants gives waimai_app its rights: everything on the business
// tables, the books read-only plus the posting functions. Migration 00085
// did this once, and only when the role existed at that moment; a role that
// the operator creates later (a managed database that does not let the
// migration create roles) would be left with nothing. So every run of
// Migrate repeats it. Every statement is safe to repeat.
const appRoleGrants = `
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'waimai_app') THEN
        RETURN;
    END IF;
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO waimai_app', current_database());
    GRANT USAGE ON SCHEMA public TO waimai_app;
    GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO waimai_app;
    GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO waimai_app;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO waimai_app;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO waimai_app;
    REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON wallet_journals, wallet_postings, wallet_journal_rules,
        customer_wallet_entries, wallet_daily_balances, wallet_days_closed FROM waimai_app;
    REVOKE UPDATE, DELETE, TRUNCATE ON wallet_accounts FROM waimai_app;
    GRANT UPDATE (frozen, frozen_note, updated_at) ON wallet_accounts TO waimai_app;
    REVOKE ALL ON FUNCTION wallet_post_journal(TEXT, TEXT, BIGINT, TEXT, BIGINT, BIGINT, BIGINT, BIGINT, TEXT, BIGINT,
        BIGINT, TEXT, BIGINT, BIGINT[], BIGINT[], TIMESTAMPTZ) FROM PUBLIC;
    GRANT EXECUTE ON FUNCTION wallet_post_journal(TEXT, TEXT, BIGINT, TEXT, BIGINT, BIGINT, BIGINT, BIGINT, TEXT, BIGINT,
        BIGINT, TEXT, BIGINT, BIGINT[], BIGINT[], TIMESTAMPTZ) TO waimai_app;
    REVOKE ALL ON FUNCTION wallet_close_day(BIGINT, DATE) FROM PUBLIC;
    GRANT EXECUTE ON FUNCTION wallet_close_day(BIGINT, DATE) TO waimai_app;
END $$`

func grantAppRole(ctx context.Context, sqlDB *sql.DB) error {
	if _, err := sqlDB.ExecContext(ctx, appRoleGrants); err != nil {
		return fmt.Errorf("grant waimai_app: %w", err)
	}
	return nil
}

// AppRoleExists says whether the restricted role is there at all.
func AppRoleExists(ctx context.Context, url string) (bool, error) {
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		return false, err
	}
	defer sqlDB.Close()
	var ok bool
	err = sqlDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'waimai_app')`).Scan(&ok)
	return ok, err
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
