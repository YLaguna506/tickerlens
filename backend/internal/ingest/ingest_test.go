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
	if p.gotTicker != "V" || !p.gotStart.Equal(fixedNow().AddDate(-5, 0, 0)) {
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
