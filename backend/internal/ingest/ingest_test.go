package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
)

// Fakes: plain structs whose methods satisfy the interfaces implicitly.

type fakeSEC struct {
	facts    *edgar.CompanyFacts
	factsErr error
}

func (f fakeSEC) LookupTicker(_ context.Context, t string) (edgar.Company, error) {
	if t != "V" {
		return edgar.Company{}, edgar.ErrUnknownTicker
	}
	return edgar.Company{Ticker: "V", CIK: "0001403161", Name: "VISA INC."}, nil
}

func (f fakeSEC) CompanyFacts(context.Context, string) (*edgar.CompanyFacts, error) {
	return f.facts, f.factsErr
}

type fakePrices struct {
	err       error
	gotTicker string
	gotStart  time.Time
}

func (f *fakePrices) DailyPrices(_ context.Context, ticker string, start time.Time) ([]prices.Bar, error) {
	f.gotTicker, f.gotStart = ticker, start
	if f.err != nil {
		return nil, f.err
	}
	return []prices.Bar{{Date: "2026-10-13", Close: 345, AdjClose: 345}}, nil
}

type fakeEarnings struct{ err error }

func (f fakeEarnings) Earnings(context.Context, string) ([]estimates.QuarterlyEarnings, error) {
	if f.err != nil {
		return nil, f.err
	}
	eps := 3.32
	return []estimates.QuarterlyEarnings{{FiscalDateEnding: "2026-06-30", ReportedEPS: &eps}}, nil
}

func visaFacts(t *testing.T) *edgar.CompanyFacts {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "edgar", "companyfacts_V.json"))
	if err != nil {
		t.Fatal(err)
	}
	var facts edgar.CompanyFacts
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	return &facts
}

var fixedNow = func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }

func TestFetchAllSources(t *testing.T) {
	p := &fakePrices{}
	f := &Fetcher{Fundamentals: fakeSEC{facts: visaFacts(t)}, Prices: p, Earnings: fakeEarnings{}, Now: fixedNow}

	b, err := f.Fetch(t.Context(), "V")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Financials.Annual) == 0 || len(b.Prices) != 1 || len(b.Earnings) != 1 {
		t.Errorf("bundle incomplete: %d annual, %d prices, %d earnings",
			len(b.Financials.Annual), len(b.Prices), len(b.Earnings))
	}
	if len(b.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", b.Warnings)
	}
	if p.gotTicker != "V" || !p.gotStart.Equal(fixedNow().AddDate(-11, 0, 0)) {
		t.Errorf("prices called with %s from %s", p.gotTicker, p.gotStart)
	}
}

func TestFetchPartialFailures(t *testing.T) {
	f := &Fetcher{
		Fundamentals: fakeSEC{facts: visaFacts(t)},
		Prices:       &fakePrices{err: errors.New("tiingo down")},
		Earnings:     nil, // no key configured
		Now:          fixedNow,
	}

	b, err := f.Fetch(t.Context(), "V")
	if err != nil {
		t.Fatalf("price/earnings problems must not fail the fetch: %v", err)
	}
	joined := strings.Join(b.Warnings, "\n")
	if !strings.Contains(joined, "tiingo down") || !strings.Contains(joined, "ALPHA_VANTAGE_API_KEY not set") {
		t.Errorf("warnings = %v", b.Warnings)
	}
}

func TestFetchFailsWithoutFinancials(t *testing.T) {
	f := &Fetcher{Fundamentals: fakeSEC{factsErr: errors.New("sec down")}, Now: fixedNow}
	if _, err := f.Fetch(t.Context(), "V"); err == nil || !strings.Contains(err.Error(), "sec down") {
		t.Fatalf("err = %v, want sec down", err)
	}
}

func TestFetchUnknownTicker(t *testing.T) {
	f := &Fetcher{Fundamentals: fakeSEC{}, Now: fixedNow}
	if _, err := f.Fetch(t.Context(), "NOPE"); !errors.Is(err, edgar.ErrUnknownTicker) {
		t.Fatalf("err = %v, want ErrUnknownTicker", err)
	}
}

type fakeClassFacts struct{ calls int }

func (f *fakeClassFacts) ClassFacts(context.Context, string, time.Time) ([]edgar.Fact, []edgar.Fact, []string, error) {
	f.calls++
	eps := []edgar.Fact{{Start: "2024-10-01", End: "2025-09-30", Val: 10.20, Accn: "a", Form: "10-K", Filed: "2025-11-06"}}
	shares := []edgar.Fact{{Start: "2024-10-01", End: "2025-09-30", Val: 1966e6, Accn: "a", Form: "10-K", Filed: "2025-11-06"}}
	return eps, shares, []string{"per-class EPS and diluted shares read from filing XBRL: Class A"}, nil
}

func loadFacts(t *testing.T, ticker string) *edgar.CompanyFacts {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "edgar", "companyfacts_"+ticker+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var facts edgar.CompanyFacts
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	return &facts
}

func TestFetchFillsPerClassEPS(t *testing.T) {
	cf := &fakeClassFacts{}
	f := &Fetcher{Fundamentals: fakeSEC{facts: visaFacts(t)}, ClassFacts: cf, Now: fixedNow}

	b, err := f.Fetch(t.Context(), "V")
	if err != nil {
		t.Fatal(err)
	}
	if cf.calls != 1 {
		t.Fatalf("class facts called %d times, want 1 (Visa has no plain EPS)", cf.calls)
	}
	var fy25 *float64
	for _, p := range b.Financials.Annual {
		if p.EndDate == "2025-09-30" {
			fy25 = p.EPSDiluted
		}
	}
	if fy25 == nil || *fy25 != 10.20 {
		t.Errorf("Visa FY2025 EPS = %v, want 10.20", fy25)
	}
	if len(b.Notes) != 1 {
		t.Errorf("notes = %v", b.Notes)
	}
}

func TestFetchSkipsPerClassWhenEPSExists(t *testing.T) {
	cf := &fakeClassFacts{}
	f := &Fetcher{Fundamentals: fakeSEC{facts: loadFacts(t, "AAPL")}, ClassFacts: cf, Now: fixedNow}

	if _, err := f.Fetch(t.Context(), "V"); err != nil {
		t.Fatal(err)
	}
	if cf.calls != 0 {
		t.Errorf("class facts called %d times; companies with plain EPS must not need filing documents", cf.calls)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := map[string]bool{
		"":                                 false,
		"TickerLens you@example.com":       false,
		"TickerLens":                       false,
		"TickerLens someone@somewhere.com": true,
	}
	for ua, wantOK := range tests {
		err := Config{SECUserAgent: ua}.Validate()
		if (err == nil) != wantOK {
			t.Errorf("Validate(%q) err = %v, want ok=%v", ua, err, wantOK)
		}
	}
}
