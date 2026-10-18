package ingest

import (
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
)

// Honeywell's 1-for-2 reverse split took effect with its aerospace spin-off
// on 2026-06-29. Tiingo shows one combined factor (0.9535); the Q2 2026 10-Q
// restates Q2 2025 diluted EPS 2.45 → 4.90 and shares 640.9M → 320.5M.
func TestSplitFoundInRestatedFilings(t *testing.T) {
	fact := func(val float64, form, filed string) edgar.Fact {
		return edgar.Fact{Start: "2025-04-01", End: "2025-06-30", Val: val, Form: form, Filed: filed}
	}
	facts := &edgar.CompanyFacts{Facts: map[string]map[string]edgar.Concept{"us-gaap": {
		"EarningsPerShareDiluted": {Units: map[string][]edgar.Fact{"USD/shares": {
			fact(2.45, "10-Q", "2025-07-24"),
			fact(2.45, "10-K", "2026-02-17"), // no quarterly share count in the 10-K
			fact(4.90, "10-Q", "2026-07-23"),
		}}},
		"WeightedAverageNumberOfDilutedSharesOutstanding": {Units: map[string][]edgar.Fact{"shares": {
			fact(640.9e6, "10-Q", "2025-07-24"),
			fact(320.5e6, "10-Q", "2026-07-23"),
		}}},
	}}}
	bars := []prices.Bar{
		{Date: "2025-10-30", SplitFactor: 1.061}, // an earlier spin-off, before the window
		{Date: "2026-06-29", SplitFactor: 0.9535},
	}

	got := SplitsFor(bars, facts)
	if len(got) != 1 || got[0].Date != "2026-06-29" || got[0].Factor != 0.5 {
		t.Fatalf("splits = %+v, want one 0.5 split on 2026-06-29", got)
	}

	// Known from prices already: not added twice.
	bars[1].SplitFactor = 0.5
	if got := SplitsFor(bars, facts); len(got) != 1 {
		t.Errorf("splits = %+v, want the price split only", got)
	}

	// EPS restated without the inverse share change is not a split.
	facts.Facts["us-gaap"]["WeightedAverageNumberOfDilutedSharesOutstanding"].Units["shares"][1].Val = 640e6
	if got := filingSplits(facts); len(got) != 0 {
		t.Errorf("filing splits = %+v, want none", got)
	}
}
