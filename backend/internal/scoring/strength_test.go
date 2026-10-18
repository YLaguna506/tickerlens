package scoring

import (
	"strings"
	"testing"
)

func TestStrengthScore(t *testing.T) {
	cfg := Default()
	profits := []*float64{f(1), f(2), f(3)}

	// A bank meeting everything but the capital test: 3 of 4 → 4/5.
	s := cfg.StrengthScore(StrengthInputs{Kind: "bank", ROE: f(0.15), EfficiencyRatio: f(0.55), EquityToAssets: f(0.06), NetIncomeByYear: profits})
	if s.Value == nil || *s.Value != 4 || !strings.Contains(strings.Join(s.Reasons, "|"), "✗ equity is 6.0% of assets (< 7.0%)") {
		t.Errorf("bank = %v %v", s.Value, s.Reasons)
	}

	// An insurer whose book value shrank and premiums fell, with a loss year.
	s = cfg.StrengthScore(StrengthInputs{Kind: "insurer", ROE: f(0.12), BookValueCAGR: f(-0.02), PremiumGrowth: f(-0.05),
		NetIncomeByYear: []*float64{f(1), f(-1), f(2)}})
	if s.Value == nil || *s.Value != 2 || !strings.Contains(strings.Join(s.Reasons, "|"), "book value per share shrank 2.0% a year") {
		t.Errorf("insurer = %v %v", s.Value, s.Reasons)
	}

	// A REIT is not judged on return on equity.
	s = cfg.StrengthScore(StrengthInputs{Kind: "reit", ROE: f(0.01), DebtToAssets: f(0.5), InterestCoverage: f(4), Payout: f(0.8), NetIncomeByYear: profits})
	if s.Value == nil || *s.Value != 4 || strings.Contains(strings.Join(s.Reasons, "|"), "return on equity") {
		t.Errorf("reit = %v %v", s.Value, s.Reasons)
	}

	// Too little data: no score.
	if s := cfg.StrengthScore(StrengthInputs{Kind: "bank", ROE: f(0.2)}); s.Value != nil {
		t.Errorf("one criterion should not be scored: %v", s.Reasons)
	}
}
