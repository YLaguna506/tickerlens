package metrics

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/phase"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

// Bands compares today's valuation with the company's own history.
type Bands struct {
	Metric         string  `json:"metric"`                   // P/E, P/S or P/FCF
	FellBackFrom   string  `json:"fellBackFrom,omitempty"`   // the phase's metric, when it was not usable
	FallbackReason string  `json:"fallbackReason,omitempty"` // why, e.g. "lacked enough history"
	HistoryFrom    string  `json:"historyFrom"`
	HistoryTo      string  `json:"historyTo"`
	Days           int     `json:"days"`
	Current        float64 `json:"current"`
	Cheap          float64 `json:"cheapBelow"`     // percentile edge (e.g. 25th)
	Median         float64 `json:"median"`         // the typical multiple over the period
	Expensive      float64 `json:"expensiveAbove"` // percentile edge (e.g. 75th)
	Percentile     float64 `json:"percentile"`     // share of history at or below today (0–1)
	Zone           string  `json:"zone"`           // cheap / fair / expensive

	// The price bar: band edges translated into share prices.
	PerShareBasis  string  `json:"perShareBasis"` // what the multiple is applied to
	PerShare       float64 `json:"perShare"`
	BasisPeriod    string  `json:"basisPeriod"`
	Price          float64 `json:"price"`
	CheapPrice     float64 `json:"cheapPrice"`
	MedianPrice    float64 `json:"medianPrice"`
	ExpensivePrice float64 `json:"expensivePrice"`
}

// valuationBands tries the phase's metric, then its fallbacks, then P/S, and
// scores the first one with enough history that is not distorted. A sector
// (banks, insurers, REITs) supplies its own metrics instead of the phase's.
func valuationBands(b *ingest.Bundle, cfg scoring.Config, ph phase.Result, sector []string, rolling []ttm, sp splits, price *float64) (*Bands, scoring.Score, []Point, []string) {
	score := scoring.Score{Max: 5}
	var notes []string
	if price == nil || len(b.Prices) == 0 {
		score.Reasons = []string{"no price data"}
		return nil, score, nil, notes
	}

	if d := recentSpinOff(b, rolling); d != "" {
		score.Reasons = []string{"no valuation: " + d}
		return nil, score, nil, []string{"valuation: " + d}
	}

	candidates := append([]string{ph.ValuationMetric}, ph.Fallbacks...)
	if len(sector) > 0 {
		candidates = slices.Clone(sector)
	}
	if !slices.Contains(candidates, phase.PS) {
		candidates = append(candidates, phase.PS)
	}
	from := shiftDate(b.Prices[len(b.Prices)-1].Date, -cfg.HistoryYears)
	var lastResort *choice // the first usable metric, even if distorted
	var why string         // why the phase's own metric was not used
	for i, metric := range candidates {
		hist := history(metric, b.Prices, rolling, sp, cfg.HistoryYears)
		cur, basis, perShare, period, ok := current(metric, rolling, *price)
		if !ok {
			notes = append(notes, fmt.Sprintf("%s: not meaningful today (%s)", metric, basis))
			if i == 0 {
				why = "is not meaningful today (" + basis + ")"
			}
			continue
		}
		if len(hist) < cfg.MinHistoryDays {
			notes = append(notes, fmt.Sprintf("%s: only %d days of history (need %d)", metric, len(hist), cfg.MinHistoryDays))
			if i == 0 {
				why = "lacked enough history"
			}
			continue
		}
		c := choice{metric: metric, hist: hist, current: cur, basis: basis, perShare: perShare, period: period}
		if d := distortion(metric, rolling, from, cfg); d != "" {
			notes = append(notes, fmt.Sprintf("%s: history distorted, %s", metric, d))
			if i == 0 {
				why = "has a distorted history: " + d
			}
			if lastResort == nil {
				lastResort = &c
			}
			continue
		}
		bands, score, lastYear := c.bands(cfg, candidates[0], why, *price)
		return bands, score, lastYear, notes
	}
	if lastResort != nil {
		bands, score, lastYear := lastResort.bands(cfg, candidates[0], why, *price)
		score.Reasons = append(score.Reasons, "caution: every metric's history is distorted, so this comparison is weak")
		return bands, score, lastYear, notes
	}
	score.Reasons = append([]string{"no valuation metric had enough data"}, notes...)
	return nil, score, nil, notes
}

