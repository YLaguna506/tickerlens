package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/phase"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

func num(v float64) *float64 { return &v }

// flatPrices returns a daily close of 100 from 2020-07-01 to 2026-06-30.
func flatPrices() []prices.Bar {
	var bars []prices.Bar
	for d := time.Date(2020, 7, 1, 0, 0, 0, 0, time.UTC); d.Before(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)); d = d.AddDate(0, 0, 1) {
		bars = append(bars, prices.Bar{Date: d.Format(time.DateOnly), Close: 100})
	}
	return bars
}

// quarterlyTTM returns one TTM window a quarter from mid-2020 on; eps gives
// the EPS of each window by its public date. FCF per share is a steady 4.
func quarterlyTTM(eps func(available string) float64) []ttm {
	var out []ttm
	for d := time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC); d.Year() < 2026 || d.Month() <= 5; d = d.AddDate(0, 3, 0) {
		day := d.Format(time.DateOnly)
		out = append(out, ttm{end: day, available: day, revenue: num(20), fcf: num(4), shares: num(1), eps: num(eps(day))})
	}
	return out
}

func TestDistortion(t *testing.T) {
	cfg := scoring.Default()
	from := "2021-06-30"
	tests := []struct {
		name string
		eps  func(string) float64
		want string // substring of the reason; "" means not distorted
	}{
		{"steady growth", func(d string) float64 { return 3 + float64(d[3]-'0') }, ""},
		{"collapse", func(d string) float64 {
			if strings.HasPrefix(d, "2022") {
				return 0.2
			}
			return 5
		}, "fell as low as $0.20 against a typical $5.00"},
		{"loss", func(d string) float64 {
			if d == "2023-02-01" {
				return -0.1
			}
			return 5
		}, "zero or negative"},
		{"collapse before the window is ignored", func(d string) float64 {
			if d < "2021-01-01" {
				return 0.1
			}
			return 5
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := distortion(phase.PE, quarterlyTTM(tt.eps), from, cfg)
			if tt.want == "" && got != "" || tt.want != "" && !strings.Contains(got, tt.want) {
				t.Errorf("distortion = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBandsSkipDistortedHistory(t *testing.T) {
	collapse := func(d string) float64 {
		if strings.HasPrefix(d, "2022") {
			return 0.2 // P/E of 500 for a year
		}
		return 5
	}
	b := &ingest.Bundle{Prices: flatPrices()}
	ph := phase.Result{ValuationMetric: phase.PE, Fallbacks: []string{phase.PFCF}}
	price := 100.0

	bands, score, _, notes := valuationBands(b, scoring.Default(), ph, nil, quarterlyTTM(collapse), nil, &price)

	if bands == nil || bands.Metric != phase.PFCF {
		t.Fatalf("metric = %+v, want P/FCF", bands)
	}
	if bands.FellBackFrom != phase.PE || !strings.Contains(bands.FallbackReason, "distorted history: EPS fell") {
		t.Errorf("fallback = %q / %q", bands.FellBackFrom, bands.FallbackReason)
	}
	if bands.Median != 25 { // price 100 ÷ FCF per share 4, every day
		t.Errorf("P/FCF median %v, want 25", bands.Median)
	}
	if !strings.Contains(strings.Join(score.Reasons, "; "), "used P/FCF because P/E has a distorted history") {
		t.Errorf("reasons = %v", score.Reasons)
	}
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "P/E: history distorted") {
		t.Errorf("notes = %v", notes)
	}
}

func TestBandsKeepDistortedMetricAsLastResort(t *testing.T) {
	rolling := quarterlyTTM(func(d string) float64 {
		if strings.HasPrefix(d, "2022") {
			return 0.2
		}
		return 5
	})
	for i := range rolling {
		rolling[i].revenue, rolling[i].fcf = nil, nil // no other metric has data
	}
	b := &ingest.Bundle{Prices: flatPrices()}
	ph := phase.Result{ValuationMetric: phase.PE, Fallbacks: []string{phase.PFCF}}
	price := 100.0

	bands, score, _, _ := valuationBands(b, scoring.Default(), ph, nil, rolling, nil, &price)

	if bands == nil || bands.Metric != phase.PE || bands.FellBackFrom != "" {
		t.Fatalf("bands = %+v, want P/E with no fallback", bands)
	}
	if last := score.Reasons[len(score.Reasons)-1]; !strings.HasPrefix(last, "caution:") {
		t.Errorf("last reason = %q, want a caution", last)
	}
}

// A multiple is not computed on numbers years out of date: when free cash
// flow is missing from every recent window, P/FCF is not available.
func TestCurrentIgnoresStaleWindows(t *testing.T) {
	fcf, shares := 500e6, 100e6
	ends := []string{"2022-12-31", "2023-03-31", "2023-06-30", "2025-09-30", "2025-12-31", "2026-03-31", "2026-06-30"}
	var rolling []ttm
	for _, e := range ends {
		rolling = append(rolling, ttm{end: e, shares: &shares})
	}
	rolling[0].fcf = &fcf // only an old window has it
	if _, basis, _, _, ok := current("P/FCF", rolling, 100); ok || basis != "no recent TTM data" {
		t.Errorf("stale window used: ok=%v basis=%q", ok, basis)
	}
	rolling[5].fcf = &fcf // one quarter behind the newest: fine
	if v, _, _, period, ok := current("P/FCF", rolling, 100); !ok || v != 20 || period != "TTM to 2026-03-31" {
		t.Errorf("recent window: %v %v %s", v, ok, period)
	}
}
