// Package scoring turns computed metrics into 1–5 scores. Every threshold
// and weight lives in Config, loaded from config/scoring.yaml, so the model
// can be tuned without code changes.
package scoring

import (
	"fmt"
	"sort"
)

// Config holds every scoring threshold and weight. The yaml tags match
// config/scoring.yaml.
type Config struct {
	// Version is stored on every report, so results can be traced to rules.
	Version string `yaml:"version"`

	// Growth: average CAGR (fraction) → score. A CAGR below Bands[0] scores 1,
	// below Bands[1] scores 2, ..., at or above Bands[3] scores 5.
	GrowthBands [4]float64 `yaml:"growth_bands"`

	// Health criteria; each passing criterion earns one point.
	MinInterestCoverage   float64 `yaml:"min_interest_coverage"`    // EBIT / interest expense ≥ this
	MaxDebtToEquity       float64 `yaml:"max_debt_to_equity"`       // debt / equity ≤ this
	MinFCFToDebt          float64 `yaml:"min_fcf_to_debt"`          // FCF / total debt ≥ this
	PositiveFCFYears      int     `yaml:"positive_fcf_years"`       // FCF > 0 in each of the last N fiscal years
	MinHealthCriteriaUsed int     `yaml:"min_health_criteria_used"` // fewer evaluable criteria → no score

	// Valuation bands are percentiles of the metric's own history.
	CheapPercentile     float64 `yaml:"cheap_percentile"`     // ≤ this is "cheap"
	ExpensivePercentile float64 `yaml:"expensive_percentile"` // ≥ this is "expensive"
	// Current percentile (0–1) → score: ≤ [0] scores 5, ≤ [1] 4, ≤ [2] 3,
	// ≤ [3] 2, above scores 1.
	ValuationScoreCuts [4]float64 `yaml:"valuation_score_cuts"`
	MinHistoryDays     int        `yaml:"min_history_days"` // trading days of valuation history required
	HistoryYears       int        `yaml:"history_years"`    // years of history used for bands
	// A metric's history is distorted when its per-share basis (EPS, FCF or
	// revenue per share) was ≤ 0 or its typical value was more than this many
	// times its lowest; the next metric is used instead.
	MaxBasisSwing float64 `yaml:"max_basis_swing"`

	// Weights of the business composite B (renormalized when a score is missing).
	Weights    Weights         `yaml:"weights"`
	Moat       MoatRules       `yaml:"moat"`
	Insiders   InsiderRules    `yaml:"insiders"`
	ReverseDCF ReverseDCFRules `yaml:"reverse_dcf"`
	// Sector scores banks, brokers and insurers on their own terms.
	Sector SectorRules `yaml:"sector"`
	// Verdict thresholds, applied in order: Sell, Buy, Watch, Hold, else Pass.
	Verdict VerdictRules `yaml:"verdict"`
}

// Weights of the scores that make up the business composite.
type Weights struct {
	BusinessQuality float64 `yaml:"business_quality"`
	Growth          float64 `yaml:"growth"`
	FinancialHealth float64 `yaml:"financial_health"`
	Moat            float64 `yaml:"moat"`
	Risk            float64 `yaml:"risk"`
}

// MoatRules turn the moat agent's width and direction into one 1–5 score:
// width, plus DirectionStep for each step towards widening (minus towards
// narrowing).
type MoatRules struct {
	DirectionStep float64 `yaml:"direction_step"`
}

// ReverseDCFRules set the reverse discounted cash flow model: the yearly
// growth the price implies over Years, discounted at DiscountRate, with
// TerminalGrowth forever after; and how far it may sit from the company's
// own growth before expectations count as low or high.
type ReverseDCFRules struct {
	Years          int     `yaml:"years"`
	DiscountRate   float64 `yaml:"discount_rate"`
	TerminalGrowth float64 `yaml:"terminal_growth"`
	InLineGap      float64 `yaml:"in_line_gap"` // within ± this of actual growth: "in line"
	HighGap        float64 `yaml:"high_gap"`    // above actual by more than this: "very high"
	// ReferenceCap: the company's growth counts as at most this, since few
	// companies keep growing faster for 10 years.
	ReferenceCap float64 `yaml:"reference_cap"`
	// VAdjust moves the valuation score V by expectations level.
	VAdjust struct {
		Low      float64 `yaml:"low"`
		InLine   float64 `yaml:"in_line"`
		High     float64 `yaml:"high"`
		VeryHigh float64 `yaml:"very_high"`
	} `yaml:"v_adjust"`
}

