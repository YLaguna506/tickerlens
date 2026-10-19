package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// openTestDB connects to TEST_DATABASE_URL, or skips the test when it is unset
// so `go test ./...` works without a database. CI sets it.
func openTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres test")
	}
	pool, err := Open(t.Context(), url)
	if err != nil && strings.Contains(err.Error(), "does not exist") {
		// A separate test database keeps tests away from real data (Create
		// prunes old screens and backtests): create it on first use.
		if err := createDatabase(t.Context(), url); err != nil {
			t.Fatalf("create test database: %v", err)
		}
		pool, err = Open(t.Context(), url)
	}
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, "DELETE FROM http_cache WHERE key LIKE 'test:%'")
		_, _ = pool.Exec(ctx, "DELETE FROM provider_usage WHERE provider LIKE 'test-%'")
		pool.Close()
	})
	return pool
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := openTestDB(t)
	// Open already migrated once; a second run must be a no-op.
	if err := migrate(t.Context(), pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestPGCache(t *testing.T) {
	cache := NewPGCache(openTestDB(t))
	ctx := t.Context()

	if _, ok, err := cache.Get(ctx, "test:missing"); err != nil || ok {
		t.Fatalf("missing key: ok=%v err=%v", ok, err)
	}

	if err := cache.Set(ctx, "test:k", []byte("v1"), time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(ctx, "test:k", []byte("v2"), time.Hour); err != nil {
		t.Fatal(err)
	}
	body, ok, err := cache.Get(ctx, "test:k")
	if err != nil || !ok || string(body) != "v2" {
		t.Fatalf("get = %q, %v, %v; want v2", body, ok, err)
	}

	if err := cache.Set(ctx, "test:expired", []byte("x"), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok, _ := cache.Get(ctx, "test:expired"); ok {
		t.Error("expired entry was returned")
	}
}

func TestUsageTakeStopsAtLimit(t *testing.T) {
	usage := NewUsage(openTestDB(t))
	day := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	provider := "test-" + time.Now().Format("150405.000000")

	for i := range 3 {
		ok, err := usage.Take(t.Context(), provider, day, 3)
		if err != nil || !ok {
			t.Fatalf("take %d: ok=%v err=%v", i+1, ok, err)
		}
	}
	ok, err := usage.Take(t.Context(), provider, day, 3)
	if err != nil || ok {
		t.Fatalf("take beyond limit: ok=%v err=%v; want false", ok, err)
	}
	// A new day starts a new count.
	ok, err = usage.Take(t.Context(), provider, day.AddDate(0, 0, 1), 3)
	if err != nil || !ok {
		t.Fatalf("next day: ok=%v err=%v", ok, err)
	}
}

// createDatabase creates the database named in url, connecting to the
// server's default "postgres" database to do it.
func createDatabase(ctx context.Context, url string) error {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return err
	}
	name := cfg.Database
	cfg.Database = "postgres"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	return err
}
