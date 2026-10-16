package scoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shipped config must parse and pass validation.
func TestShippedConfigLoads(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "..", "config", "scoring.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version == "" || cfg.Weights.BusinessQuality == 0 || cfg.Verdict.BuyMinB == 0 {
		t.Errorf("config not fully loaded: %+v", cfg)
	}
	if cfg.GrowthBands != Default().GrowthBands {
		t.Errorf("growth bands = %v", cfg.GrowthBands)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scoring.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadKeepsDefaultsForMissingKeys(t *testing.T) {
	cfg, err := Load(writeConfig(t, "version: test\nmin_interest_coverage: 8\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinInterestCoverage != 8 || cfg.MaxDebtToEquity != Default().MaxDebtToEquity || cfg.Version != "test" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoadRejectsBadConfig(t *testing.T) {
	tests := map[string]string{
		"unknown key (typo)":   "version: x\nmin_coverage_of_interest: 8\n",
		"bands out of order":   "version: x\ngrowth_bands: [0.2, 0.1, 0.3, 0.4]\n",
		"all weights zero":     "version: x\nweights: {business_quality: 0, growth: 0, financial_health: 0}\n",
		"cheap above pricey":   "version: x\ncheap_percentile: 0.8\n",
		"verdict out of order": "version: x\nverdict: {hold_min_b: 4.5}\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, body)); err == nil {
				t.Errorf("expected an error for %q", strings.TrimSpace(body))
			}
		})
	}
}