// VPoints is the change to V for an expectations level.
func (r ReverseDCFRules) VPoints(level string) float64 {
	switch level {
	case "low":
		return r.VAdjust.Low
	case "in line":
		return r.VAdjust.InLine
	case "high":
		return r.VAdjust.High
	case "very high":
		return r.VAdjust.VeryHigh
	}
	return 0
}

// InsiderRules adjust B for what insiders did with their own money over the
// last year (open-market trades on Form 4s).
type InsiderRules struct {
	// Buying: bought at least this much, and more than was sold.
	BuyMinUSD float64 `yaml:"buy_min_usd"`
	// Selling: sold at least this share of the market cap outside
	// pre-arranged plans, with no buying.
	SellMinShareOfCap float64 `yaml:"sell_min_share_of_cap"`
	// Points added to B for buying, taken off for selling.
	Points float64 `yaml:"points"`
}

// VerdictRules are the thresholds of the verdict rules. B is the business
// composite, V the valuation score (both 1–5).
type VerdictRules struct {
	SellBelow     float64 `yaml:"sell_below"`      // Sell: B < this
	SellWeakBelow float64 `yaml:"sell_weak_below"` // ...or B < this
	SellWeakMaxV  float64 `yaml:"sell_weak_max_v"` //    and V ≤ this
	BuyMinB       float64 `yaml:"buy_min_b"`       // Buy: B ≥ this
	BuyMinV       float64 `yaml:"buy_min_v"`       //    and V ≥ this
	BuyCheapMinB  float64 `yaml:"buy_cheap_min_b"` // ...or B ≥ this
	BuyCheapMinV  float64 `yaml:"buy_cheap_min_v"` //    and V ≥ this
	WatchMinB     float64 `yaml:"watch_min_b"`     // Watch: B ≥ this
	HoldMinB      float64 `yaml:"hold_min_b"`      // Hold: B ≥ this
	HoldMinV      float64 `yaml:"hold_min_v"`      //    and V ≥ this
	// A Buy needs the AI's business quality judgment; without it the
	// verdict is capped at Watch.
	BuyNeedsQuality     bool    `yaml:"buy_needs_quality"`
	HighConfidenceMin   float64 `yaml:"high_confidence_min"`   // confidence ≥ this is "high"
	MediumConfidenceMin float64 `yaml:"medium_confidence_min"` // confidence ≥ this is "medium"
}

// Default returns the plan's starting thresholds. config/scoring.yaml ships
// with the same values; anything left out of the file keeps its default.
func Default() Config {
	return Config{
		Version:               "2026-10-18.3",
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
		MaxBasisSwing:         3,
		Weights:               Weights{BusinessQuality: 0.30, Growth: 0.25, FinancialHealth: 0.20, Moat: 0.15, Risk: 0.10},
		Moat:                  MoatRules{DirectionStep: 0.25},
		Insiders:              InsiderRules{BuyMinUSD: 1e6, SellMinShareOfCap: 0.002, Points: 0.25},
		ReverseDCF:            defaultReverseDCF(),
		Sector:                defaultSector(),
		Verdict: VerdictRules{
			SellBelow: 2.5, SellWeakBelow: 3.0, SellWeakMaxV: 2,
			BuyMinB: 4.0, BuyMinV: 4, BuyCheapMinB: 3.5, BuyCheapMinV: 5,
			WatchMinB: 4.0,
			HoldMinB:  3.0, HoldMinV: 3,
			BuyNeedsQuality:   true,
			HighConfidenceMin: 80, MediumConfidenceMin: 55,
		},
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

func defaultReverseDCF() ReverseDCFRules {
	r := ReverseDCFRules{Years: 10, DiscountRate: 0.09, TerminalGrowth: 0.025, InLineGap: 0.03, HighGap: 0.08, ReferenceCap: 0.15}
	r.VAdjust.Low, r.VAdjust.VeryHigh = 1, -1
	return r
}
