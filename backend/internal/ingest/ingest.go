// Package ingest gathers everything TickerLens knows about a ticker from its
// data providers: SEC financials, Tiingo prices and Alpha Vantage earnings.
package ingest

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
)

// The provider interfaces are declared here, where they are used. Any type
// with these methods qualifies (the real clients, or fakes in tests), which is
// also how a paid provider would be swapped in later.

// Fundamentals provides company identity and XBRL facts (SEC EDGAR).
type Fundamentals interface {
	LookupTicker(ctx context.Context, ticker string) (edgar.Company, error)
	CompanyFacts(ctx context.Context, cik string) (*edgar.CompanyFacts, error)
}

// PriceSource provides daily price history (Tiingo).
type PriceSource interface {
	DailyPrices(ctx context.Context, ticker string, start time.Time) ([]prices.Bar, error)
}

// EarningsSource provides EPS actuals vs estimates (Alpha Vantage).
type EarningsSource interface {
	Earnings(ctx context.Context, ticker string) ([]estimates.QuarterlyEarnings, error)
}

// ClassFactsSource reads per-share-class EPS and shares from filing documents,
// for companies whose companyfacts lack them (edgar.Client implements it).
type ClassFactsSource interface {
	ClassFacts(ctx context.Context, cik string, now time.Time) (eps, shares []edgar.Fact, notes []string, err error)
}

// Fetcher gathers a Bundle. Prices and Earnings may be nil when no API key is
// configured; the bundle then says so in Warnings.
type Fetcher struct {
	Fundamentals Fundamentals
	ClassFacts   ClassFactsSource // optional
	Prices       PriceSource
	Earnings     EarningsSource
	PriceYears   int              // history to fetch; default 5
	Now          func() time.Time // default time.Now
}

// Bundle is the raw material for a report.
type Bundle struct {
	Company    edgar.Company                 `json:"company"`
	Financials financials.Financials         `json:"financials"`
	Prices     []prices.Bar                  `json:"prices"`
	Earnings   []estimates.QuarterlyEarnings `json:"earnings"`
	// Warnings lists data sources that failed or were skipped. Financials are
	// required; prices and earnings are best-effort so a report can still be
	// produced (marked partial).
	Warnings []string `json:"warnings,omitempty"`
	// Notes explain unusual but successful data handling.
	Notes []string `json:"notes,omitempty"`
}

// lacksRecentEPS reports whether companyfacts has no diluted EPS within
// ~7 months of the latest net income figure.
func lacksRecentEPS(cf *edgar.CompanyFacts) bool {
	gaap := cf.Facts["us-gaap"]
	latest := func(concept, unit string) string {
		var max string
		for _, f := range gaap[concept].Units[unit] {
			if f.End > max {
				max = f.End
			}
		}
		return max
	}
	epsEnd := latest("EarningsPerShareDiluted", "USD/shares")
	if epsEnd == "" {
		return true
	}
	incomeEnd := latest("NetIncomeLoss", "USD")
	if incomeEnd == "" {
		return false
	}
	eps, err1 := time.Parse(time.DateOnly, epsEnd)
	income, err2 := time.Parse(time.DateOnly, incomeEnd)
	return err1 == nil && err2 == nil && income.Sub(eps) > 210*24*time.Hour
}

// Fetch resolves the ticker and fetches all sources concurrently.
func (f *Fetcher) Fetch(ctx context.Context, ticker string) (*Bundle, error) {
	co, err := f.Fundamentals.LookupTicker(ctx, ticker)
	if err != nil {
		return nil, err
	}
	b := &Bundle{Company: co}

	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	years := f.PriceYears
	if years <= 0 {
		years = 5
	}

	// errgroup runs functions in goroutines and waits for all of them, like
	// Task.WhenAll. If one returns an error, ctx is cancelled for the others.
	// Only the financials return errors; price and earnings failures are kept
	// as warnings so they do not cancel the rest.
	var (
		facts       *edgar.CompanyFacts
		priceErr    error
		earningsErr error
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		facts, err = f.Fundamentals.CompanyFacts(gctx, co.CIK)
		return err
	})
	if f.Prices != nil {
		g.Go(func() error {
			b.Prices, priceErr = f.Prices.DailyPrices(gctx, co.Ticker, now().AddDate(-years, 0, 0))
			return nil
		})
	}
	if f.Earnings != nil {
		g.Go(func() error {
			b.Earnings, earningsErr = f.Earnings.Earnings(gctx, co.Ticker)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("financials for %s: %w", co.Ticker, err)
	}

	// Only companies whose companyfacts lack recent EPS (several share
	// classes) need their filing documents read; for most, this is skipped.
	if f.ClassFacts != nil && lacksRecentEPS(facts) {
		eps, shares, notes, err := f.ClassFacts.ClassFacts(ctx, co.CIK, now())
		if err != nil {
			b.Warnings = append(b.Warnings, "per-class EPS: "+err.Error())
		} else {
			edgar.MergeClassFacts(facts, eps, shares)
			b.Notes = append(b.Notes, notes...)
		}
	}

	b.Financials = financials.Normalize(co, facts)

	switch {
	case f.Prices == nil:
		b.Warnings = append(b.Warnings, "prices: skipped (TIINGO_API_KEY not set)")
	case priceErr != nil:
		b.Warnings = append(b.Warnings, "prices: "+priceErr.Error())
	}
	switch {
	case f.Earnings == nil:
		b.Warnings = append(b.Warnings, "earnings: skipped (ALPHA_VANTAGE_API_KEY not set)")
	case earningsErr != nil:
		b.Warnings = append(b.Warnings, "earnings: "+earningsErr.Error())
	}
	return b, nil
}
