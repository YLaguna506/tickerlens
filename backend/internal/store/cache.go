package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGCache stores provider responses in the http_cache table.
// It satisfies fetch.Cache.
type PGCache struct {
	pool *pgxpool.Pool
}

// NewPGCache returns a cache backed by pool.
func NewPGCache(pool *pgxpool.Pool) *PGCache {
	return &PGCache{pool: pool}
}

// Get returns the cached body for key if it has not expired.
func (c *PGCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	var body []byte
	err := c.pool.QueryRow(ctx,
		"SELECT body FROM http_cache WHERE key = $1 AND expires_at > now()", key,
	).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cache get %q: %w", key, err)
	}
	return body, true, nil
}

// Set stores body under key for ttl, replacing any previous entry.
func (c *PGCache) Set(ctx context.Context, key string, body []byte, ttl time.Duration) error {
	_, err := c.pool.Exec(ctx, `
		INSERT INTO http_cache (key, body, fetched_at, expires_at)
		VALUES ($1, $2, now(), now() + $3::interval)
		ON CONFLICT (key) DO UPDATE
		SET body = EXCLUDED.body, fetched_at = EXCLUDED.fetched_at, expires_at = EXCLUDED.expires_at`,
		key, body, ttl)
	if err != nil {
		return fmt.Errorf("cache set %q: %w", key, err)
	}
	return nil
}
