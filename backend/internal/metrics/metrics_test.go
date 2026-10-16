package metrics

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/phase"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

var update = flag.Bool("update", false, "rewrite golden files")

var companies = map[string]edgar.Company{
	"V":    {Ticker: "V", CIK: "0001403161", Name: "VISA INC."},
	"AAPL": {Ticker: "AAPL", CIK: "0000320193", Name: "Apple Inc."},
	"SNOW": {Ticker: "SNOW", CIK: "0001640147", Name: "Snowflake Inc."},
}

func testdata(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...)
}

func readJSON(t *testing.T, path string, v any) bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return true
}

// loadBundle rebuilds an ingest.Bundle from recorded fixtures, the same way
// ingest does: companyfacts + per-class facts → Normalize, plus prices and
// earnings.
func loadBundle(t *testing.T, ticker string) *ingest.Bundle {
	t.Helper()
	var facts edgar.CompanyFacts
	if !readJSON(t, testdata("edgar", "companyfacts_"+ticker+".json"), &facts) {
		t.Fatalf("missing companyfacts fixture for %s", ticker)
	}
	var class map[string][]edgar.Fact
	if readJSON(t, testdata("edgar", "classfacts_"+ticker+".json"), &class) {
		edgar.MergeClassFacts(&facts, class["eps"], class["shares"])
	}
	b := &ingest.Bundle{Company: companies[ticker]}
	b.Financials = financials.Normalize(b.Company, &facts)
	var bars []prices.Bar
	readJSON(t, testdata("tiingo", "prices_"+ticker+".json"), &bars)
	b.Prices = bars
	var quarters []estimates.QuarterlyEarnings
	readJSON(t, testdata("alphavantage", "quarters_"+ticker+".json"), &quarters)
	b.Earnings = quarters
	return b
}

func compute(t *testing.T, ticker string) Report {
	t.Helper()
	return Compute(loadBundle(t, ticker), scoring.Default())
}

// TestGolden snapshots the whole report. The daily price chart is left out of
// the snapshot (it is just the input prices) and checked for length instead.
func TestGolden(t *testing.T) {
	for ticker := range companies {
		t.Run(ticker, func(t *testing.T) {
			r := compute(t, ticker)
			if len(r.Charts.Price) < 1200 {
				t.Errorf("price chart has %d points, want ~5 years of trading days", len(r.Charts.Price))
			}
			r.Charts.Price = nil

			got, err := json.MarshalIndent(r, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			path := testdata("golden", "metrics_"+ticker+".json")
			if *update {
				if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden file (run with -update): %v", err)
			}
			if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
				t.Errorf("report differs from %s; run with -update and review the diff", path)
			}
		})
	}
}

func metric(t *testing.T, ms []Metric, key string) Metric {
	t.Helper()
	for _, m := range ms {
		if m.Key == key {
			return m
		}
	}
	t.Fatalf("metric %q not found", key)
	return Metric{}
}

func near(t *testing.T, name string, got *float64, want, relTol float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %v", name, want)
		return
	}
	if math.Abs(*got-want) > math.Abs(want)*relTol {
		t.Errorf("%s = %v, want %v (±%.2g%%)", name, *got, want, relTol*100)
	}
}

// Expected values are worked out by hand from the companies' filings and the
// recorded closing prices; the comments show the arithmetic.
func TestHandCheckedApple(t *testing.T) {
	r := compute(t, "AAPL")

	// TTM to 2026-06-27: net income 128,930 ÷ revenue 466,823 ($M).
	near(t, "net margin", metric(t, r.Profitability, "netMargin").Value, 128_930.0/466_823, 1e-9)
	// EPS TTM = Q4 FY25 1.84 + Q1 FY26 2.84 + Q2 2.01 + Q3 2.02 = 8.71;
	// P/E = close 342.96 ÷ 8.71.
	near(t, "P/E", metric(t, r.Valuation, "pe").Value, 342.96/8.71, 1e-9)
	// CAGRs use TTM endpoints. Revenue TTM to 2026-06-27 = 466,823; TTM to
	// 2023-07-01 = 90,146 + 117,154 + 94,836 + 81,797 = 383,933.
	near(t, "revenue CAGR 3y", metric(t, r.Growth, "revenueCagr3y").Value, math.Pow(466_823.0/383_933, 1.0/3)-1, 1e-6)
	// EPS TTM to 2016-06-25 = 1.96 + 3.28 + 1.90 + 1.42 = 8.56, filed before the
	// 4-for-1 split of 2020-08-31, so 2.14 in today's shares; TTM now 8.71.
	near(t, "EPS CAGR 10y", metric(t, r.Growth, "epsCagr10y").Value, math.Pow(8.71/(8.56/4), 0.1)-1, 1e-4)
	// Interest expense stopped being reported separately after FY2023.
	if m := metric(t, r.Health, "interestCoverage"); m.Value != nil || m.Note == "" {
		t.Errorf("interest coverage should be unknown with a note, got %+v", m)
	}
	if r.Phase.Number != 4 || r.Phase.ValuationMetric != phase.PE {
		t.Errorf("phase = %d %s, want 4 (capital return) valued on P/E", r.Phase.Number, r.Phase.ValuationMetric)
	}
}

