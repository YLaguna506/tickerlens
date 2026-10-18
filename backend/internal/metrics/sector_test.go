package metrics

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

// Hand-checked against JPMorgan's own reporting for mid-2026: ROE ~18%,
// overhead ratio ~51%, book value per common share ~$131.
func TestSectorBank(t *testing.T) {
	r := compute(t, "JPM")
	s := r.Sector
	if s == nil || s.Kind != "bank" || r.LimitedSupport != nil {
		t.Fatalf("sector = %+v, limited = %+v", s, r.LimitedSupport)
	}
	within(t, "ROE", find(s.Metrics, "roe"), 0.15, 0.22)
	within(t, "efficiency ratio", find(s.Metrics, "efficiencyRatio"), 0.45, 0.58)
	within(t, "book value per share", find(s.Metrics, "bookValuePerShare"), 120, 140)
	within(t, "loans ÷ deposits", find(s.Metrics, "loansToDeposits"), 0.4, 0.8)
	if r.Bands == nil || r.Bands.Metric != "P/B" || r.Phase.ValuationMetric != "P/B" {
		t.Errorf("valued on %+v (phase %s), want P/B", r.Bands, r.Phase.ValuationMetric)
	}
	if r.HealthScore.Value == nil || !strings.Contains(strings.Join(r.HealthScore.Reasons, " "), "efficiency ratio") {
		t.Errorf("strength score = %+v", r.HealthScore)
	}
	if r.ReverseDCF == nil || r.ReverseDCF.ImpliedGrowth != nil {
		t.Errorf("reverse DCF should not run for a bank: %+v", r.ReverseDCF)
	}
	if s.Trend == nil || len(s.Trend.Points) < 5 {
		t.Errorf("ROE trend = %+v", s.Trend)
	}
}

// Progressive: loss and loss-adjustment expense ratio around two thirds.
func TestSectorInsurer(t *testing.T) {
	r := compute(t, "PGR")
	s := r.Sector
	if s == nil || s.Kind != "insurer" || r.LimitedSupport != nil {
		t.Fatalf("sector = %+v, limited = %+v", s, r.LimitedSupport)
	}
	within(t, "loss ratio", find(s.Metrics, "lossRatio"), 0.55, 0.8)
	within(t, "ROE", find(s.Metrics, "roe"), 0.2, 0.45)
	if find(s.Metrics, "premiumGrowth5y") == nil {
		t.Error("5-year premium growth missing (PremiumsEarnedNetPropertyAndCasualty before 2020)")
	}
	if r.Bands == nil || r.Bands.Metric != "P/B" {
		t.Errorf("bands = %+v, want P/B", r.Bands)
	}
}

// Realty Income reported FFO of about $4.2 a share for 2025.
func TestSectorREIT(t *testing.T) {
	r := compute(t, "O")
	s := r.Sector
	if s == nil || s.Kind != "reit" || r.LimitedSupport != nil {
		t.Fatalf("sector = %+v, limited = %+v", s, r.LimitedSupport)
	}
	within(t, "FFO per share", find(s.Metrics, "ffoPerShare"), 3.9, 4.7)
	within(t, "payout", find(s.Metrics, "ffoPayout"), 0.6, 0.9)
	if r.Bands == nil || r.Bands.Metric != "P/FFO" {
		t.Errorf("bands = %+v, want P/FFO", r.Bands)
	}
	if find(r.Valuation, "pffo") == nil {
		t.Error("P/FFO missing from the valuation metrics")
	}
	growth := strings.Join(r.GrowthScore.Reasons, " ")
	if !strings.Contains(growth, "FFO per share") || strings.Contains(growth, "EPS") || strings.Contains(growth, "revenue") {
		t.Errorf("REIT growth should rest on FFO per share: %s", growth)
	}
	if r.HealthScore.Value == nil || !strings.Contains(strings.Join(r.HealthScore.Reasons, " "), "FFO") {
		t.Errorf("strength score = %+v", r.HealthScore)
	}
}

// Without equity there is no return on equity: the bank falls back to the
// limited-support notice, with no health score.
func TestSectorFallsBackToLimitedSupport(t *testing.T) {
	b := loadBundle(t, "JPM")
	for _, list := range [][]financials.FinancialPeriod{b.Financials.Annual, b.Financials.Quarterly} {
		for i := range list {
			list[i].TotalEquity = nil
		}
	}
	b.Financials.TTM.TotalEquity = nil
	r := Compute(b, scoring.Default())
	if r.LimitedSupport == nil || r.LimitedSupport.Kind != "bank" {
		t.Fatalf("limited support = %+v", r.LimitedSupport)
	}
	if r.HealthScore.Value != nil || !strings.Contains(strings.Join(r.HealthScore.Reasons, " "), "banks") {
		t.Errorf("health score = %+v", r.HealthScore)
	}
}

func within(t *testing.T, name string, v *float64, lo, hi float64) {
	t.Helper()
	if v == nil || *v < lo || *v > hi {
		got := "nil"
		if v != nil {
			got = fmt.Sprintf("%.4f", *v)
		}
		t.Errorf("%s = %s, want %g–%g", name, got, lo, hi)
	}
}
