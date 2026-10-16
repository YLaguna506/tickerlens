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

// FreshSource reads the latest 10-K/10-Q directly when companyfacts has not
// caught up with it (edgar.Client implements it).
type FreshSource interface {
	FreshFacts(ctx context.Context, cik, knownThrough string, concepts []string,
		unitFor func(string) string) (edgar.Filing, edgar.FactsByConcept, bool, error)
}

// Fetcher gathers a Bundle. Prices and Earnings may be nil when no API key is
// configured; the bundle then says so in Warnings.
type Fetcher struct {
	// SEC is the EDGAR client NewFetcher built, shared so other code (the
	// ticker search) uses the same rate limiter. Nil in tests.
	SEC          *edgar.Client
	Fundamentals Fundamentals
	ClassFacts   ClassFactsSource // optional
	Fresh        FreshSource      // optional
	Prices       PriceSource
	Earnings     EarningsSource
	PriceYears   int              // history to fetch; default 11
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
	// Failed names the sources behind those warnings ("prices", "earnings",
	// "per-class-eps"), for callers that track sections.
	Failed []string `json:"failed,omitempty"`
	// Notes explain unusual but successful data handling.
	Notes []string `json:"notes,omitempty"`
}

// latestEnd is the latest period end reported for any of the concepts.
func latestEnd(cf *edgar.CompanyFacts, concepts ...string) string {
	var max string
	for _, name := range concepts {
		for _, facts := range cf.Facts["us-gaap"][name].Units {
			for _, f := range facts {
				if f.End > max {
					max = f.End
				}
			}
		}
	}
	return max
}

// LacksRecentEPS reports whether companyfacts has no diluted EPS within
// ~7 months of the latest net income figure.
func LacksRecentEPS(cf *edgar.CompanyFacts) bool {
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

// Splits lists the stock splits in daily prices, oldest first.
func Splits(bars []prices.Bar) []financials.Split {
	var out []financials.Split
	for _, b := range bars {
		if b.SplitFactor != 0 && b.SplitFactor != 1 {
			out = append(out, financials.Split{Date: b.Date, Factor: b.SplitFactor})
		}
	}
	return out
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
	// 11 years of prices: charts and valuation bands use the last 5, but the
	// full span is needed to know every stock split behind a 10-year growth
	// rate (old EPS is reported in pre-split shares).
	years := f.PriceYears
	if years <= 0 {
		years = 11
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
	if f.ClassFacts != nil && LacksRecentEPS(facts) {
		eps, shares, notes, err := f.ClassFacts.ClassFacts(ctx, co.CIK, now())
		if err != nil {
			b.Warnings = append(b.Warnings, "per-class EPS: "+err.Error())
			b.Failed = append(b.Failed, "per-class-eps")
		} else {
			edgar.MergeClassFacts(facts, eps, shares)
			b.Notes = append(b.Notes, notes...)
		}
	}

	// companyfacts can lag a new filing by weeks; read the newest one
	// directly so TTM figures include the latest quarter.
	if f.Fresh != nil {
		through := latestEnd(facts, "NetIncomeLoss", "Revenues", "RevenueFromContractWithCustomerExcludingAssessedTax")
		filing, add, ok, err := f.Fresh.FreshFacts(ctx, co.CIK, through, financials.Concepts(), financials.UnitOf)
		switch {
		case err != nil:
			b.Warnings = append(b.Warnings, "latest filing: "+err.Error())
		case ok:
			edgar.MergeFacts(facts, add)
			b.Notes = append(b.Notes, fmt.Sprintf(
				"read the %s for the period ended %s (filed %s) directly: the SEC data feed did not include it yet",
				filing.Form, filing.ReportDate, filing.FilingDate))
		}
	}

	// Prices carry the stock splits, which normalization needs to combine
	// per-share values filed on either side of a split.
	b.Financials = financials.Normalize(co, facts, Splits(b.Prices))

	switch {
	case f.Prices == nil:
		b.Warnings = append(b.Warnings, "prices: skipped (TIINGO_API_KEY not set)")
		b.Failed = append(b.Failed, "prices")
	case priceErr != nil:
		b.Warnings = append(b.Warnings, "prices: "+priceErr.Error())
		b.Failed = append(b.Failed, "prices")
	}
	switch {
	case f.Earnings == nil:
		b.Warnings = append(b.Warnings, "earnings: skipped (ALPHA_VANTAGE_API_KEY not set)")
		b.Failed = append(b.Failed, "earnings")
	case earningsErr != nil:
		b.Warnings = append(b.Warnings, "earnings: "+earningsErr.Error())
		b.Failed = append(b.Failed, "earnings")
	}
	return b, nil
}
