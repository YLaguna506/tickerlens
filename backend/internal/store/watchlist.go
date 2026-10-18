package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Watchlist is the repository for the watchlist table.
type Watchlist struct {
	pool *pgxpool.Pool
}

// NewWatchlist returns a repository backed by pool.
func NewWatchlist(pool *pgxpool.Pool) *Watchlist {
	return &Watchlist{pool: pool}
}

// WatchSummary is what the watchlist shows from a ticker's latest finished
// report.
type WatchSummary struct {
	ReportID   string     `json:"reportId"`
	Status     string     `json:"status"` // done | partial
	Lang       string     `json:"lang"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`

	Verdict    string   `json:"verdict,omitempty"` // Buy, Watch, Hold, Pass, Sell
	Business   *float64 `json:"business"`          // B, 1–5
	Valuation  *float64 `json:"valuation"`         // V, 1–5
	Price      *float64 `json:"price"`
	PriceDate  string   `json:"priceDate,omitempty"`
	Phase      string   `json:"phase,omitempty"`
	Moat       string   `json:"moat,omitempty"` // None … Wide
	RiskScore  *float64 `json:"riskScore"`      // 1–5, 5 = lowest risk
	Bands      *Bands   `json:"bands"`          // nil when valuation was not possible
	NoBandsWhy string   `json:"noBandsWhy,omitempty"`
}

// Bands is where the price sits in the company's own valuation history.
type Bands struct {
	Metric         string  `json:"metric"`
	Zone           string  `json:"zone"`       // cheap | fair | expensive
	Percentile     float64 `json:"percentile"` // share of history at or below today (0–1)
	Current        float64 `json:"current"`
	CheapPrice     float64 `json:"cheapPrice"`
	MedianPrice    float64 `json:"medianPrice"`
	ExpensivePrice float64 `json:"expensivePrice"`
}

// WatchItem is one watched ticker with its latest report.
type WatchItem struct {
	Ticker      string        `json:"ticker"`
	CompanyName string        `json:"companyName"`
	AddedAt     time.Time     `json:"addedAt"`
	Latest      *WatchSummary `json:"latest"` // nil before the first finished report
	// NextEarnings is the next quarterly report: from the latest report, or
	// the earnings calendar when the API has it (nil if unknown).
	NextEarnings *NextEarnings `json:"nextEarnings"`
	// Running is a report being built right now, if any.
	Running *RunningReport `json:"running,omitempty"`
}

// NextEarnings is a company's next earnings date.
type NextEarnings struct {
	Date        string   `json:"date"`
	Time        string   `json:"time,omitempty"` // pre-market / post-market
	Source      string   `json:"source"`         // calendar | estimated
	EstimateEPS *float64 `json:"estimateEps"`
}

// RunningReport is a queued or running report.
type RunningReport struct {
	ID    string `json:"id"`
	Stage string `json:"stage"`
}

// Add puts a ticker on the watchlist (a no-op if it is already there).
func (w *Watchlist) Add(ctx context.Context, ticker, companyName string) error {
	_, err := w.pool.Exec(ctx, `
		INSERT INTO watchlist (ticker, company_name) VALUES ($1, $2)
		ON CONFLICT (ticker) DO UPDATE SET company_name = EXCLUDED.company_name`,
		strings.ToUpper(ticker), companyName)
	if err != nil {
		return fmt.Errorf("add to watchlist: %w", err)
	}
	return nil
}

// Remove takes a ticker off the watchlist; it reports whether it was there.
func (w *Watchlist) Remove(ctx context.Context, ticker string) (bool, error) {
	tag, err := w.pool.Exec(ctx, `DELETE FROM watchlist WHERE ticker = $1`, strings.ToUpper(ticker))
	if err != nil {
		return false, fmt.Errorf("remove from watchlist: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Contains reports whether a ticker is on the watchlist.
func (w *Watchlist) Contains(ctx context.Context, ticker string) (bool, error) {
	var ok bool
	err := w.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM watchlist WHERE ticker = $1)`, strings.ToUpper(ticker)).Scan(&ok)
	return ok, err
}

// List returns the watchlist in the order tickers were added, each with its
// latest finished report (in any language or model) and any report being
// built. The summary is read straight from the report JSON.
func (w *Watchlist) List(ctx context.Context) ([]WatchItem, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT w.ticker, w.company_name, w.added_at,
		       f.id::text, f.status, f.lang, f.created_at, f.finished_at,
		       f.body->'verdict'->>'label',
		       (f.body->'verdict'->>'business')::float8,
		       (f.body->'verdict'->>'valuation')::float8,
		       (f.body->'metrics'->>'price')::float8,
		       f.body->'metrics'->>'priceDate',
		       f.body->'metrics'->'phase'->>'name',
		       f.body->'moat'->>'widthName',
		       (f.body->'risk'->>'score')::float8,
		       f.body->'metrics'->'valuationBands',
		       f.body->'metrics'->'valuationScore'->'reasons'->>0,
		       f.body->'metrics'->'nextEarnings',
		       r.id::text, r.stage
		FROM watchlist w
		LEFT JOIN LATERAL (
		    SELECT id, status, lang, created_at, finished_at, body FROM reports
		    WHERE ticker = w.ticker AND status IN ('done', 'partial') AND body IS NOT NULL
		    ORDER BY created_at DESC LIMIT 1
		) f ON true
		LEFT JOIN LATERAL (
		    SELECT id, stage FROM reports
		    WHERE ticker = w.ticker AND status IN ('queued', 'running')
		    ORDER BY created_at DESC LIMIT 1
		) r ON true
		ORDER BY w.added_at, w.ticker`)
	if err != nil {
		return nil, fmt.Errorf("list watchlist: %w", err)
	}
	defer rows.Close()

	items := []WatchItem{}
	for rows.Next() {
		var (
			it                                     WatchItem
			id, status, lang                       *string
			created                                *time.Time
			s                                      WatchSummary
			verdict, priceDate, phase, moat, noWhy *string
			bands, next                            []byte
			runID, runStage                        *string
		)
		if err := rows.Scan(&it.Ticker, &it.CompanyName, &it.AddedAt,
			&id, &status, &lang, &created, &s.FinishedAt,
			&verdict, &s.Business, &s.Valuation, &s.Price, &priceDate, &phase, &moat, &s.RiskScore,
			&bands, &noWhy, &next, &runID, &runStage); err != nil {
			return nil, fmt.Errorf("list watchlist: %w", err)
		}
		if id != nil {
			s.ReportID, s.Status, s.Lang, s.CreatedAt = *id, deref(status), deref(lang), *created
			s.Verdict, s.PriceDate, s.Phase, s.Moat = deref(verdict), deref(priceDate), deref(phase), deref(moat)
			if len(bands) > 0 && string(bands) != "null" {
				var b Bands
				if json.Unmarshal(bands, &b) == nil && b.Metric != "" {
					s.Bands = &b
				}
			}
			if s.Bands == nil {
				s.NoBandsWhy = deref(noWhy)
			}
			it.Latest = &s
			if len(next) > 0 && string(next) != "null" {
				var n NextEarnings
				if json.Unmarshal(next, &n) == nil && n.Date != "" {
					it.NextEarnings = &n
				}
			}
		}
		if runID != nil {
			it.Running = &RunningReport{ID: *runID, Stage: deref(runStage)}
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
