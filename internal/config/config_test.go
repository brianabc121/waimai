package config

import "testing"

func TestPooledDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@pool:6432/db?sslmode=require")
	t.Setenv("LISTEN_DATABASE_URL", "")
	t.Setenv("DIRECT_DATABASE_URL", "")
	if c := Load(); c.DatabaseURL != "postgres://u:p@pool:6432/db?sslmode=require" || c.ListenDatabaseURL != c.DatabaseURL {
		t.Fatalf("without a direct URL nothing changes: %+v", c)
	}

	t.Setenv("DIRECT_DATABASE_URL", "postgres://u:p@db:5432/db?sslmode=require")
	c := Load()
	if c.DatabaseURL != "postgres://u:p@pool:6432/db?sslmode=require&default_query_exec_mode=cache_describe" {
		t.Fatalf("pooled URL: %s", c.DatabaseURL)
	}
	if c.ListenDatabaseURL != "postgres://u:p@db:5432/db?sslmode=require" {
		t.Fatalf("direct URL: %s", c.ListenDatabaseURL)
	}

	// the older variable name still works, and an explicit mode is kept
	t.Setenv("DIRECT_DATABASE_URL", "")
	t.Setenv("LISTEN_DATABASE_URL", "postgres://u:p@db:5432/db")
	t.Setenv("DATABASE_URL", "postgres://u:p@pool:6432/db?default_query_exec_mode=exec")
	if c := Load(); c.DatabaseURL != "postgres://u:p@pool:6432/db?default_query_exec_mode=exec" || c.ListenDatabaseURL != "postgres://u:p@db:5432/db" {
		t.Fatalf("legacy name / explicit mode: %+v", c)
	}
	if got := PoolerSafe("postgres://u:p@pool/db"); got != "postgres://u:p@pool/db?default_query_exec_mode=cache_describe" {
		t.Fatalf("no query string: %s", got)
	}
}
