package verdict

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

func f(v float64) *float64 { return &v }

// TestRuleBoundaries walks every rule edge from the plan:
//
//	Sell: B < 2.5, or (B < 3.0 and V ≤ 2)
//	Buy: (B ≥ 4.0 and V ≥ 4) or (B ≥ 3.5 and V = 5)
//	Watch: B ≥ 4.0
//	Hold: B ≥ 3.0 and V ≥ 3
//	Pass: everything else
func TestRuleBoundaries(t *testing.T) {
	rules := scoring.Default().Verdict
	tests := []struct {
		b    float64
		v    *float64
		want string
	}{
		// Sell: B < 2.5 regardless of price.
		{2.49, f(5), Sell},
		{1.00, f(1), Sell},
		// Sell: B < 3.0 and V ≤ 2.
		{2.50, f(2), Sell},
		{2.99, f(2), Sell},
		{2.99, f(1), Sell},
		// ...but not when V > 2 or B reaches 3.0.
		{2.50, f(3), Pass},
		{2.99, f(3), Pass},
		{3.00, f(2), Pass},
		// Hold: B ≥ 3.0 and V ≥ 3.
		{3.00, f(3), Hold},
		{3.49, f(5), Hold}, // cheap-Buy needs B ≥ 3.5
		{3.50, f(4), Hold}, // B < 4.0, V < 5
		{3.99, f(4), Hold},
		// Buy via the cheap rule: B ≥ 3.5 and V = 5.
		{3.50, f(5), Buy},
		// Buy: B ≥ 4.0 and V ≥ 4.
		{4.00, f(4), Buy},
		{5.00, f(5), Buy},
		// Watch: B ≥ 4.0 but V < 4.
		{4.00, f(3), Watch},
		{4.00, f(1), Watch},
		{4.99, f(3), Watch},
		// Pass: B between 3.0 and 4.0 and V < 3.
		{3.99, f(2), Pass},
		{3.00, f(1), Pass},
		// Unknown V: rules on V cannot match.
		{2.49, nil, Sell},
		{2.99, nil, Pass},
		{3.50, nil, Pass},
		{4.50, nil, Watch},
	}
	for _, tc := range tests {
		label, rule := apply(tc.b, tc.v, rules)
		v := "nil"
		if tc.v != nil {
			v = fmt.Sprint(*tc.v)
		}
		if label != tc.want {
			t.Errorf("B=%.2f V=%s: got %s (%s), want %s", tc.b, v, label, rule, tc.want)
		}
		if !strings.HasPrefix(rule, label+":") {
			t.Errorf("rule text %q should start with %q", rule, label)
		}
	}
}

func TestCompositeRenormalizesMissingScores(t *testing.T) {
	cfg := scoring.Default()

	// All three: 0.4×4.5 + 0.3×4 + 0.3×5 = 4.5
	b, comps := composite(Input{BusinessQuality: f(4.5), Growth: f(4), FinancialHealth: f(5)}, cfg.Weights)
	if b == nil || *b != 4.5 || comps[0].Weight != 0.4 {
		t.Fatalf("B = %v, comps %+v", b, comps)
	}

	// No business quality: growth and health share the weight 50/50.
	b, comps = composite(Input{Growth: f(4), FinancialHealth: f(5)}, cfg.Weights)
	if b == nil || *b != 4.5 || comps[1].Weight != 0.5 || comps[0].Weight != 0 {
		t.Fatalf("B = %v, comps %+v", b, comps)
	}

	// Nothing: no composite.
	if b, _ := composite(Input{}, cfg.Weights); b != nil {
		t.Errorf("B = %v, want nil", *b)
	}
}

func TestDecide(t *testing.T) {
	cfg := scoring.Default()
	// Visa-like: quality 4.5, growth 4, health 5 → B 4.5; valuation 2 → Watch.
	r := Decide(Input{BusinessQuality: f(4.5), Growth: f(4), FinancialHealth: f(5), Valuation: f(2)}, cfg)
	if r.Label != Watch || *r.Business != 4.5 || r.Meaning == "" || r.ScoringVersion != cfg.Version {
		t.Errorf("result = %+v", r)
	}
	if r.Confidence != 100 || r.ConfidenceLevel != "high" {
		t.Errorf("confidence = %d %s %v", r.Confidence, r.ConfidenceLevel, r.ConfidenceReasons)
	}

	// Netflix-like with the AI's quality judgment missing: growth 5 and
	// health 5 alone give B 5 and a cheap price, but a Buy needs quality.
	noQuality := Input{Growth: f(5), FinancialHealth: f(5), Valuation: f(4), FailedAnalysis: []string{"business_quality"}}
	if r := Decide(noQuality, cfg); r.Label != Watch || r.Rule != "Watch: would be Buy: B 5.00 is at least 4 and V 4 is at least 4, but there is no business quality score" {
		t.Errorf("without quality: %s (%s)", r.Label, r.Rule)
	}
	cfg.Verdict.BuyNeedsQuality = false
	if r := Decide(noQuality, cfg); r.Label != Buy {
		t.Errorf("with the cap off: %s, want Buy", r.Label)
	}
	cfg.Verdict.BuyNeedsQuality = true

	if r := Decide(Input{Valuation: f(5)}, cfg); r.Label != "" {
		t.Errorf("no business scores should give no verdict, got %s", r.Label)
	}
}

func TestConfidence(t *testing.T) {
	cfg := scoring.Default()
	tests := []struct {
		name      string
		in        Input
		wantScore int
		wantLevel string
	}{
		{"complete", Input{BusinessQuality: f(4), Growth: f(4), FinancialHealth: f(4), Valuation: f(3)}, 100, "high"},
		{"no AI analysis", Input{Growth: f(4), FinancialHealth: f(4), Valuation: f(3)}, 70, "medium"},
		{"fallback metric and missing prices", Input{BusinessQuality: f(4), Growth: f(4), FinancialHealth: f(4),
			Valuation: f(3), ValuationFellBack: true, MissingData: []string{"earnings"}}, 80, "high"},
		{"weak phase, no valuation, failed overview", Input{BusinessQuality: f(4), Growth: f(4), FinancialHealth: f(4),
			PhaseLowConfidence: true, FailedAnalysis: []string{"business_overview"}}, 45, "low"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := Decide(tc.in, cfg)
			if r.Confidence != tc.wantScore || r.ConfidenceLevel != tc.wantLevel {
				t.Errorf("confidence = %d (%s), want %d (%s); reasons %v",
					r.Confidence, r.ConfidenceLevel, tc.wantScore, tc.wantLevel, r.ConfidenceReasons)
			}
		})
	}
}
