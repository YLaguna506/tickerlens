// Package scoring turns computed metrics into 1–5 scores. Every threshold
// lives in Config so it can be tuned in one place (M5 loads it from
// config/scoring.yaml).
package scoring

import (
	"fmt"
	"sort"
)

// Config holds every scoring threshold.
type Config struct {
	Version string

	// Growth: average CAGR (fraction) → score. A CAGR below Bands[0] scores 1,
	// below Bands[1] scores 2, ..., at or above Bands[3] scores 5.
	GrowthBands [4]float64

	// Health criteria; each passing criterion earns one point.
	MinInterestCoverage   float64 // EBIT / interest expense ≥ this
	MaxDebtToEquity       float64 // debt / equity ≤ this
	MinFCFToDebt          float64 // FCF / total debt ≥ this
	PositiveFCFYears      int     // FCF > 0 in each of the last N fiscal years
	MinHealthCriteriaUsed int     // fewer evaluable criteria → no score

	// Valuation bands are percentiles of the metric's own history.
	CheapPercentile     float64 // ≤ this is "cheap"
	ExpensivePercentile float64 // ≥ this is "expensive"
	// Current percentile (0–1) → score: ≤ [0] scores 5, ≤ [1] 4, ≤ [2] 3,
	// ≤ [3] 2, above scores 1.
	ValuationScoreCuts [4]float64
	MinHistoryDays     int // trading days of valuation history required
	HistoryYears       int // years of history used for bands
}

// Default returns the plan's starting thresholds.
func Default() Config {
	return Config{
		Version:               "2026-10-14.1",
		GrowthBands:           [4]float64{0, 0.05, 0.12, 0.20},
		MinInterestCoverage:   10,
		MaxDebtToEquity:       1,
		MinFCFToDebt:          0.5,
		PositiveFCFYears:      3,
		MinHealthCriteriaUsed: 2,
		CheapPercentile:       0.25,
		ExpensivePercentile:   0.75,
		ValuationScoreCuts:    [4]float64{0.10, 0.30, 0.70, 0.90},
		MinHistoryDays:        250,
		HistoryYears:          5,
	}
}

// Score is a 1–5 score with a plain-language explanation of each input.
type Score struct {
	Value   *float64 `json:"value"` // nil when there is not enough data
	Max     float64  `json:"max"`
	Reasons []string `json:"reasons"`
}

// GrowthScore averages the available revenue and EPS CAGRs (3y and 5y) and
// maps the average onto 1–5.
func (c Config) GrowthScore(cagrs map[string]*float64) Score {
	s := Score{Max: 5}
	keys := make([]string, 0, len(cagrs))
	for k := range cagrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sum float64
	n := 0
	for _, k := range keys {
		v := cagrs[k]
		if v == nil {
			s.Reasons = append(s.Reasons, k+": not available")
			continue
		}
		sum += *v
		n++
		s.Reasons = append(s.Reasons, fmt.Sprintf("%s: %.1f%%", k, *v*100))
	}
	if n == 0 {
		s.Reasons = append(s.Reasons, "no growth rates available")
		return s
	}
	avg := sum / float64(n)
	score := 1.0
	for _, edge := range c.GrowthBands {
		if avg >= edge {
			score++
		}
	}
	s.Value = &score
	s.Reasons = append(s.Reasons, fmt.Sprintf("average %.1f%% → %.0f/5", avg*100, score))
	return s
}

// HealthInputs are the numbers the financial health score needs.
type HealthInputs struct {
	EBIT, InterestExpense *float64 // TTM
	TotalDebt, Equity     *float64 // latest balance sheet
	FCF                   *float64 // TTM
	FCFByYear             []*float64
}

