package db

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
)

// M18: a waimai_app role that had no rights when the migrations ran (it was
// created by hand afterwards) gets them from the next run of Migrate: it can
// then work with the business tables and still cannot write the books.
func TestMigrateGrantsAppRoleEveryRun(t *testing.T) {
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var may bool
	if err := admin.QueryRowContext(ctx, `SELECT (rolsuper OR rolcreatedb) AND (rolsuper OR rolcreaterole)
		FROM pg_roles WHERE rolname = current_user`).Scan(&may); err != nil {
		t.Fatal(err)
	}
	if !may {
		t.Skip("the test user may not create databases and roles")
	}
	const name = "waimai_grant_test"
	admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+name) //nolint:errcheck
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`) }) //nolint:errcheck
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	target := u.String()
	if err := Migrate(ctx, target); err != nil {
		t.Fatal(err)
	}
	d, err := sql.Open("pgx", target)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// Put the role where a hand-made one starts: no rights in this database.
	for _, q := range []string{
		`REVOKE ALL ON ALL TABLES IN SCHEMA public FROM waimai_app`,
		`REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM waimai_app`,
		`REVOKE ALL ON ALL FUNCTIONS IN SCHEMA public FROM waimai_app`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM waimai_app`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM waimai_app`,
		`REVOKE USAGE ON SCHEMA public FROM waimai_app`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	as := func(q string) error {
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback() //nolint:errcheck
		if _, err := tx.ExecContext(ctx, `SET LOCAL ROLE waimai_app`); err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(ctx, q)
		return err
	}
	const write = `INSERT INTO jobs (kind, payload) VALUES ('sms', '{}')`
	if err := as(write); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("a role without rights wrote a table: %v", err)
	}

	// migrate again: nothing new to apply, but the rights come back
	if err := Migrate(ctx, target); err != nil {
		t.Fatal(err)
	}
	if err := as(write); err != nil {
		t.Fatalf("after migrate the app role cannot write a business table: %v", err)
	}
	if err := as(`SELECT count(*) FROM wallet_journals`); err != nil {
		t.Fatalf("the app role cannot read the books: %v", err)
	}
	for _, q := range []string{
		`DELETE FROM wallet_journals`,
		`UPDATE wallet_accounts SET balance = balance + 1`,
		`INSERT INTO wallet_days_closed SELECT * FROM wallet_days_closed`,
	} {
		if err := as(q); err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("the app role may %q: %v", q, err)
		}
	}
}
