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

// Screens is the repository for screens and their results.
type Screens struct {
	pool *pgxpool.Pool
}

// NewScreens returns a repository backed by pool.
func NewScreens(pool *pgxpool.Pool) *Screens {
	return &Screens{pool: pool}
}

// ScreenMember is one company in a screen's list.
type ScreenMember struct {
	Ticker string `json:"ticker"`
	Name   string `json:"name,omitempty"`
	Sector string `json:"sector,omitempty"` // GICS sector, when the list has it
}

// ScreenParams are a screen's settings.
type ScreenParams struct {
	// MinBusiness: companies whose business score (without the AI) is at
	// least this get their prices looked up.
	MinBusiness float64 `json:"minBusiness"`
	// MaxPriced caps the price lookups: the free price feed allows 50
	// requests an hour and 500 different symbols a month.
	MaxPriced int `json:"maxPriced"`
}

// Screen is one scan of a list of companies.
type Screen struct {
	ID         string         `json:"id"`
	Universe   string         `json:"universe"`
	Status     string         `json:"status"` // scanning | pricing | done | cancelled | failed
	Params     ScreenParams   `json:"params"`
	Members    []ScreenMember `json:"-"`
	Total      int            `json:"total"`
	Error      string         `json:"error,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
}

// ScreenResult is one company's numbers in a screen.
type ScreenResult struct {
	Ticker string `json:"ticker"`
	Name   string `json:"name"`
	Sector string `json:"sector,omitempty"`
	// Stage: scanned (fundamentals read), queued (waiting for its price),
	// priced, below (business score under the bar: not priced), capped (over
	// the price cap: not priced) or failed.
	Stage string `json:"stage"`
	Error string `json:"error,omitempty"`

	Kind     string   `json:"kind,omitempty"`  // bank, broker, insurer, reit
	Limited  bool     `json:"limited"`         // the metrics do not fit it
	Phase    string   `json:"phase,omitempty"` // lifecycle phase
	Business *float64 `json:"business"`        // growth and health, weighted (no AI)
	Growth   *float64 `json:"growth"`
	Health   *float64 `json:"health"`

	Valuation      *float64 `json:"valuation"` // V, 1–5
	Metric         string   `json:"metric,omitempty"`
	Zone           string   `json:"zone,omitempty"` // cheap, fair, expensive
	Percentile     *float64 `json:"percentile"`
	Current        *float64 `json:"current"`
	Price          *float64 `json:"price"`
	PriceDate      string   `json:"priceDate,omitempty"`
	CheapPrice     *float64 `json:"cheapPrice"`
	MedianPrice    *float64 `json:"medianPrice"`
	ExpensivePrice *float64 `json:"expensivePrice"`
	MarketCap      *float64 `json:"marketCap"`
	Expectations   string   `json:"expectations,omitempty"` // reverse DCF: low, in line, high, very high
}

// keepScreens is how many screens are kept; older ones are deleted.
const keepScreens = 5

// Create stores a new screen and returns it with its ID.
func (s *Screens) Create(ctx context.Context, universe string, params ScreenParams, members []ScreenMember) (Screen, error) {
	p, _ := json.Marshal(params)
	m, _ := json.Marshal(members)
	sc := Screen{Universe: universe, Status: "scanning", Params: params, Members: members, Total: len(members)}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO screens (universe, params, members) VALUES ($1, $2, $3)
		RETURNING id::text, created_at`, universe, p, m).Scan(&sc.ID, &sc.CreatedAt)
	if err != nil {
		return Screen{}, fmt.Errorf("create screen: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		DELETE FROM screens WHERE id NOT IN (SELECT id FROM screens ORDER BY created_at DESC LIMIT $1)`, keepScreens)
	if err != nil {
		return Screen{}, fmt.Errorf("prune screens: %w", err)
	}
	return sc, nil
}

const screenColumns = `id::text, universe, status, params, members, error, created_at, finished_at`

func scanScreen(row pgx.Row) (Screen, error) {
	var (
		sc        Screen
		p, m      []byte
		errorText string
	)
	if err := row.Scan(&sc.ID, &sc.Universe, &sc.Status, &p, &m, &errorText, &sc.CreatedAt, &sc.FinishedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Screen{}, ErrNotFound
		}
		return Screen{}, err
	}
	sc.Error = errorText
	if err := json.Unmarshal(p, &sc.Params); err != nil {
		return Screen{}, err
	}
	if err := json.Unmarshal(m, &sc.Members); err != nil {
		return Screen{}, err
	}
	sc.Total = len(sc.Members)
	return sc, nil
}

// Get returns a screen.
func (s *Screens) Get(ctx context.Context, id string) (Screen, error) {
	sc, err := scanScreen(s.pool.QueryRow(ctx, `SELECT `+screenColumns+` FROM screens WHERE id = $1`, id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Screen{}, fmt.Errorf("get screen: %w", err)
	}
	return sc, err
}

// Latest returns the most recent screen.
func (s *Screens) Latest(ctx context.Context) (Screen, error) {
	sc, err := scanScreen(s.pool.QueryRow(ctx, `SELECT `+screenColumns+` FROM screens ORDER BY created_at DESC LIMIT 1`))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Screen{}, fmt.Errorf("latest screen: %w", err)
	}
	return sc, err
}

// Unfinished returns the screens still scanning or pricing (to resume after
// a restart).
func (s *Screens) Unfinished(ctx context.Context) ([]Screen, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+screenColumns+` FROM screens WHERE status IN ('scanning', 'pricing') ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("unfinished screens: %w", err)
	}
	defer rows.Close()
	var out []Screen
	for rows.Next() {
		sc, err := scanScreen(rows)
		if err != nil {
			return nil, fmt.Errorf("unfinished screens: %w", err)
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// SetStatus records a screen's status; a final status also sets finished_at.
func (s *Screens) SetStatus(ctx context.Context, id, status, errorText string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE screens SET status = $2, error = $3,
		       finished_at = CASE WHEN $2 IN ('done', 'cancelled', 'failed') THEN now() ELSE NULL END
		WHERE id = $1`, id, status, errorText)
	if err != nil {
		return fmt.Errorf("set screen status: %w", err)
	}
	return nil
}

// SaveResult stores (or replaces) one company's result.
func (s *Screens) SaveResult(ctx context.Context, id string, r ScreenResult) error {
	b, _ := json.Marshal(r)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO screen_results (screen_id, ticker, result) VALUES ($1, $2, $3)
		ON CONFLICT (screen_id, ticker) DO UPDATE SET result = EXCLUDED.result, updated_at = now()`, id, r.Ticker, b)
	if err != nil {
		return fmt.Errorf("save screen result: %w", err)
	}
	return nil
}

// Results returns a screen's results, in no particular order.
func (s *Screens) Results(ctx context.Context, id string) ([]ScreenResult, error) {
	rows, err := s.pool.Query(ctx, `SELECT result FROM screen_results WHERE screen_id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("screen results: %w", err)
	}
	defer rows.Close()
	out := []ScreenResult{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, fmt.Errorf("screen results: %w", err)
		}
		var r ScreenResult
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("screen results: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