func TestHandCheckedVisa(t *testing.T) {
	r := compute(t, "V")

	// Class A diluted EPS read from the filings: TTM to 2026-06-30 =
	// 2.61 (Q4 FY25) + 3.03 + 3.14 + 2.97 = 11.75.
	near(t, "P/E", metric(t, r.Valuation, "pe").Value, 391.43/11.75, 1e-9)
	// Revenue TTM to 2026-03-31 = 43,027 (the fixture predates the June 10-Q);
	// TTM to 2023-03-31 = 7,275 + 7,787 + 7,936 + 7,985 = 30,983 (Q4 derived as FY − 9M).
	near(t, "revenue CAGR 3y", metric(t, r.Growth, "revenueCagr3y").Value, math.Pow(43_027.0/30_983, 1.0/3)-1, 1e-4)
	// Balance sheet at 2026-03-31: debt 23,976 ÷ equity 35,661.
	near(t, "debt/equity", metric(t, r.Health, "debtToEquity").Value, 23_976.0/35_661, 1e-9)
	// Visa reports no cost of revenue.
	if m := metric(t, r.Profitability, "grossMargin"); m.Value != nil {
		t.Errorf("gross margin should be unknown, got %v", *m.Value)
	}
	if r.HealthScore.Value == nil || *r.HealthScore.Value != 5 {
		t.Errorf("health score = %v, want 5 (all four criteria met)", r.HealthScore.Value)
	}
}

func TestHandCheckedSnowflake(t *testing.T) {
	r := compute(t, "SNOW")

	if r.Phase.Number != 2 || r.Phase.ValuationMetric != phase.PS {
		t.Errorf("phase = %d %s, want 2 (hypergrowth) valued on P/S", r.Phase.Number, r.Phase.ValuationMetric)
	}
	if m := metric(t, r.Valuation, "pe"); m.Value != nil {
		t.Errorf("P/E of a loss-making company should be undefined, got %v", *m.Value)
	}
	if m := metric(t, r.Growth, "epsCagr3y"); m.Value != nil || m.Note == "" {
		t.Errorf("EPS CAGR from a negative start should be undefined with a note, got %+v", m)
	}
	if r.Bands == nil || r.Bands.Metric != phase.PS {
		t.Fatalf("valuation bands should use P/S, got %+v", r.Bands)
	}
	// Revenue per share × band edge = price edge.
	b := r.Bands
	near(t, "cheap price", &b.CheapPrice, b.Cheap*b.PerShare, 1e-3)
}

func TestSplitAdjustment(t *testing.T) {
	sp := splits{{date: "2020-08-31", factor: 4}, {date: "2024-06-10", factor: 10}}
	eps := 8.0
	shares := 1000.0
	p := financials.FinancialPeriod{EndDate: "2019-09-28", EPSDiluted: &eps, SharesDiluted: &shares,
		Filed: map[string]string{"epsDiluted": "2019-10-30", "sharesDiluted": "2021-10-29"}}

	// EPS filed before both splits: ÷ 40. Shares filed (restated) after the
	// first split: only the second applies, × 10.
	if got := sp.perShare(p, "epsDiluted"); *got != 0.2 {
		t.Errorf("EPS = %v, want 0.2", *got)
	}
	if got := sp.shareCount(p, "sharesDiluted"); *got != 10_000 {
		t.Errorf("shares = %v, want 10000", *got)
	}
	// A price on the day of a split is already post-split.
	if got := sp.price(prices.Bar{Date: "2024-06-10", Close: 120}); got != 120 {
		t.Errorf("price on split day = %v, want 120", got)
	}
	if got := sp.price(prices.Bar{Date: "2024-06-07", Close: 1200}); got != 120 {
		t.Errorf("price before split = %v, want 120", got)
	}
}

func TestAppleSplitInPrices(t *testing.T) {
	sp := splitsFrom(loadBundle(t, "AAPL").Prices)
	if len(sp) != 1 || sp[0].date != "2020-08-31" || sp[0].factor != 4 {
		t.Errorf("splits = %+v, want the 4-for-1 split on 2020-08-31", sp)
	}
}
