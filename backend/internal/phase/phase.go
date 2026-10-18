// Package phase places a company in one of five lifecycle phases, which
// decides the valuation metric used for it.
package phase

import "fmt"

// Valuation metrics a phase can call for.
const (
	PS   = "P/S"
	PE   = "P/E"
	PFCF = "P/FCF"
	// Banks, brokers and insurers are valued on book value, REITs on funds
	// from operations (see metrics/sector.go); the phases never pick these.
	PB   = "P/B"
	PFFO = "P/FFO"
)

// Inputs are the computed numbers the classifier needs. Nil means unknown.
type Inputs struct {
	RevenueTTM          *float64
	RevenueYoY          *float64 // TTM vs TTM a year earlier (fraction)
	RevenueCAGR3y       *float64 // fiscal years (fraction)
	NetIncomeTTM        *float64
	NetIncomeTTMYearAgo *float64
	NetMarginTTM        *float64
	OperatingMargin     *float64 // latest fiscal year
	OperatingMargin3y   *float64 // fiscal year three years earlier
	ShareholderYield    *float64 // (dividends + buybacks) / market cap
	PayoutOfFCF         *float64 // (dividends + buybacks) / FCF
}

// Check is one of the three yes/no questions shown in the report.
type Check struct {
	Question string `json:"question"`
	Answer   *bool  `json:"answer"` // nil when unknown
	Detail   string `json:"detail"`
}

// Result is the classification and why.
type Result struct {
	Number          int      `json:"number"` // 1–5
	Name            string   `json:"name"`
	ValuationMetric string   `json:"valuationMetric"`
	Fallbacks       []string `json:"fallbacks"` // tried in order when the main metric has too little data
	LowConfidence   bool     `json:"lowConfidence"`
	Reason          string   `json:"reason"`
	Checks          []Check  `json:"checks"`
}

const startupRevenue = 100e6

// Classify applies the rules in order; the first match wins.
func Classify(in Inputs) Result {
	checks := []Check{revenueGrowingCheck(in), profitableCheck(in), returningCapitalCheck(in)}
	r := Result{Checks: checks}

	profitable := in.NetIncomeTTM != nil && *in.NetIncomeTTM > 0
	growing := in.RevenueYoY != nil && *in.RevenueYoY > 0

	switch {
	case lt(in.RevenueCAGR3y, 0) && in.OperatingMargin != nil && in.OperatingMargin3y != nil &&
		*in.OperatingMargin < *in.OperatingMargin3y:
		r.Number, r.Name, r.ValuationMetric = 5, "Decline", PFCF
		r.Fallbacks = []string{PE, PS}
		r.Reason = fmt.Sprintf("revenue shrank %.1f%%/yr over 3 years and operating margin fell from %.1f%% to %.1f%%",
			-*in.RevenueCAGR3y*100, *in.OperatingMargin3y*100, *in.OperatingMargin*100)

	case lt(in.NetIncomeTTM, 0) && (lt(in.RevenueTTM, startupRevenue) || lossesWidening(in)):
		r.Number, r.Name, r.ValuationMetric = 1, "Startup", PS
		r.LowConfidence = true
		if lt(in.RevenueTTM, startupRevenue) {
			r.Reason = fmt.Sprintf("losing money with revenue under $100M (%s)", money(in.RevenueTTM))
		} else {
			r.Reason = fmt.Sprintf("losses widened from %s to %s over the past year", money(in.NetIncomeTTMYearAgo), money(in.NetIncomeTTM))
		}

	case (le(in.NetIncomeTTM, 0) || lt(in.NetMarginTTM, 0.03)) && gt(in.RevenueYoY, 0.20):
		r.Number, r.Name, r.ValuationMetric = 2, "Hypergrowth", PS
		r.Reason = fmt.Sprintf("revenue up %.0f%% in a year while net margin is %s", *in.RevenueYoY*100, pct(in.NetMarginTTM))

	case profitable && growing && (ge(in.ShareholderYield, 0.01) || ge(in.PayoutOfFCF, 0.25)):
		r.Number, r.Name, r.ValuationMetric = 4, "Capital return", PE
		r.Fallbacks = []string{PFCF}
		r.Reason = fmt.Sprintf("profitable, growing, and returning capital (shareholder yield %s, %s of FCF)",
			pct(in.ShareholderYield), pct(in.PayoutOfFCF))

	case profitable:
		r.Number, r.Name, r.ValuationMetric = 3, "Operating leverage", PFCF
		r.Fallbacks = []string{PE}
		r.Reason = "profitable, but not yet returning much capital (or revenue not growing)"

	default:
		// Not in the plan's table: losing money, losses not widening, revenue
		// over $100M and growing ≤ 20%. Treated as an early-stage business.
		r.Number, r.Name, r.ValuationMetric = 1, "Startup", PS
		r.LowConfidence = true
		r.Reason = "not profitable and not growing fast enough for hypergrowth"
	}
	if r.Number == 1 || r.Number == 2 {
		r.Fallbacks = []string{}
	}
	return r
}

func lossesWidening(in Inputs) bool {
	return in.NetIncomeTTM != nil && in.NetIncomeTTMYearAgo != nil && *in.NetIncomeTTM < *in.NetIncomeTTMYearAgo
}

func revenueGrowingCheck(in Inputs) Check {
	c := Check{Question: "Is revenue growing?"}
	if in.RevenueYoY == nil {
		c.Detail = "not enough quarterly data"
		return c
	}
	c.Answer = boolPtr(*in.RevenueYoY > 0)
	c.Detail = fmt.Sprintf("TTM revenue %+.1f%% vs a year earlier", *in.RevenueYoY*100)
	return c
}

func profitableCheck(in Inputs) Check {
	c := Check{Question: "Is it profitable?"}
	if in.NetIncomeTTM == nil {
		c.Detail = "net income not reported"
		return c
	}
	c.Answer = boolPtr(*in.NetIncomeTTM > 0)
	c.Detail = fmt.Sprintf("TTM net income %s (net margin %s)", money(in.NetIncomeTTM), pct(in.NetMarginTTM))
	return c
}

func returningCapitalCheck(in Inputs) Check {
	c := Check{Question: "Is it returning capital to shareholders?"}
	if in.ShareholderYield == nil && in.PayoutOfFCF == nil {
		c.Detail = "dividends and buybacks not reported"
		return c
	}
	c.Answer = boolPtr(ge(in.ShareholderYield, 0.01) || ge(in.PayoutOfFCF, 0.25))
	c.Detail = fmt.Sprintf("dividends + buybacks = %s of market cap, %s of FCF", pct(in.ShareholderYield), pct(in.PayoutOfFCF))
	return c
}

// Comparisons that treat an unknown value as "does not satisfy".
func lt(v *float64, x float64) bool { return v != nil && *v < x }
func le(v *float64, x float64) bool { return v != nil && *v <= x }
func gt(v *float64, x float64) bool { return v != nil && *v > x }
func ge(v *float64, x float64) bool { return v != nil && *v >= x }

func boolPtr(b bool) *bool { return &b }

func pct(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", *v*100)
}

func money(v *float64) string {
	if v == nil {
		return "n/a"
	}
	sign, abs := "", *v
	if abs < 0 {
		sign, abs = "-", -abs
	}
	if abs >= 1e9 {
		return fmt.Sprintf("%s$%.1fB", sign, abs/1e9)
	}
	return fmt.Sprintf("%s$%.0fM", sign, abs/1e6)
}
