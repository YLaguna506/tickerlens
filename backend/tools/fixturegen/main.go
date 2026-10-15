// Command fixturegen records test fixtures in testdata/:
//
//   - edgar/company_tickers.json and edgar/companyfacts_<T>.json, trimmed to
//     the concepts the normalizer reads;
//   - edgar/classfacts_<T>.json for companies without plain EPS;
//   - tiingo/prices_<T>.json and alphavantage/quarters_<T>.json when the API
//     keys are set.
//
// Run it with `task fixtures`, which loads .env and uses the Postgres cache
// (so already-fetched data costs no API quota).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// Facts older than this are dropped; 10-year CAGRs need data back to ~2015.
const oldest = "2013-01-01"

// Extra tickers kept in the trimmed ticker list for lookup tests.
var extraTickers = []string{"BRK-B"}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fixturegen:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, tickers []string) error {
	cfg := ingest.Config{
		SECUserAgent:    os.Getenv("SEC_USER_AGENT"),
		TiingoKey:       os.Getenv("TIINGO_API_KEY"),
		AlphaVantageKey: os.Getenv("ALPHA_VANTAGE_API_KEY"),
	}
	if len(tickers) == 0 {
		return errors.New("usage: fixturegen TICKER [TICKER...]")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	var cache fetch.Cache
	var budget estimates.Budget
	if url := os.Getenv("DATABASE_URL"); url != "" {
		pool, err := store.Open(ctx, url)
		if err != nil {
			return err
		}
		defer pool.Close()
		cache, budget = store.NewPGCache(pool), store.NewUsage(pool)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	f := fetch.New(fetch.Options{
		Name:    "sec",
		Limiter: rate.NewLimiter(rate.Every(200*time.Millisecond), 1),
		Cache:   cache,
		Header:  http.Header{"User-Agent": {cfg.SECUserAgent}},
		Logger:  logger,
	})
	sec := edgar.NewClient(f, edgar.Options{Cache: cache})
	fetcher := ingest.NewFetcher(cfg, cache, budget, logger)

	if err := writeTickers(ctx, sec, tickers); err != nil {
		return err
	}
	for _, t := range tickers {
		if err := writeCompany(ctx, sec, fetcher, t); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
	}
	return nil
}

func writeTickers(ctx context.Context, sec *edgar.Client, tickers []string) error {
	all, err := sec.Tickers(ctx)
	if err != nil {
		return err
	}
	keep := append(slices.Clone(tickers), extraTickers...)
	subset := map[string]map[string]any{}
	for _, co := range all {
		if !slices.Contains(keep, co.Ticker) {
			continue
		}
		var cik int
		_, _ = fmt.Sscan(strings.TrimLeft(co.CIK, "0"), &cik)
		subset[fmt.Sprint(len(subset))] = map[string]any{"cik_str": cik, "ticker": co.Ticker, "title": co.Name}
	}
	return writeJSON(filepath.Join("testdata", "edgar", "company_tickers.json"), subset)
}

func writeCompany(ctx context.Context, sec *edgar.Client, fetcher *ingest.Fetcher, ticker string) error {
	co, err := sec.LookupTicker(ctx, ticker)
	if err != nil {
		return err
	}
	facts, err := sec.CompanyFacts(ctx, co.CIK)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join("testdata", "edgar", "companyfacts_"+co.Ticker+".json"), trim(facts)); err != nil {
		return err
	}

	if ingest.LacksRecentEPS(facts) {
		eps, shares, _, err := sec.ClassFacts(ctx, co.CIK, time.Now())
		if err != nil {
			return err
		}
		err = writeJSON(filepath.Join("testdata", "edgar", "classfacts_"+co.Ticker+".json"),
			map[string][]edgar.Fact{"eps": eps, "shares": shares})
		if err != nil {
			return err
		}
	}

	bundle, err := fetcher.Fetch(ctx, co.Ticker)
	if err != nil {
		return err
	}
	if len(bundle.Prices) > 0 {
		if err := writeJSON(filepath.Join("testdata", "tiingo", "prices_"+co.Ticker+".json"), bundle.Prices); err != nil {
			return err
		}
	}
	if len(bundle.Earnings) > 0 {
		// Parsed quarters (earnings_<T>.json holds raw API responses for the client test).
		if err := writeJSON(filepath.Join("testdata", "alphavantage", "quarters_"+co.Ticker+".json"), bundle.Earnings); err != nil {
			return err
		}
	}
	for _, w := range bundle.Warnings {
		fmt.Println("warning:", co.Ticker, w)
	}
	fmt.Println("recorded", co.Ticker)
	return nil
}

// trim keeps only the us-gaap concepts the normalizer reads, from 2013 on.
func trim(facts *edgar.CompanyFacts) *edgar.CompanyFacts {
	wanted := financials.Concepts()
	out := &edgar.CompanyFacts{
		CIK:        facts.CIK,
		EntityName: facts.EntityName,
		Facts:      map[string]map[string]edgar.Concept{"us-gaap": {}},
	}
	for name, c := range facts.Facts["us-gaap"] {
		if !slices.Contains(wanted, name) {
			continue
		}
		units := map[string][]edgar.Fact{}
		for unit, list := range c.Units {
			for _, fact := range list {
				if fact.End >= oldest {
					units[unit] = append(units[unit], fact)
				}
			}
		}
		out.Facts["us-gaap"][name] = edgar.Concept{Label: c.Label, Units: units}
	}
	return out
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
