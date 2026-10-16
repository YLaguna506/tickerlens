package scoring

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// Load reads a scoring config file on top of Default(): keys missing from the
// file keep their default value, and unknown keys are an error (a typo should
// not silently fall back to a default).
func Load(path string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("scoring config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("scoring config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("scoring config %s: %w", path, err)
	}
	return cfg, nil
}

// Validate catches settings that would make the scores meaningless.
func (c Config) Validate() error {
	var errs []error
	if c.Version == "" {
		errs = append(errs, errors.New("version is required"))
	}
	if !sort.Float64sAreSorted(c.GrowthBands[:]) {
		errs = append(errs, errors.New("growth_bands must be in increasing order"))
	}
	if !sort.Float64sAreSorted(c.ValuationScoreCuts[:]) {
		errs = append(errs, errors.New("valuation_score_cuts must be in increasing order"))
	}
	if c.CheapPercentile >= c.ExpensivePercentile {
		errs = append(errs, errors.New("cheap_percentile must be below expensive_percentile"))
	}
	if c.MaxBasisSwing <= 1 {
		errs = append(errs, errors.New("max_basis_swing must be above 1"))
	}
	w := c.Weights
	if w.BusinessQuality < 0 || w.Growth < 0 || w.FinancialHealth < 0 || w.BusinessQuality+w.Growth+w.FinancialHealth == 0 {
		errs = append(errs, errors.New("weights must be non-negative and not all zero"))
	}
	v := c.Verdict
	if v.SellBelow > v.SellWeakBelow || v.HoldMinB > v.BuyMinB {
		errs = append(errs, errors.New("verdict thresholds out of order (sell_below ≤ sell_weak_below, hold_min_b ≤ buy_min_b)"))
	}
	if v.MediumConfidenceMin > v.HighConfidenceMin {
		errs = append(errs, errors.New("medium_confidence_min must not exceed high_confidence_min"))
	}
	return errors.Join(errs...)
}
