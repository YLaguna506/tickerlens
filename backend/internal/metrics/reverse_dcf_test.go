package metrics

import (
	"math"
	"strings"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

// Valuing cash flows at a known growth rate and solving back recovers it.
func TestImpliedGrowthRoundTrip(t *testing.T) {
	for _, g := range []float64{-0.05, 0, 0.04, 0.09, 0.25} {
		value := dcfValue(1e9, g, 0.09, 0.025, 10)
		got, ok := impliedGrowth(1e9, value, 0.09, 0.025, 10)
		if !ok || math.Abs(got-g) > 0.0001 {
			t.Errorf("g %v: got %v (ok %v)", g, got, ok)
		}
	}
	// A price no growth can reach, and one below any plausible decline.
	if g, ok := impliedGrowth(1e9, 1e15, 0.09, 0.025, 10); ok || g != maxImplied {
		t.Errorf("unreachable price: %v %v", g, ok)
	}
	if g, ok := impliedGrowth(1e9, 1e6, 0.09, 0.025, 10); ok || g != minImplied {
		t.Errorf("tiny price: %v %v", g, ok)
	}
}

func TestReverseDCFComparesWithActualGrowth(t *testing.T) {
	cfg := scoring.Default().ReverseDCF
	fcf, f := 1e9, 0.05
	c := &calc{ttm: &financials.FinancialPeriod{FreeCashFlow: &fcf}, period: "TTM to 2026-06-30"}
	growth := []Metric{{Key: "revenueCagr5y", Value: &f}}
	// 5% actual growth: within ±3 points is in line, 3–8 above is high.
	for _, tc := range []struct {
		g    float64
		want string
	}{{0.01, "low"}, {0.06, "in line"}, {0.12, "high"}, {0.20, "very high"}} {
		mcap := dcfValue(fcf, tc.g, cfg.DiscountRate, cfg.TerminalGrowth, cfg.Years)
		d := c.reverseDCF(&mcap, growth, nil, cfg)
		if d.ImpliedGrowth == nil || math.Abs(*d.ImpliedGrowth-tc.g) > 0.0001 || d.Expectations != tc.want || d.ReferenceName != "revenue growth, 5 years" {
			t.Errorf("implied %v: %+v, want %q", tc.g, d, tc.want)
		}
	}
	// Banks: no model, with the reason.
	mcap := 10e9
	if d := c.reverseDCF(&mcap, growth, &LimitedSupport{Kind: "bank"}, cfg); d.ImpliedGrowth != nil || d.Note == "" {
		t.Errorf("bank: %+v", d)
	}
	// Negative cash flow and no positive 3-year average: nothing to grow.
	neg := -1e9
	c.ttm.FreeCashFlow = &neg
	if d := c.reverseDCF(&mcap, growth, nil, cfg); d.ImpliedGrowth != nil || d.Note == "" {
		t.Errorf("negative FCF: %+v", d)
	}
}

// Snowflake-like: 45% past growth counts as 15%, so a price implying 27% is
// "very high" and V drops a step; a low reading lifts V, within 1–5.
func TestReverseDCFAdjustsV(t *testing.T) {
	cfg := scoring.Default().ReverseDCF
	fcf, past := 1e9, 0.45
	c := &calc{ttm: &financials.FinancialPeriod{FreeCashFlow: &fcf}, period: "TTM"}
	growth := []Metric{{Key: "revenueCagr5y", Value: &past}}
	mcap := dcfValue(fcf, 0.27, cfg.DiscountRate, cfg.TerminalGrowth, cfg.Years)
	d := c.reverseDCF(&mcap, growth, nil, cfg)
	if !d.Capped || *d.JudgedAgainst != 0.15 || d.Expectations != "very high" {
		t.Fatalf("judged %+v", d)
	}
	three := 3.0
	v := scoring.Score{Value: &three}
	adjustValuation(&v, d, cfg)
	if *v.Value != 2 || d.VAdjustment != -1 || !strings.Contains(v.Reasons[0], "very high → -1 (3 → 2)") {
		t.Errorf("V %v, adjustment %v, reasons %v", *v.Value, d.VAdjustment, v.Reasons)
	}

	// Low expectations at V 5 cannot go higher: no change, no reason.
	slow := 0.10
	mcap = dcfValue(fcf, 0.02, cfg.DiscountRate, cfg.TerminalGrowth, cfg.Years)
	d = c.reverseDCF(&mcap, []Metric{{Key: "revenueCagr5y", Value: &slow}}, nil, cfg)
	five := 5.0
	v = scoring.Score{Value: &five}
	adjustValuation(&v, d, cfg)
	if d.Expectations != "low" || *v.Value != 5 || d.VAdjustment != 0 || len(v.Reasons) != 0 {
		t.Errorf("capped at 5: V %v, %+v", *v.Value, d)
	}
	// ...but lifts V 3 to 4.
	v = scoring.Score{Value: &three}
	adjustValuation(&v, d, cfg)
	if *v.Value != 4 {
		t.Errorf("low at V 3: %v", *v.Value)
	}
	// No V (e.g. right after a spin-off): nothing to adjust.
	v = scoring.Score{}
	adjustValuation(&v, d, cfg)
	if v.Value != nil {
		t.Errorf("V appeared: %v", *v.Value)
	}
}
