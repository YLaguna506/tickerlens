package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Backtests is the repository for backtests and their results.
type Backtests struct {
	pool *pgxpool.Pool
}

// NewBacktests returns a repository backed by pool.
func NewBacktests(pool *pgxpool.Pool) *Backtests {
	return &Backtests{pool: pool}
}

// BacktestParams are a backtest's settings.
type BacktestParams struct {
	Dates          []string `json:"dates"` // as-of dates, oldest first
	ScoringVersion string   `json:"scoringVersion"`
	Benchmark      string   `json:"benchmark"` // e.g. SPY
}

// Backtest is one run.
type Backtest struct {
	ID         string         `json:"id"`
	Universe   string         `json:"universe"`
	Status     string         `json:"status"` // running | done | cancelled | failed
	Params     BacktestParams `json:"params"`
	Members    []ScreenMember `json:"-"`
	Total      int            `json:"total"`
	Error      string         `json:"error,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
}

// Observation is one company's verdict as of one date, and what its stock
// did afterwards.
type Observation struct {
	Ticker string `json:"ticker"`
	Date   string `json:"date"` // as of
	Sector string `json:"sector,omitempty"`
	Kind   string `json:"kind,omitempty"` // bank, broker, insurer, reit

	Label     string   `json:"label"` // Buy … Sell, without the AI
	Business  *float64 `json:"business"`
	Growth    *float64 `json:"growth"`
	Health    *float64 `json:"health"`
	Valuation *float64 `json:"valuation"`
	// Percentile is where the multiple sat in its own history (0 cheapest).
	Percentile   *float64 `json:"percentile"`
	Zone         string   `json:"zone,omitempty"`
	Metric       string   `json:"metric,omitempty"`
	Expectations string   `json:"expectations,omitempty"`
	Price        float64  `json:"price"`

	// Return is the total return (dividends reinvested) over 1, 2 and 3
	// years; Excess subtracts the benchmark's. Nil when not yet known.
	Return [3]*float64 `json:"return"`
	Excess [3]*float64 `json:"excess"`
}

// BacktestResult is one company's observations.
type BacktestResult struct {
	Ticker       string        `json:"ticker"`
	Name         string        `json:"name"`
	Error        string        `json:"error,omitempty"`
	Observations []Observation `json:"observations"`
}

// keepBacktests is how many backtests are kept; older ones are deleted.
const keepBacktests = 5

// Create stores a new backtest and returns it with its ID.
func (s *Backtests) Create(ctx context.Context, universe string, params BacktestParams, members []ScreenMember) (Backtest, error) {
	p, _ := json.Marshal(params)
	m, _ := json.Marshal(members)
	bt := Backtest{Universe: universe, Status: "running", Params: params, Members: members, Total: len(members)}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO backtests (universe, params, members) VALUES ($1, $2, $3)
		RETURNING id::text, created_at`, universe, p, m).Scan(&bt.ID, &bt.CreatedAt)
	if err != nil {
		return Backtest{}, fmt.Errorf("create backtest: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		DELETE FROM backtests WHERE id NOT IN (SELECT id FROM backtests ORDER BY created_at DESC LIMIT $1)`, keepBacktests)
	if err != nil {
		return Backtest{}, fmt.Errorf("prune backtests: %w", err)
	}
	return bt, nil
}

const backtestColumns = `id::text, universe, status, params, members, error, created_at, finished_at`

func scanBacktest(row pgx.Row) (Backtest, error) {
	var (
		bt   Backtest
		p, m []byte
	)
	if err := row.Scan(&bt.ID, &bt.Universe, &bt.Status, &p, &m, &bt.Error, &bt.CreatedAt, &bt.FinishedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backtest{}, ErrNotFound
		}
		return Backtest{}, err
	}
	if err := json.Unmarshal(p, &bt.Params); err != nil {
		return Backtest{}, err
	}
	if err := json.Unmarshal(m, &bt.Members); err != nil {
		return Backtest{}, err
	}
	bt.Total = len(bt.Members)
	return bt, nil
}

// Get returns a backtest.
func (s *Backtests) Get(ctx context.Context, id string) (Backtest, error) {
	bt, err := scanBacktest(s.pool.QueryRow(ctx, `SELECT `+backtestColumns+` FROM backtests WHERE id = $1`, id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Backtest{}, fmt.Errorf("get backtest: %w", err)
	}
	return bt, err
}

// Latest returns the most recent backtest.
func (s *Backtests) Latest(ctx context.Context) (Backtest, error) {
	bt, err := scanBacktest(s.pool.QueryRow(ctx, `SELECT `+backtestColumns+` FROM backtests ORDER BY created_at DESC LIMIT 1`))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Backtest{}, fmt.Errorf("latest backtest: %w", err)
	}
	return bt, err
}

// Unfinished returns the backtests still running (to resume after a restart).
func (s *Backtests) Unfinished(ctx context.Context) ([]Backtest, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+backtestColumns+` FROM backtests WHERE status = 'running' ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("unfinished backtests: %w", err)
	}
	defer rows.Close()
	var out []Backtest
	for rows.Next() {
		bt, err := scanBacktest(rows)
		if err != nil {
			return nil, fmt.Errorf("unfinished backtests: %w", err)
		}
		out = append(out, bt)
	}
	return out, rows.Err()
}

// SetStatus records a backtest's status; a final status sets finished_at.
func (s *Backtests) SetStatus(ctx context.Context, id, status, errorText string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE backtests SET status = $2, error = $3,
		       finished_at = CASE WHEN $2 IN ('done', 'cancelled', 'failed') THEN now() ELSE NULL END
		WHERE id = $1`, id, status, errorText)
	if err != nil {
		return fmt.Errorf("set backtest status: %w", err)
	}
	return nil
}

// SaveResult stores (or replaces) one company's result.
func (s *Backtests) SaveResult(ctx context.Context, id string, r BacktestResult) error {
	b, _ := json.Marshal(r)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO backtest_results (backtest_id, ticker, result) VALUES ($1, $2, $3)
		ON CONFLICT (backtest_id, ticker) DO UPDATE SET result = EXCLUDED.result, updated_at = now()`, id, r.Ticker, b)
	if err != nil {
		return fmt.Errorf("save backtest result: %w", err)
	}
	return nil
}

// Results returns a backtest's results, in no particular order.
func (s *Backtests) Results(ctx context.Context, id string) ([]BacktestResult, error) {
	rows, err := s.pool.Query(ctx, `SELECT result FROM backtest_results WHERE backtest_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("backtest results: %w", err)
	}
	defer rows.Close()
	out := []BacktestResult{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, fmt.Errorf("backtest results: %w", err)
		}
		var r BacktestResult
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("backtest results: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
