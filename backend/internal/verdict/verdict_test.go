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

	// All five: 0.30×4.5 + 0.25×4 + 0.20×5 + 0.15×5 + 0.10×3 = 4.4
	all := Input{BusinessQuality: f(4.5), Growth: f(4), FinancialHealth: f(5), MoatWidth: f(5), MoatDirection: f(3), BusinessRisk: f(3)}
	b, comps := composite(all, cfg)
	if b == nil || *b != 4.4 || comps[0].Weight != 0.3 || len(comps) != 5 {
		t.Fatalf("B = %v, comps %+v", b, comps)
	}

	// No moat or risk (no AI analysis): the other three share the weight.
	b, comps = composite(Input{BusinessQuality: f(4.5), Growth: f(4), FinancialHealth: f(5)}, cfg)
	if b == nil || *b != 4.47 || comps[0].Weight != 0.4 || comps[3].Weight != 0 {
		t.Fatalf("B = %v, comps %+v", b, comps)
	}

	// Nothing: no composite.
	if b, _ := composite(Input{}, cfg); b != nil {
		t.Errorf("B = %v, want nil", *b)
	}
}

func TestMoatDirectionNudgesWidth(t *testing.T) {
	m := scoring.Default().Moat
	for _, tc := range []struct{ width, dir, want float64 }{
		{4, 3, 4},    // Broad, stable
		{4, 5, 4.5},  // Broad, widening
		{4, 1, 3.5},  // Broad, narrowing
		{5, 5, 5},    // capped at 5
		{1, 1, 1},    // and at 1
		{3, 4, 3.25}, // Narrow, slightly widening
	} {
		if got := moatScore(Input{MoatWidth: f(tc.width), MoatDirection: f(tc.dir)}, m); *got != tc.want {
			t.Errorf("width %v direction %v: %v, want %v", tc.width, tc.dir, *got, tc.want)
		}
	}
}

func TestInsiderAdjustment(t *testing.T) {
	cfg := scoring.Default()
	base := Input{BusinessQuality: f(4), Growth: f(4), FinancialHealth: f(4), Valuation: f(4)}
	cap := 180e9

	// PepsiCo-like: a director bought $23.7M, insiders sold $6.2M → +0.25.
	in := base
	in.Insiders = &Insiders{BoughtUSD: 23.7e6, SoldUSD: 6.2e6, MarketCap: &cap}
	r := Decide(in, cfg)
	if *r.Business != 4.25 || len(r.Adjustments) != 1 || r.Adjustments[0].Points != 0.25 {
		t.Errorf("buying: B %v, adjustments %+v", *r.Business, r.Adjustments)
	}

	// Heavy unplanned selling (0.5% of the company), no buying → −0.25.
	in.Insiders = &Insiders{SoldUSD: 1e9, PlannedSoldUSD: 0.1e9, MarketCap: &cap}
	if r := Decide(in, cfg); *r.Business != 3.75 || !strings.Contains(r.Adjustments[0].Reason, "outside pre-arranged plans") {
		t.Errorf("selling: B %v, adjustments %+v", *r.Business, r.Adjustments)
	}

	// Routine selling, mostly planned and small for the company: no change.
	in.Insiders = &Insiders{SoldUSD: 50e6, PlannedSoldUSD: 40e6, MarketCap: &cap}
	if r := Decide(in, cfg); *r.Business != 4 || len(r.Adjustments) != 0 {
		t.Errorf("routine: B %v, adjustments %+v", *r.Business, r.Adjustments)
	}
}

func TestDecide(t *testing.T) {
	cfg := scoring.Default()
	// Visa-like: quality 4.5, growth 4, health 5 → B 4.47; valuation 2 → Watch.
	r := Decide(Input{BusinessQuality: f(4.5), Growth: f(4), FinancialHealth: f(5), Valuation: f(2)}, cfg)
	if r.Label != Watch || *r.Business != 4.47 || r.Meaning == "" || r.ScoringVersion != cfg.Version {
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

func TestLimitedSupportLowersConfidence(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	full := Input{BusinessQuality: f(4), Growth: f(4), FinancialHealth: f(4), Valuation: f(4)}
	c1, _ := confidence(full)
	full.LimitedSupport = "bank"
	c2, reasons := confidence(full)
	if c1-c2 != 25 || !strings.Contains(strings.Join(reasons, " "), "not banks") {
		t.Errorf("confidence %d → %d, reasons %v", c1, c2, reasons)
	}
}