// recentSpinOff explains why no multiple is meaningful after a large spin-off
// inside the last twelve months, or returns "". The price is then that of
// the company without the business spun off, while the filings' twelve
// months still include it, and its one-off gain (Honeywell's $6.6B gain on
// the aerospace spin-off in June 2026). Tiingo records a spin-off as a price
// factor that is not a split ratio; a split on the same day (Honeywell's
// 1-for-2) is divided out first.
func recentSpinOff(b *ingest.Bundle, rolling []ttm) string {
	if len(rolling) == 0 {
		return ""
	}
	end := rolling[len(rolling)-1].end
	start := shiftDate(end, -1)
	for _, bar := range b.Prices {
		if bar.Date <= start || bar.SplitFactor <= 0 || bar.SplitFactor == 1 || bar.IsSplit() {
			continue
		}
		f := bar.SplitFactor
		for _, s := range b.Splits {
			if s.Date == bar.Date {
				f /= s.Factor
			}
		}
		if r := math.Max(f, 1/f); r >= 1.15 {
			return fmt.Sprintf("a spin-off on %s took about %.0f%% off the share price, and the last twelve months of filings (to %s) still include the business spun off",
				bar.Date, (1-1/r)*100, end)
		}
	}
	return ""
}

// choice is a metric with enough history to build bands from.
type choice struct {
	metric   string
	hist     []Point
	current  float64
	basis    string
	perShare float64
	period   string
}

func (c choice) bands(cfg scoring.Config, wanted, why string, price float64) (*Bands, scoring.Score, []Point) {
	values := make([]float64, len(c.hist))
	for j, p := range c.hist {
		values[j] = p.Value
	}
	sort.Float64s(values)
	bands := &Bands{
		Metric:        c.metric,
		HistoryFrom:   c.hist[0].Date,
		HistoryTo:     c.hist[len(c.hist)-1].Date,
		Days:          len(c.hist),
		Current:       round(c.current, 4),
		Cheap:         round(scoring.Percentile(values, cfg.CheapPercentile), 4),
		Median:        round(scoring.Percentile(values, 0.5), 4),
		Expensive:     round(scoring.Percentile(values, cfg.ExpensivePercentile), 4),
		Percentile:    round(scoring.Rank(values, c.current), 4),
		PerShareBasis: c.basis,
		PerShare:      round(c.perShare, 4),
		BasisPeriod:   c.period,
		Price:         price,
	}
	if c.metric != wanted {
		bands.FellBackFrom = wanted
		bands.FallbackReason = why
	}
	bands.CheapPrice = round(bands.Cheap*c.perShare, 2)
	bands.MedianPrice = round(bands.Median*c.perShare, 2)
	bands.ExpensivePrice = round(bands.Expensive*c.perShare, 2)
	switch {
	case bands.Percentile <= cfg.CheapPercentile:
		bands.Zone = "cheap"
	case bands.Percentile >= cfg.ExpensivePercentile:
		bands.Zone = "expensive"
	default:
		bands.Zone = "fair"
	}

	v := cfg.ValuationScore(bands.Percentile)
	score := scoring.Score{Max: 5, Value: &v}
	score.Reasons = []string{
		fmt.Sprintf("%s today %.1f× vs %d-year range: cheap below %.1f×, expensive above %.1f×",
			c.metric, bands.Current, cfg.HistoryYears, bands.Cheap, bands.Expensive),
		fmt.Sprintf("today is higher than %.0f%% of the last %d trading days → %.0f/5", bands.Percentile*100, bands.Days, v),
	}
	if bands.FellBackFrom != "" {
		score.Reasons = append(score.Reasons, fmt.Sprintf("used %s because %s %s", c.metric, bands.FellBackFrom, why))
	}

	var lastYear []Point
	cutoff := shiftDate(c.hist[len(c.hist)-1].Date, -1)
	for _, p := range c.hist {
		if p.Date >= cutoff {
			lastYear = append(lastYear, p)
		}
	}
	return bands, score, lastYear
}

// distortion explains why a metric's own history is a poor yardstick, or
// returns "". When the per-share basis (EPS, FCF or revenue per share) was
// ≤ 0 or sank far below its typical level during the window, the multiples
// from those days spike, and today looks cheap against them.
func distortion(metric string, rolling []ttm, from string, cfg scoring.Config) string {
	var usable []ttm
	for _, t := range rolling {
		if _, _, ok := perShareBasis(metric, t); ok {
			usable = append(usable, t)
		}
	}
	start := 0 // the window already public on the first day of the history
	for i, t := range usable {
		if t.available <= from {
			start = i
		}
	}
	var bases []float64
	name := ""
	for _, t := range usable[start:] {
		ps, n, _ := perShareBasis(metric, t)
		bases = append(bases, ps)
		name = n
	}
	if len(bases) == 0 {
		return ""
	}
	sort.Float64s(bases)
	low, typical := bases[0], scoring.Percentile(bases, 0.5)
	if low <= 0 {
		return fmt.Sprintf("%s was zero or negative during the last %d years", name, cfg.HistoryYears)
	}
	if typical/low > cfg.MaxBasisSwing {
		return fmt.Sprintf("%s fell as low as $%.2f against a typical $%.2f", name, low, typical)
	}
	return ""
}

