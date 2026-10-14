package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Usage counts requests per provider per day, for providers with a daily quota.
type Usage struct {
	pool *pgxpool.Pool
}

// NewUsage returns a Usage backed by pool.
func NewUsage(pool *pgxpool.Pool) *Usage {
	return &Usage{pool: pool}
}

// Take records one request for provider on day if fewer than limit have been
// made, and reports whether it was allowed. The check and the increment happen
// in one statement, so concurrent callers cannot overshoot the limit.
func (u *Usage) Take(ctx context.Context, provider string, day time.Time, limit int) (bool, error) {
	var count int
	err := u.pool.QueryRow(ctx, `
		INSERT INTO provider_usage (provider, day, count) VALUES ($1, $2, 1)
		ON CONFLICT (provider, day) DO UPDATE
		SET count = provider_usage.count + 1
		WHERE provider_usage.count < $3
		RETURNING count`,
		provider, day.Format(time.DateOnly), limit,
	).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // the WHERE clause blocked the update: limit reached
	}
	if err != nil {
		return false, fmt.Errorf("usage take %s: %w", provider, err)
	}
	return true, nil
}
