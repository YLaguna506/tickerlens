package scoring

import (
	"fmt"
	"math"
)

// SectorRules set the financial strength score of banks, broker-dealers,
// insurers and REITs, which replaces the health score for them: a bank's debt
// is deposits, an insurer's is claims reserves, and a REIT reports no
// operating income for interest coverage.
type SectorRules struct {
	MinROE float64 `yaml:"min_roe"` // return on equity ≥ this
	// Banks: non-interest expense ÷ revenue ≤ this.
	MaxBankEfficiency float64 `yaml:"max_bank_efficiency"`
	// Equity ÷ total assets ≥ this (brokers run with less).
	MinBankEquityToAssets   float64 `yaml:"min_bank_equity_to_assets"`
	MinBrokerEquityToAssets float64 `yaml:"min_broker_equity_to_assets"`
	ProfitableYears         int     `yaml:"profitable_years"` // net income (REITs: FFO) > 0 in each of the last N fiscal years
	// REITs: total debt ÷ total assets ≤ this; (FFO + interest) ÷ interest
	// ≥ this; dividends ÷ FFO ≤ this.
	MaxREITDebtToAssets     float64 `yaml:"max_reit_debt_to_assets"`
	MinREITInterestCoverage float64 `yaml:"min_reit_interest_coverage"`
	MaxREITPayout           float64 `yaml:"max_reit_ffo_payout"`
}

func defaultSector() SectorRules {
	return SectorRules{MinROE: 0.10, MaxBankEfficiency: 0.60, MinBankEquityToAssets: 0.07, MinBrokerEquityToAssets: 0.06, ProfitableYears: 3,
		MaxREITDebtToAssets: 0.45, MinREITInterestCoverage: 3, MaxREITPayout: 0.90}
}

// StrengthInputs are the numbers the financial strength score needs.
type StrengthInputs struct {
	Kind            string   // bank | broker | insurer | reit
	ROE             *float64 // TTM
	EfficiencyRatio *float64 // TTM, banks
	EquityToAssets  *float64 // latest balance sheet
	BookValueCAGR   *float64 // book value per share, 3 fiscal years
	PremiumGrowth   *float64 // insurers: latest fiscal year vs the one before
	DebtToAssets    *float64 // REITs
	// REITs: (FFO + interest expense) ÷ interest expense, and dividends ÷ FFO.
	InterestCoverage *float64
	Payout           *float64
	// NetIncomeByYear is net income by fiscal year (REITs: FFO).
	NetIncomeByYear []*float64
}

