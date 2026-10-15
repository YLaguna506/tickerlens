package scoring

import (
	"math"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestGrowthScoreBands(t *testing.T) {
	c := Default()
	tests := []struct {
		avg  float64
		want float64
	}{
		{-0.05, 1}, {0, 2}, {0.049, 2}, {0.05, 3}, {0.119, 3}, {0.12, 4}, {0.199, 4}, {0.20, 5}, {0.45, 5},
	}
	for _, tc := range tests {
		s := c.GrowthScore(map[string]*float64{"revenue 3y": f(tc.avg)})
		if s.Value == nil || *s.Value != tc.want {
			t.Errorf("avg %.3f → %v, want %v", tc.avg, s.Value, tc.want)
		}
	}
}

func TestGrowthScoreAveragesAvailableInputs(t *testing.T) {
	s := Default().GrowthScore(map[string]*float64{
		"revenue 3y": f(0.10), "revenue 5y": f(0.14), "EPS 3y": nil, "EPS 5y": f(0.15),
	})
	// (10 + 14 + 15) / 3 = 13% → 4
	if s.Value == nil || *s.Value != 4 {
		t.Fatalf("score = %v, want 4; reasons %v", s.Value, s.Reasons)
	}
	if s := Default().GrowthScore(map[string]*float64{"EPS 3y": nil}); s.Value != nil {
		t.Errorf("no inputs should give no score, got %v", *s.Value)
	}
}

func TestHealthScore(t *testing.T) {
	c := Default()
	tests := []struct {
		name string
		in   HealthInputs
		want *float64
	}{
		{
			name: "all four criteria pass",
			in: HealthInputs{EBIT: f(100), InterestExpense: f(5), TotalDebt: f(50), Equity: f(100), FCF: f(40),
				FCFByYear: []*float64{f(1), f(2), f(3)}},
			want: f(5),
		},
		{
			name: "two of four pass",
			in: HealthInputs{EBIT: f(100), InterestExpense: f(20), TotalDebt: f(300), Equity: f(100), FCF: f(200),
				FCFByYear: []*float64{f(1), f(2), f(3)}},
			want: f(3), // coverage 5× fails, D/E 3 fails, FCF/debt 67% passes, FCF positive passes
		},
		{
			name: "missing interest expense is skipped, not failed (Apple after 2023)",
			in: HealthInputs{EBIT: f(100), TotalDebt: f(50), Equity: f(100), FCF: f(40),
				FCFByYear: []*float64{f(1), f(2), f(3)}},
			want: f(5), // 3 of 3 evaluated
		},
		{
			name: "no debt passes coverage and repayment",
			in: HealthInputs{EBIT: f(-10), TotalDebt: f(0), Equity: f(100), FCF: f(-5),
				FCFByYear: []*float64{f(-1), f(2), f(3)}},
			want: f(4), // 3 of 4
		},
		{
			name: "negative equity fails debt/equity",
			in: HealthInputs{EBIT: f(100), InterestExpense: f(5), TotalDebt: f(50), Equity: f(-10), FCF: f(40),
				FCFByYear: []*float64{f(1), f(2), f(3)}},
			want: f(4),
		},
		{
			name: "too little data gives no score",
			in:   HealthInputs{EBIT: f(100)},
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := c.HealthScore(tc.in)
			if (s.Value == nil) != (tc.want == nil) || (s.Value != nil && *s.Value != *tc.want) {
				t.Errorf("score = %v, want %v; reasons %v", deref(s.Value), deref(tc.want), s.Reasons)
			}
		})
	}
}

func TestValuationScoreBoundaries(t *testing.T) {
	c := Default()
	tests := map[float64]float64{0: 5, 0.10: 5, 0.11: 4, 0.30: 4, 0.31: 3, 0.70: 3, 0.71: 2, 0.90: 2, 0.91: 1, 1: 1}
	for p, want := range tests {
		if got := c.ValuationScore(p); got != want {
			t.Errorf("percentile %.2f → %v, want %v", p, got, want)
		}
	}
}

func TestPercentileAndRank(t *testing.T) {
	s := []float64{10, 20, 30, 40, 50}
	if got := Percentile(s, 0.25); got != 20 {
		t.Errorf("p25 = %v", got)
	}
	if got := Percentile(s, 0.5); got != 30 {
		t.Errorf("p50 = %v", got)
	}
	if got := Percentile(s, 0.1); math.Abs(got-14) > 1e-9 {
		t.Errorf("p10 = %v, want 14", got)
	}
	if got := Rank(s, 30); got != 0.6 {
		t.Errorf("rank(30) = %v, want 0.6", got)
	}
	if got := Rank(s, 5); got != 0 {
		t.Errorf("rank(5) = %v", got)
	}
}

func deref(p *float64) any {
	if p == nil {
		return "nil"
	}
	return *p
}