// history computes the metric for every trading day in the window, using the
// TTM numbers that were public on that day.
func history(metric string, bars []prices.Bar, rolling []ttm, sp splits, years int) []Point {
	if len(bars) == 0 {
		return nil
	}
	// Only windows that have the numbers this metric needs; a window with
	// partial data (e.g. EPS but no revenue yet) must not hide an older one.
	var usable []ttm
	for _, t := range rolling {
		if _, _, has := perShareBasis(metric, t); has {
			usable = append(usable, t)
		}
	}

	from := shiftDate(bars[len(bars)-1].Date, -years)
	var out []Point
	j := -1 // index of the latest usable TTM public so far
	for _, bar := range bars {
		if bar.Date < from {
			continue
		}
		for j+1 < len(usable) && usable[j+1].available <= bar.Date {
			j++
		}
		if j < 0 {
			continue
		}
		if v, ok := multipleOn(metric, usable[j], sp.price(bar)); ok {
			out = append(out, Point{Date: bar.Date, Value: round(v, 4)})
		}
	}
	return out
}

// current computes today's value of the metric from the most recent TTM
// window that has the needed numbers.
func current(metric string, rolling []ttm, price float64) (value float64, basis string, perShare float64, period string, ok bool) {
	for i := len(rolling) - 1; i >= 0; i-- {
		t := rolling[i]
		ps, name, has := perShareBasis(metric, t)
		if !has {
			continue
		}
		if ps <= 0 {
			return 0, name + " is negative or zero", 0, "", false
		}
		return price / ps, name, ps, "TTM to " + t.end, true
	}
	return 0, "no TTM data", 0, "", false
}

// multipleOn computes price ÷ per-share basis for one day.
func multipleOn(metric string, t ttm, price float64) (float64, bool) {
	ps, _, ok := perShareBasis(metric, t)
	if !ok || ps <= 0 {
		return 0, false
	}
	return price / ps, true
}

// perShareBasis is what the multiple divides the price by: EPS for P/E,
// revenue per share for P/S, free cash flow per share for P/FCF, book value
// per share for P/B, funds from operations per share for P/FFO.
func perShareBasis(metric string, t ttm) (float64, string, bool) {
	switch metric {
	case phase.PE:
		eps := t.valuationEPS
		if eps == nil {
			eps = t.eps
		}
		if eps == nil {
			return 0, "EPS", false
		}
		return *eps, "EPS", true
	case phase.PS:
		if t.revenue == nil || t.shares == nil || *t.shares <= 0 {
			return 0, "revenue per share", false
		}
		return *t.revenue / *t.shares, "revenue per share", true
	case phase.PFCF:
		if t.fcf == nil || t.shares == nil || *t.shares <= 0 {
			return 0, "free cash flow per share", false
		}
		return *t.fcf / *t.shares, "free cash flow per share", true
	case phase.PB:
		if t.equity == nil || t.shares == nil || *t.shares <= 0 {
			return 0, "book value per share", false
		}
		return *t.equity / *t.shares, "book value per share", true
	case phase.PFFO:
		if t.ffo == nil || t.shares == nil || *t.shares <= 0 {
			return 0, "FFO per share", false
		}
		return *t.ffo / *t.shares, "FFO per share", true
	}
	return 0, metric, false
}

func shiftDate(date string, years int) string {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}
	return t.AddDate(years, 0, 0).Format(time.DateOnly)
}

// yearsApart reports whether two quarter-end dates are about n years apart
// (within three weeks, for 52/53-week fiscal calendars).
func yearsApart(a, b string, n int) bool {
	ta, err1 := time.Parse(time.DateOnly, a)
	tb, err2 := time.Parse(time.DateOnly, b)
	if err1 != nil || err2 != nil {
		return false
	}
	d := tb.Sub(ta).Hours() / 24
	return math.Abs(d-float64(n)*365.25) <= 21
}

// yearApart reports whether two quarter-end dates are about a year apart
// (52/53-week fiscal years make it 364 or 371 days).
func yearApart(a, b string) bool {
	ta, err1 := time.Parse(time.DateOnly, a)
	tb, err2 := time.Parse(time.DateOnly, b)
	if err1 != nil || err2 != nil {
		return false
	}
	d := tb.Sub(ta).Hours() / 24
	return d >= 350 && d <= 380
}
