package metrics

import (
	"strings"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
)

func quarterWithEPS(start, end string, total float64, cont *float64) financials.FinancialPeriod {
	return financials.FinancialPeriod{
		PeriodType: financials.FiscalQuarter, StartDate: start, EndDate: end,
		EPSDiluted: num(total), EPSContinuing: cont, SharesDiluted: num(1e9),
		Sources: map[string]string{}, Filed: map[string]string{},
	}
}

// A company that sold a business: one quarter's diluted EPS includes the
// gain in discontinued operations; P/E uses EPS from continuing operations.
func TestValuationUsesContinuingEPS(t *testing.T) {
	qs := []financials.FinancialPeriod{
		quarterWithEPS("2025-01-01", "2025-03-31", 1, num(1)),
		quarterWithEPS("2025-04-01", "2025-06-30", 9, num(1)), // 8 of it from the sale
		quarterWithEPS("2025-07-01", "2025-09-30", 1, nil),    // not tagged: use diluted EPS
		quarterWithEPS("2025-10-01", "2025-12-31", 1, num(1)),
	}
	rolling := rollingTTM(qs, nil, nil)
	if len(rolling) != 1 {
		t.Fatalf("windows = %d", len(rolling))
	}
	w := rolling[0]
	if *w.eps != 12 || *w.valuationEPS != 4 || !w.continuing {
		t.Errorf("eps %v, valuation EPS %v, continuing %v; want 12, 4, true", *w.eps, *w.valuationEPS, w.continuing)
	}
	if ps, _, _ := perShareBasis("P/E", w); ps != 4 {
		t.Errorf("P/E basis = %v, want 4", ps)
	}

	// The same EPS both ways: nothing to say.
	qs[1].EPSDiluted = num(1)
	if w := rollingTTM(qs, nil, nil)[0]; w.continuing || *w.valuationEPS != 4 {
		t.Errorf("continuing %v, valuation EPS %v", w.continuing, *w.valuationEPS)
	}
}

// Honeywell spun off its aerospace business on 2026-06-29, with a 1-for-2
// reverse split the same day; Tiingo's combined factor is 0.9535.
func TestNoValuationRightAfterLargeSpinOff(t *testing.T) {
	b := &ingest.Bundle{
		Prices: []prices.Bar{{Date: "2025-10-30", SplitFactor: 1.061}, {Date: "2026-06-29", SplitFactor: 0.9535}},
		Splits: []financials.Split{{Date: "2026-06-29", Factor: 0.5}},
	}
	rolling := []ttm{{end: "2026-06-30"}}
	d := recentSpinOff(b, rolling)
	if !strings.Contains(d, "spin-off on 2026-06-29 took about 48% off") {
		t.Errorf("reason = %q", d)
	}

	// A small spin-off (Honeywell's Solstice, 6%) does not count.
	b.Prices = b.Prices[:1]
	if d := recentSpinOff(b, []ttm{{end: "2026-03-31"}}); d != "" {
		t.Errorf("small spin-off flagged: %q", d)
	}
	// Nor does a large one more than a year before the latest quarter.
	b.Prices = []prices.Bar{{Date: "2026-06-29", SplitFactor: 0.9535}}
	if d := recentSpinOff(b, []ttm{{end: "2027-09-30"}}); d != "" {
		t.Errorf("old spin-off flagged: %q", d)
	}
}
