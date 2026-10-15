package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Report statuses.
const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusPartial = "partial" // finished, but some sections are missing
	StatusFailed  = "failed"
)

// ErrNotFound is returned when a report does not exist.
var ErrNotFound = errors.New("not found")

// Report is one row of the reports table.
type Report struct {
	ID             string          `json:"id"`
	Ticker         string          `json:"ticker"`
	CompanyName    string          `json:"companyName"`
	Status         string          `json:"status"`
	Stage          string          `json:"stage"`
	Error          string          `json:"error,omitempty"`
	FailedSections []string        `json:"failedSections"`
	CacheKey       string          `json:"-"`
	ReusedFrom     *string         `json:"reusedFrom,omitempty"`
	Body           json.RawMessage `json:"report,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	FinishedAt     *time.Time      `json:"finishedAt,omitempty"`
}

// Finished reports whether the report has stopped running.
func (r Report) Finished() bool {
	return r.Status == StatusDone || r.Status == StatusPartial || r.Status == StatusFailed
}

// Reports is the repository for the reports table.
type Reports struct {
	pool *pgxpool.Pool
}

// NewReports returns a repository backed by pool.
func NewReports(pool *pgxpool.Pool) *Reports {
	return &Reports{pool: pool}
}

const reportColumns = `id::text, ticker, company_name, status, stage, error, failed_sections, cache_key,
	reused_from::text, body, created_at, updated_at, finished_at`

// scanReport reads one row in reportColumns order.
func scanReport(row pgx.Row) (Report, error) {
	var r Report
	err := row.Scan(&r.ID, &r.Ticker, &r.CompanyName, &r.Status, &r.Stage, &r.Error, &r.FailedSections,
		&r.CacheKey, &r.ReusedFrom, &r.Body, &r.CreatedAt, &r.UpdatedAt, &r.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// Create inserts a queued report for ticker.
func (s *Reports) Create(ctx context.Context, ticker, companyName string) (Report, error) {
	r, err := scanReport(s.pool.QueryRow(ctx,
		`INSERT INTO reports (ticker, company_name) VALUES ($1, $2) RETURNING `+reportColumns, ticker, companyName))
	if err != nil {
		return r, fmt.Errorf("create report: %w", err)
	}
	return r, nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Get returns a report by id (ErrNotFound for unknown or malformed ids).
func (s *Reports) Get(ctx context.Context, id string) (Report, error) {
	if !uuidRe.MatchString(id) {
		return Report{}, ErrNotFound
	}
	r, err := scanReport(s.pool.QueryRow(ctx, `SELECT `+reportColumns+` FROM reports WHERE id = $1`, id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return r, fmt.Errorf("get report %s: %w", id, err)
	}
	return r, err
}

// SetStage marks the report running at the given stage.
func (s *Reports) SetStage(ctx context.Context, id, stage string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE reports SET status = 'running', stage = $2, updated_at = now() WHERE id = $1`, id, stage)
	if err != nil {
		return fmt.Errorf("set stage %s: %w", id, err)
	}
	return nil
}

// SaveBody stores the (possibly partial) report JSON.
func (s *Reports) SaveBody(ctx context.Context, id string, body json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `UPDATE reports SET body = $2, updated_at = now() WHERE id = $1`, id, body)
	if err != nil {
		return fmt.Errorf("save report %s: %w", id, err)
	}
	return nil
}

// SetCacheKey records which data the report is built from.
func (s *Reports) SetCacheKey(ctx context.Context, id, key string) error {
	_, err := s.pool.Exec(ctx, `UPDATE reports SET cache_key = $2, updated_at = now() WHERE id = $1`, id, key)
	if err != nil {
		return fmt.Errorf("set cache key %s: %w", id, err)
	}
	return nil
}

// Outcome is how a report finished.
type Outcome struct {
	Status         string
	Error          string
	FailedSections []string
	ReusedFrom     string // id of the report whose body was reused, if any
}

// Finish records the final status. The body is saved separately.
func (s *Reports) Finish(ctx context.Context, id string, o Outcome) error {
	if o.FailedSections == nil {
		o.FailedSections = []string{}
	}
	var reused *string
	if o.ReusedFrom != "" {
		reused = &o.ReusedFrom
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE reports
		SET status = $2, stage = 'done', error = $3, failed_sections = $4, reused_from = $5,
		    updated_at = now(), finished_at = now()
		WHERE id = $1`,
		id, o.Status, o.Error, o.FailedSections, reused)
	if err != nil {
		return fmt.Errorf("finish report %s: %w", id, err)
	}
	return nil
}

// LatestFinished returns the newest done or partial report for ticker
// created after since, or ErrNotFound.
func (s *Reports) LatestFinished(ctx context.Context, ticker string, since time.Time) (Report, error) {
	r, err := scanReport(s.pool.QueryRow(ctx, `
		SELECT `+reportColumns+` FROM reports
		WHERE ticker = $1 AND status IN ('done', 'partial') AND created_at > $2
		ORDER BY created_at DESC LIMIT 1`, ticker, since))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return r, fmt.Errorf("latest report %s: %w", ticker, err)
	}
	return r, err
}

// FindByCacheKey returns the newest finished, complete report for ticker built
// from the same data, or ErrNotFound.
func (s *Reports) FindByCacheKey(ctx context.Context, ticker, key, excludeID string) (Report, error) {
	r, err := scanReport(s.pool.QueryRow(ctx, `
		SELECT `+reportColumns+` FROM reports
		WHERE ticker = $1 AND cache_key = $2 AND cache_key <> '' AND status = 'done' AND id::text <> $3
		ORDER BY created_at DESC LIMIT 1`, ticker, key, excludeID))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return r, fmt.Errorf("find report by key: %w", err)
	}
	return r, err
}

// ListByTicker returns the newest reports for ticker, without their bodies.
func (s *Reports) ListByTicker(ctx context.Context, ticker string, limit int) ([]Report, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, ticker, company_name, status, stage, error, failed_sections, cache_key,
		       reused_from::text, NULL::jsonb, created_at, updated_at, finished_at
		FROM reports WHERE ticker = $1 ORDER BY created_at DESC LIMIT $2`, ticker, limit)
	if err != nil {
		return nil, fmt.Errorf("list reports %s: %w", ticker, err)
	}
	defer rows.Close()
	out := []Report{}
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, fmt.Errorf("list reports %s: %w", ticker, err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkInterrupted fails every report that was still running when the API
// stopped. Called once at startup; the user can re-run them.
func (s *Reports) MarkInterrupted(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE reports
		SET status = 'failed', error = 'interrupted: the API restarted while this report was running',
		    updated_at = now(), finished_at = now()
		WHERE status IN ('queued', 'running')`)
	if err != nil {
		return 0, fmt.Errorf("mark interrupted: %w", err)
	}
	return tag.RowsAffected(), nil
}