// StrengthScore awards one point per criterion met, like HealthScore:
//
//   - all kinds: return on equity ≥ MinROE, and a profit in each of the last
//     ProfitableYears fiscal years;
//   - banks: efficiency ratio ≤ MaxBankEfficiency and equity ≥
//     MinBankEquityToAssets of assets;
//   - brokers: equity ≥ MinBrokerEquityToAssets of assets, and book value
//     per share growing;
//   - insurers: book value per share growing, and premiums growing;
//   - REITs (instead of return on equity): debt ≤ MaxREITDebtToAssets of
//     assets, interest covered MinREITInterestCoverage times by FFO before
//     interest, dividends ≤ MaxREITPayout of FFO, and positive FFO each year.
func (c Config) StrengthScore(in StrengthInputs) Score {
	r := c.Sector
	s := Score{Max: 5}
	points, used := 0, 0
	// check scores one criterion; pass and fail describe the value.
	check := func(v *float64, ok func(float64) bool, pass, fail func(float64) string, unknown string) {
		switch {
		case v == nil:
			s.Reasons = append(s.Reasons, "skipped: "+unknown)
		case ok(*v):
			points++
			used++
			s.Reasons = append(s.Reasons, "✓ "+pass(*v))
		default:
			used++
			s.Reasons = append(s.Reasons, "✗ "+fail(*v))
		}
	}
	pct := func(v float64) string { return fmt.Sprintf("%.1f%%", v*100) }
	describe := func(format string, limit float64) func(float64) string {
		return func(v float64) string { return fmt.Sprintf(format, pct(v), pct(limit)) }
	}
	growth := func(format string) func(float64) string {
		return func(v float64) string { return fmt.Sprintf(format, pct(math.Abs(v))) }
	}

	if in.Kind != "reit" {
		check(in.ROE, func(v float64) bool { return v >= r.MinROE },
			describe("return on equity %s (≥ %s)", r.MinROE), describe("return on equity %s (< %s)", r.MinROE),
			"return on equity not available")
	}

	switch in.Kind {
	case "reit":
		check(in.DebtToAssets, func(v float64) bool { return v <= r.MaxREITDebtToAssets },
			describe("debt is %s of assets (≤ %s)", r.MaxREITDebtToAssets), describe("debt is %s of assets (> %s)", r.MaxREITDebtToAssets),
			"debt or total assets not reported")
		times := func(format string) func(float64) string {
			return func(v float64) string { return fmt.Sprintf(format, v, r.MinREITInterestCoverage) }
		}
		check(in.InterestCoverage, func(v float64) bool { return v >= r.MinREITInterestCoverage },
			times("FFO before interest covers interest %.1f× (≥ %.0f×)"), times("FFO before interest covers interest only %.1f× (< %.0f×)"),
			"interest expense not reported")
		check(in.Payout, func(v float64) bool { return v <= r.MaxREITPayout },
			describe("dividends are %s of FFO (≤ %s)", r.MaxREITPayout), describe("dividends are %s of FFO (> %s)", r.MaxREITPayout),
			"dividends or FFO not reported")
	case "bank":
		check(in.EfficiencyRatio, func(v float64) bool { return v <= r.MaxBankEfficiency },
			describe("efficiency ratio %s (≤ %s)", r.MaxBankEfficiency), describe("efficiency ratio %s (> %s)", r.MaxBankEfficiency),
			"efficiency ratio not available")
		check(in.EquityToAssets, func(v float64) bool { return v >= r.MinBankEquityToAssets },
			describe("equity is %s of assets (≥ %s)", r.MinBankEquityToAssets), describe("equity is %s of assets (< %s)", r.MinBankEquityToAssets),
			"total assets not reported")
	case "broker":
		check(in.EquityToAssets, func(v float64) bool { return v >= r.MinBrokerEquityToAssets },
			describe("equity is %s of assets (≥ %s)", r.MinBrokerEquityToAssets), describe("equity is %s of assets (< %s)", r.MinBrokerEquityToAssets),
			"total assets not reported")
		check(in.BookValueCAGR, func(v float64) bool { return v > 0 },
			growth("book value per share grew %s a year over 3 years"), growth("book value per share shrank %s a year over 3 years"),
			"book value per share history not available")
	case "insurer":
		check(in.BookValueCAGR, func(v float64) bool { return v > 0 },
			growth("book value per share grew %s a year over 3 years"), growth("book value per share shrank %s a year over 3 years"),
			"book value per share history not available")
		check(in.PremiumGrowth, func(v float64) bool { return v > 0 },
			growth("premiums earned grew %s in the latest fiscal year"), growth("premiums earned fell %s in the latest fiscal year"),
			"premiums earned not reported")
	}

	// A profit (REITs: positive FFO) in each recent year.
	n := r.ProfitableYears
	what, good, bad := "net income", "a profit in each of the last %d years", "a loss in at least one of the last %d years"
	if in.Kind == "reit" {
		what, good, bad = "FFO", "positive FFO in each of the last %d years", "FFO was not positive in each of the last %d years"
	}
	if len(in.NetIncomeByYear) < n {
		s.Reasons = append(s.Reasons, fmt.Sprintf("skipped: fewer than %d years of %s", n, what))
	} else {
		known, profitable := true, true
		for _, v := range in.NetIncomeByYear[len(in.NetIncomeByYear)-n:] {
			if v == nil {
				known = false
			} else if *v <= 0 {
				profitable = false
			}
		}
		switch {
		case !known:
			s.Reasons = append(s.Reasons, fmt.Sprintf("skipped: %s missing for a recent year", what))
		case profitable:
			points++
			used++
			s.Reasons = append(s.Reasons, "✓ "+fmt.Sprintf(good, n))
		default:
			used++
			s.Reasons = append(s.Reasons, "✗ "+fmt.Sprintf(bad, n))
		}
	}

	if used < c.MinHealthCriteriaUsed {
		s.Reasons = append(s.Reasons, "not enough data to score")
		return s
	}
	score := 1 + 4*float64(points)/float64(used)
	s.Value = &score
	s.Reasons = append(s.Reasons, fmt.Sprintf("%d of %d criteria met → %.1f/5", points, used, score))
	return s
}
