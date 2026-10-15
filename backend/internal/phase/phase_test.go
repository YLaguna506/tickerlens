package phase

import "testing"

func f(v float64) *float64 { return &v }

func TestClassify(t *testing.T) {
	tests := []struct {
		name        string
		in          Inputs
		wantNumber  int
		wantMetric  string
		wantLowConf bool
	}{
		{
			name: "decline: shrinking revenue and falling margin",
			in: Inputs{RevenueTTM: f(5e9), RevenueYoY: f(-0.04), RevenueCAGR3y: f(-0.03), NetIncomeTTM: f(2e8),
				OperatingMargin: f(0.08), OperatingMargin3y: f(0.12)},
			wantNumber: 5, wantMetric: PFCF,
		},
		{
			name: "shrinking revenue but improving margin is not decline",
			in: Inputs{RevenueTTM: f(5e9), RevenueYoY: f(-0.04), RevenueCAGR3y: f(-0.03), NetIncomeTTM: f(2e8),
				NetMarginTTM: f(0.04), OperatingMargin: f(0.12), OperatingMargin3y: f(0.08)},
			wantNumber: 3, wantMetric: PFCF,
		},
		{
			name:       "startup: small and losing money",
			in:         Inputs{RevenueTTM: f(40e6), RevenueYoY: f(0.8), NetIncomeTTM: f(-30e6)},
			wantNumber: 1, wantMetric: PS, wantLowConf: true,
		},
		{
			name: "startup: losses widening",
			in: Inputs{RevenueTTM: f(900e6), RevenueYoY: f(0.5), NetIncomeTTM: f(-400e6),
				NetIncomeTTMYearAgo: f(-200e6)},
			wantNumber: 1, wantMetric: PS, wantLowConf: true,
		},
		{
			name: "hypergrowth: losses narrowing, revenue +26% (Snowflake)",
			in: Inputs{RevenueTTM: f(5.4e9), RevenueYoY: f(0.26), NetIncomeTTM: f(-1.09e9),
				NetIncomeTTMYearAgo: f(-1.3e9), NetMarginTTM: f(-0.2)},
			wantNumber: 2, wantMetric: PS,
		},
		{
			name:       "hypergrowth: thin profit, fast growth",
			in:         Inputs{RevenueTTM: f(2e9), RevenueYoY: f(0.35), NetIncomeTTM: f(20e6), NetMarginTTM: f(0.01)},
			wantNumber: 2, wantMetric: PS,
		},
		{
			name: "capital return: profitable, growing, 2.5% shareholder yield (Apple, Visa)",
			in: Inputs{RevenueTTM: f(400e9), RevenueYoY: f(0.08), NetIncomeTTM: f(100e9), NetMarginTTM: f(0.25),
				ShareholderYield: f(0.025), PayoutOfFCF: f(0.9)},
			wantNumber: 4, wantMetric: PE,
		},
		{
			name: "capital return via payout of FCF only",
			in: Inputs{RevenueTTM: f(1e9), RevenueYoY: f(0.05), NetIncomeTTM: f(1e8), NetMarginTTM: f(0.1),
				ShareholderYield: f(0.005), PayoutOfFCF: f(0.3)},
			wantNumber: 4, wantMetric: PE,
		},
		{
			name: "operating leverage: profitable, little capital returned",
			in: Inputs{RevenueTTM: f(1e9), RevenueYoY: f(0.15), NetIncomeTTM: f(1e8), NetMarginTTM: f(0.1),
				ShareholderYield: f(0.002), PayoutOfFCF: f(0.05)},
			wantNumber: 3, wantMetric: PFCF,
		},
		{
			name: "unprofitable, slow growth, losses narrowing: falls back to startup",
			in: Inputs{RevenueTTM: f(800e6), RevenueYoY: f(0.05), NetIncomeTTM: f(-50e6),
				NetIncomeTTMYearAgo: f(-80e6), NetMarginTTM: f(-0.06)},
			wantNumber: 1, wantMetric: PS, wantLowConf: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := Classify(tc.in)
			if r.Number != tc.wantNumber || r.ValuationMetric != tc.wantMetric || r.LowConfidence != tc.wantLowConf {
				t.Errorf("got phase %d (%s, %s, lowConf=%v), want %d (%s, lowConf=%v); reason: %s",
					r.Number, r.Name, r.ValuationMetric, r.LowConfidence, tc.wantNumber, tc.wantMetric, tc.wantLowConf, r.Reason)
			}
			if len(r.Checks) != 3 {
				t.Errorf("want 3 checks, got %d", len(r.Checks))
			}
		})
	}
}

func TestChecksWithUnknowns(t *testing.T) {
	r := Classify(Inputs{})
	for _, c := range r.Checks {
		if c.Answer != nil {
			t.Errorf("%q: answer should be unknown, got %v", c.Question, *c.Answer)
		}
	}
}
