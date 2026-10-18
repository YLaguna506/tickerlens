package metrics

import (
	"strings"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

func TestLimitedSupportByIndustryCode(t *testing.T) {
	for sic, want := range map[string]string{"6021": "bank", "6211": "broker", "6331": "insurer", "6798": "reit", "7389": "", "2080": ""} {
		ls := limitedSupport(sic)
		if got := ""; ls != nil {
			got = ls.Kind
			if got != want {
				t.Errorf("SIC %s: %q, want %q", sic, got, want)
			}
		} else if want != "" {
			t.Errorf("SIC %s: none, want %q", sic, want)
		}
	}
	if limitedSupport("6798").NoHealthScore {
		t.Error("REITs keep the health score")
	}
}

// A bank's debt is mostly deposits: no health score, with the reason.
func TestBankHasNoHealthScore(t *testing.T) {
	b := loadBundle(t, "V")
	b.Industry = &edgar.Profile{SIC: "6021", SICDescription: "National Commercial Banks"}
	r := Compute(b, scoring.Default())
	if r.LimitedSupport == nil || r.LimitedSupport.Kind != "bank" {
		t.Fatalf("limited support = %+v", r.LimitedSupport)
	}
	if r.HealthScore.Value != nil || !strings.Contains(strings.Join(r.HealthScore.Reasons, " "), "banks") {
		t.Errorf("health score = %+v", r.HealthScore)
	}
}
