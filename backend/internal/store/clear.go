package store

import (
	"context"
	"fmt"
	"strings"
)

// Cleared counts what ClearTicker deleted.
type Cleared struct {
	Reports      int64 `json:"reports"`
	CacheEntries int64 `json:"cacheEntries"`
}

// ClearTicker deletes a ticker's finished reports and everything cached for
// it, so its next report is built from scratch: AI agent answers, the parsed
// sections and facts of the filings its reports used, and the SEC, price and
// estimate responses. A report still running is kept.
//
// Per-class EPS facts (sec:classfacts) are kept: they come from years of old
// filings, never change, and take minutes to read again. Verdict narratives
// are keyed by a hash of their input, not by ticker, so a changed report gets
// a new one anyway.
func (s *Reports) ClearTicker(ctx context.Context, ticker string) (Cleared, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	var c Cleared
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return c, fmt.Errorf("clear %s: %w", ticker, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The CIK and the filings referenced by this ticker's reports.
	var ciks, accessions []string
	err = tx.QueryRow(ctx, `
		SELECT coalesce(array_agg(DISTINCT cik) FILTER (WHERE cik IS NOT NULL), '{}'),
		       coalesce(array_agg(DISTINCT acc) FILTER (WHERE acc IS NOT NULL), '{}')
		FROM reports r
		CROSS JOIN LATERAL (SELECT r.body->'company'->>'cik' AS cik) c
		CROSS JOIN LATERAL (
			SELECT r.body->'filing'->>'accession' AS acc
			UNION SELECT p->>'accession' FROM jsonb_array_elements(coalesce(r.body->'financials'->'annual', '[]')) p
			UNION SELECT p->>'accession' FROM jsonb_array_elements(coalesce(r.body->'financials'->'quarterly', '[]')) p
			UNION SELECT r.body->'financials'->'ttm'->>'accession'
		) a
		WHERE r.ticker = $1`, ticker).Scan(&ciks, &accessions)
	if err != nil {
		return c, fmt.Errorf("clear %s: find filings: %w", ticker, err)
	}

	keys := []string{"alphavantage:earnings:" + ticker}
	for _, cik := range ciks {
		keys = append(keys, "sec:companyfacts:"+cik, "sec:submissions:"+cik)
	}
	for _, a := range accessions {
		keys = append(keys, "sec:sections:v1:"+a, "sec:filingfacts:v1:"+a, "sec:index:"+a)
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM http_cache
		WHERE key = ANY($2) OR key LIKE 'agent:' || $1 || ':%' OR key LIKE 'tiingo:daily:' || $1 || ':%'`,
		ticker, keys)
	if err != nil {
		return c, fmt.Errorf("clear %s: cache: %w", ticker, err)
	}
	c.CacheEntries = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM reports WHERE ticker = $1 AND status NOT IN ('queued', 'running')`, ticker)
	if err != nil {
		return c, fmt.Errorf("clear %s: reports: %w", ticker, err)
	}
	c.Reports = tag.RowsAffected()
	return c, tx.Commit(ctx)
}
