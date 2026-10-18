// Package verdict turns the scores into Sell / Pass / Hold / Watch / Buy,
// with the rule that decided it and a confidence level. It is deterministic:
// the LLM only writes the narrative afterwards.
package verdict

import (
	"fmt"
	"math"

	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

// Labels.
const (
	Buy   = "Buy"
	Watch = "Watch"
	Hold  = "Hold"
	Pass  = "Pass"
	Sell  = "Sell"
)

var meanings = map[string]string{
	Buy:   "Strong business at an attractive price",
	Watch: "Great business; wait for a better price",
	Hold:  "Solid business at a fair price",
	Pass:  "Not compelling at this price",
	Sell:  "Weak business, or a mediocre one that is expensive",
}

// Input holds the scores (1–5; nil when unavailable) and what lowers
// confidence in them.
type Input struct {
	BusinessQuality *float64
	Growth          *float64
	FinancialHealth *float64
	Valuation       *float64

	ValuationFellBack  bool     // the phase's valuation metric lacked data
	PhaseLowConfidence bool     // the lifecycle phase is a weak fit
	MissingData        []string // data sources that failed (prices, earnings, ...)
	FailedAnalysis     []string // AI sections that failed (business_overview, ...)
	// LimitedSupport is the kind of company the metrics do not fit (bank,
	// broker, insurer, reit), or "".
	LimitedSupport string
}

// Component is one score in the business composite.
type Component struct {
	Name   string   `json:"name"`
	Score  *float64 `json:"score"`
	Weight float64  `json:"weight"` // after renormalizing over available scores
}

// Result is the verdict and everything needed to explain it.
type Result struct {
	Label      string      `json:"label"` // empty when no verdict can be given
	Meaning    string      `json:"meaning"`
	Business   *float64    `json:"business"`  // B, the business composite
	Valuation  *float64    `json:"valuation"` // V, the valuation score
	Components []Component `json:"components"`
	Rule       string      `json:"rule"` // which rule decided, with the numbers

	Confidence        int      `json:"confidence"` // 0–100
	ConfidenceLevel   string   `json:"confidenceLevel"`
	ConfidenceReasons []string `json:"confidenceReasons"`
	ScoringVersion    string   `json:"scoringVersion"`
}

// Decide applies the verdict rules from cfg.
func Decide(in Input, cfg scoring.Config) Result {
	r := Result{ScoringVersion: cfg.Version}
	r.Business, r.Components = composite(in, cfg.Weights)
	r.Valuation = in.Valuation
	r.Confidence, r.ConfidenceReasons = confidence(in)
	r.ConfidenceLevel = level(r.Confidence, cfg.Verdict)

	if r.Business == nil {
		r.Rule = "no business scores available, so no verdict"
		return r
	}
	r.Label, r.Rule = apply(*r.Business, r.Valuation, cfg.Verdict)
	if r.Label == Buy && in.BusinessQuality == nil && cfg.Verdict.BuyNeedsQuality {
		r.Label = Watch
		r.Rule = fmt.Sprintf("Watch: would be %s, but there is no business quality score", r.Rule)
	}
	r.Meaning = meanings[r.Label]
	return r
}

// composite is the weighted mean of the available scores; weights are
// renormalized over the scores that exist. Rounded to 2 decimals so rule
// boundaries are not decided by floating-point noise.
func composite(in Input, w scoring.Weights) (*float64, []Component) {
	comps := []Component{
		{Name: "Business quality", Score: in.BusinessQuality, Weight: w.BusinessQuality},
		{Name: "Growth", Score: in.Growth, Weight: w.Growth},
		{Name: "Financial health", Score: in.FinancialHealth, Weight: w.FinancialHealth},
	}
	var total float64
	for _, c := range comps {
		if c.Score != nil {
			total += c.Weight
		}
	}
	if total == 0 {
		for i := range comps {
			comps[i].Weight = 0
		}
		return nil, comps
	}
	var sum float64
	for i := range comps {
		if comps[i].Score == nil {
			comps[i].Weight = 0
			continue
		}
		comps[i].Weight = round(comps[i].Weight/total, 4)
		sum += *comps[i].Score * comps[i].Weight
	}
	b := round(sum, 2)
	return &b, comps
}

// apply checks the rules in order; the first match wins. Conditions on V are
// false when V is unknown.
func apply(b float64, v *float64, t scoring.VerdictRules) (string, string) {
	vIs := func(cmp func(float64) bool) bool { return v != nil && cmp(*v) }
	vs := "unknown"
	if v != nil {
		vs = fmt.Sprintf("%g", *v)
	}

	switch {
	case b < t.SellBelow:
		return Sell, fmt.Sprintf("Sell: business score B %.2f is below %g", b, t.SellBelow)
	case b < t.SellWeakBelow && vIs(func(x float64) bool { return x <= t.SellWeakMaxV }):
		return Sell, fmt.Sprintf("Sell: B %.2f is below %g and valuation V %s is %g or lower (expensive)", b, t.SellWeakBelow, vs, t.SellWeakMaxV)
	case b >= t.BuyMinB && vIs(func(x float64) bool { return x >= t.BuyMinV }):
		return Buy, fmt.Sprintf("Buy: B %.2f is at least %g and V %s is at least %g", b, t.BuyMinB, vs, t.BuyMinV)
	case b >= t.BuyCheapMinB && vIs(func(x float64) bool { return x >= t.BuyCheapMinV }):
		return Buy, fmt.Sprintf("Buy: B %.2f is at least %g and V %s is at least %g (very cheap)", b, t.BuyCheapMinB, vs, t.BuyCheapMinV)
	case b >= t.WatchMinB:
		return Watch, fmt.Sprintf("Watch: B %.2f is at least %g, but V %s is below %g", b, t.WatchMinB, vs, t.BuyMinV)
	case b >= t.HoldMinB && vIs(func(x float64) bool { return x >= t.HoldMinV }):
		return Hold, fmt.Sprintf("Hold: B %.2f is at least %g and V %s is at least %g", b, t.HoldMinB, vs, t.HoldMinV)
	default:
		return Pass, fmt.Sprintf("Pass: B %.2f and V %s match no Buy, Watch or Hold rule", b, vs)
	}
}

// confidence starts at 100 and subtracts for each gap in the inputs.
func confidence(in Input) (int, []string) {
	score := 100
	var reasons []string
	take := func(points int, why string) {
		score -= points
		reasons = append(reasons, fmt.Sprintf("−%d: %s", points, why))
	}
	if in.BusinessQuality == nil {
		take(30, "no business quality score (AI analysis unavailable)")
	}
	if in.Growth == nil {
		take(15, "no growth score")
	}
	if in.FinancialHealth == nil {
		take(15, "no financial health score")
	}
	if in.Valuation == nil {
		take(30, "no valuation score")
	} else if in.ValuationFellBack {
		take(10, "valued with a fallback metric")
	}
	if in.PhaseLowConfidence {
		take(15, "lifecycle phase is a weak fit")
	}
	for _, s := range in.MissingData {
		take(10, "missing data: "+s)
	}
	if in.LimitedSupport != "" {
		take(25, "limited support: the metrics are built for operating companies, not "+in.LimitedSupport+"s")
	}
	for _, s := range in.FailedAnalysis {
		take(10, "AI analysis failed: "+s)
	}
	if len(reasons) == 0 {
		reasons = []string{"all scores and data available"}
	}
	return max(score, 0), reasons
}

func level(c int, t scoring.VerdictRules) string {
	switch {
	case float64(c) >= t.HighConfidenceMin:
		return "high"
	case float64(c) >= t.MediumConfidenceMin:
		return "medium"
	default:
		return "low"
	}
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}