// HealthScore awards one point per passing criterion. Criteria that cannot be
// evaluated (missing data) are left out and the score is scaled to the ones
// that could be, so a company is not penalised for a value it does not report.
// With all four criteria evaluated this is the plan's "1 + points".
func (c Config) HealthScore(in HealthInputs) Score {
	s := Score{Max: 5}
	points, used := 0, 0
	check := func(ok, known bool, pass, fail, unknown string) {
		switch {
		case !known:
			s.Reasons = append(s.Reasons, "skipped: "+unknown)
		case ok:
			points++
			used++
			s.Reasons = append(s.Reasons, "✓ "+pass)
		default:
			used++
			s.Reasons = append(s.Reasons, "✗ "+fail)
		}
	}

	// Interest coverage. No debt means nothing to cover: pass.
	switch {
	case in.TotalDebt != nil && *in.TotalDebt == 0:
		check(true, true, "no debt, so interest coverage is not a concern", "", "")
	case in.EBIT != nil && *in.EBIT <= 0:
		check(false, true, "", "operating loss: earnings do not cover interest", "")
	case in.EBIT != nil && in.InterestExpense != nil && *in.InterestExpense > 0:
		cov := *in.EBIT / *in.InterestExpense
		check(cov >= c.MinInterestCoverage, true,
			fmt.Sprintf("EBIT covers interest %.1f× (≥ %.0f×)", cov, c.MinInterestCoverage),
			fmt.Sprintf("EBIT covers interest only %.1f× (< %.0f×)", cov, c.MinInterestCoverage), "")
	default:
		check(false, false, "", "", "interest expense not reported")
	}

	// Debt / equity.
	switch {
	case in.TotalDebt == nil || in.Equity == nil:
		check(false, false, "", "", "debt or equity not reported")
	case *in.Equity <= 0:
		check(false, true, "", "shareholders' equity is negative or zero", "")
	default:
		de := *in.TotalDebt / *in.Equity
		check(de <= c.MaxDebtToEquity, true,
			fmt.Sprintf("debt/equity %.2f (≤ %.1f)", de, c.MaxDebtToEquity),
			fmt.Sprintf("debt/equity %.2f (> %.1f)", de, c.MaxDebtToEquity), "")
	}

	// FCF vs debt.
	switch {
	case in.TotalDebt != nil && *in.TotalDebt == 0:
		check(true, true, "no debt to repay", "", "")
	case in.TotalDebt == nil || in.FCF == nil:
		check(false, false, "", "", "free cash flow or debt not reported")
	default:
		ratio := *in.FCF / *in.TotalDebt
		check(ratio >= c.MinFCFToDebt, true,
			fmt.Sprintf("yearly FCF is %.0f%% of total debt (≥ %.0f%%)", ratio*100, c.MinFCFToDebt*100),
			fmt.Sprintf("yearly FCF is %.0f%% of total debt (< %.0f%%)", ratio*100, c.MinFCFToDebt*100), "")
	}

	// Positive FCF for N straight years.
	if len(in.FCFByYear) < c.PositiveFCFYears {
		check(false, false, "", "", fmt.Sprintf("fewer than %d years of free cash flow", c.PositiveFCFYears))
	} else {
		recent := in.FCFByYear[len(in.FCFByYear)-c.PositiveFCFYears:]
		allKnown, allPositive := true, true
		for _, v := range recent {
			if v == nil {
				allKnown = false
			} else if *v <= 0 {
				allPositive = false
			}
		}
		check(allPositive, allKnown,
			fmt.Sprintf("positive FCF in each of the last %d years", c.PositiveFCFYears),
			fmt.Sprintf("FCF was not positive in each of the last %d years", c.PositiveFCFYears),
			"free cash flow missing for a recent year")
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

// ValuationScore maps the current value's percentile within its own history
// (0 = cheapest ever, 1 = most expensive) onto 1–5.
func (c Config) ValuationScore(percentile float64) float64 {
	for i, cut := range c.ValuationScoreCuts {
		if percentile <= cut {
			return float64(5 - i)
		}
	}
	return 1
}

// Percentile returns the value at quantile q (0–1) of sorted values, using
// linear interpolation between the closest ranks.
func Percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := q * float64(len(sorted)-1)
	lo := int(pos)
	if lo >= len(sorted)-1 {
		return sorted[len(sorted)-1]
	}
	frac := pos - float64(lo)
	return sorted[lo] + frac*(sorted[lo+1]-sorted[lo])
}

// Rank returns the fraction of values at or below v (0–1).
func Rank(sorted []float64, v float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	n := sort.Search(len(sorted), func(i int) bool { return sorted[i] > v })
	return float64(n) / float64(len(sorted))
}
