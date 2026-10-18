package metrics

import "testing"

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
