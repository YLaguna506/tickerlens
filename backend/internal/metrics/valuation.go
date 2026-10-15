package metrics

import (
	"fmt"
	"sort"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/phase"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

// Bands compares today's valuation with the company's own history.
type Bands struct {
	Metric       string  `json:"metric"`                 // P/E, P/S or P/FCF
	FellBackFrom string  `json:"fellBackFrom,omitempty"` // phase metric that lacked data
	HistoryFrom  string  `json:"historyFrom"`
	HistoryTo    string  `json:"historyTo"`
	Days         int     `json:"days"`
	Current      float64 `json:"current"`
	Cheap        float64 `json:"cheapBelow"`     // percentile edge (e.g. 25th)
	Expensive    float64 `json:"expensiveAbove"` // percentile edge (e.g. 75th)
	Percentile   float64 `json:"percentile"`     // share of history at or below today (0–1)
	Zone         string  `json:"zone"`           // cheap / fair / expensive

	// The price bar: band edges translated into share prices.
	PerShareBasis  string  `json:"perShareBasis"` // what the multiple is applied to
	PerShare       float64 `json:"perShare"`
	BasisPeriod    string  `json:"basisPeriod"`
	Price          float64 `json:"price"`
	CheapPrice     float64 `json:"cheapPrice"`
	ExpensivePrice float64 `json:"expensivePrice"`
}

// valuationBands tries the phase's metric, then its fallbacks, and scores the
// first one with enough history.
func valuationBands(b *ingest.Bundle, cfg scoring.Config, ph phase.Result, rolling []ttm, sp splits, price *float64) (*Bands, scoring.Score, []Point, []string) {
	score := scoring.Score{Max: 5}
	var notes []string
	if price == nil || len(b.Prices) == 0 {
		score.Reasons = []string{"no price data"}
		return nil, score, nil, notes
	}

	candidates := append([]string{ph.ValuationMetric}, ph.Fallbacks...)
	for i, metric := range candidates {
		hist := history(metric, b.Prices, rolling, sp, cfg.HistoryYears)
		cur, basis, perShare, period, ok := current(metric, rolling, *price)
		if !ok {
			notes = append(notes, fmt.Sprintf("%s: not meaningful today (%s)", metric, basis))
			continue
		}
		if len(hist) < cfg.MinHistoryDays {
			notes = append(notes, fmt.Sprintf("%s: only %d days of history (need %d)", metric, len(hist), cfg.MinHistoryDays))
			continue
		}

		values := make([]float64, len(hist))
		for j, p := range hist {
			values[j] = p.Value
		}
		sort.Float64s(values)
		bands := &Bands{
			Metric:        metric,
			HistoryFrom:   hist[0].Date,
			HistoryTo:     hist[len(hist)-1].Date,
			Days:          len(hist),
			Current:       round(cur, 4),
			Cheap:         round(scoring.Percentile(values, cfg.CheapPercentile), 4),
			Expensive:     round(scoring.Percentile(values, cfg.ExpensivePercentile), 4),
			Percentile:    round(scoring.Rank(values, cur), 4),
			PerShareBasis: basis,
			PerShare:      round(perShare, 4),
			BasisPeriod:   period,
			Price:         *price,
		}
		if i > 0 {
			bands.FellBackFrom = candidates[0]
		}
		bands.CheapPrice = round(bands.Cheap*perShare, 2)
		bands.ExpensivePrice = round(bands.Expensive*perShare, 2)
		switch {
		case bands.Percentile <= cfg.CheapPercentile:
			bands.Zone = "cheap"
		case bands.Percentile >= cfg.ExpensivePercentile:
			bands.Zone = "expensive"
		default:
			bands.Zone = "fair"
		}

		v := cfg.ValuationScore(bands.Percentile)
		score.Value = &v
		score.Reasons = []string{
			fmt.Sprintf("%s today %.1f× vs %d-year range: cheap below %.1f×, expensive above %.1f×",
				metric, bands.Current, cfg.HistoryYears, bands.Cheap, bands.Expensive),
			fmt.Sprintf("today is higher than %.0f%% of the last %d trading days → %.0f/5", bands.Percentile*100, bands.Days, v),
		}
		if bands.FellBackFrom != "" {
			score.Reasons = append(score.Reasons, fmt.Sprintf("used %s because %s lacked data", metric, bands.FellBackFrom))
		}

		var lastYear []Point
		cutoff := shiftDate(hist[len(hist)-1].Date, -1)
		for _, p := range hist {
			if p.Date >= cutoff {
				lastYear = append(lastYear, p)
			}
		}
		return bands, score, lastYear, notes
	}
	score.Reasons = append([]string{"no valuation metric had enough data"}, notes...)
	return nil, score, nil, notes
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
// revenue per share for P/S, free cash flow per share for P/FCF.
func perShareBasis(metric string, t ttm) (float64, string, bool) {
	switch metric {
	case phase.PE:
		if t.eps == nil {
			return 0, "EPS", false
		}
		return *t.eps, "EPS", true
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
