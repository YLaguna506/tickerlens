// Package backtest runs the verdict as of past dates, on only the data
// available then, and measures how the stocks did over the next one to three
// years against a benchmark. It answers whether the scores pick good stocks
// or just look convincing, and which inputs carry the signal.
//
// What it can and cannot test:
//
//   - Filings are as filed: each date sees only the SEC facts filed on or
//     before it, so later restatements and later quarters are left out.
//     Prices are cut at the date too. The scoring code is the same code a
//     report runs today (with today's thresholds, which is the point).
//   - The AI's inputs (business quality, moat, risk) cannot be rebuilt for
//     past dates: the model knows what happened since. So this tests the
//     verdict without them, from growth, financial health (or strength) and
//     the valuation score V, and a Buy needs no quality score here.
//   - Insider trades and earnings surprises are left out as well.
//   - The universe is today's: today's S&P 500 members tested back in time
//     favor companies that did well enough to stay or join (survivorship
//     bias). Comparing verdicts within the same list still shows whether
//     the scores separate better stocks from worse ones.
package backtest

import (
	"strconv"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/metrics"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
	"github.com/YLaguna506/tickerlens/backend/internal/verdict"
)

// Horizons are the forward return periods, in years.
var Horizons = [3]int{1, 2, 3}

// FirstDate is the earliest as-of date: valuation bands need at least a year
// of price history, and prices are fetched from 2014.
const FirstDate = "2016-06-30"

// HistoryStart is where price history starts.
var HistoryStart = time.Date(2014, 1, 1, 0, 0, 0, 0, time.UTC)

// Dates are the as-of dates: every June 30 and December 31 from FirstDate
// until a year before lastPrice, so every date has at least a one-year
// return.
func Dates(lastPrice string) []string {
	last, err := time.Parse(time.DateOnly, lastPrice)
	if err != nil {
		return nil
	}
	cutoff := last.AddDate(-1, 0, 0).Format(time.DateOnly)
	var out []string
	for y := 2016; ; y++ {
		for _, md := range []string{"-06-30", "-12-31"} {
			d := strconv.Itoa(y) + md
			if d < FirstDate {
				continue
			}
			if d > cutoff {
				return out
			}
			out = append(out, d)
		}
	}
}

// AsOf returns the facts filed on or before date: what the market knew.
func AsOf(cf *edgar.CompanyFacts, date string) *edgar.CompanyFacts {
	out := &edgar.CompanyFacts{CIK: cf.CIK, EntityName: cf.EntityName, Facts: map[string]map[string]edgar.Concept{}}
	for taxonomy, concepts := range cf.Facts {
		kept := map[string]edgar.Concept{}
		for name, c := range concepts {
			units := map[string][]edgar.Fact{}
			for unit, facts := range c.Units {
				var keep []edgar.Fact
				for _, f := range facts {
					if f.Filed != "" && f.Filed <= date {
						keep = append(keep, f)
					}
				}
				if len(keep) > 0 {
					units[unit] = keep
				}
			}
			if len(units) > 0 {
				kept[name] = edgar.Concept{Label: c.Label, Units: units}
			}
		}
		out.Facts[taxonomy] = kept
	}
	return out
}

// upTo returns the bars on or before date.
func upTo(bars []prices.Bar, date string) []prices.Bar {
	n := 0
	for n < len(bars) && bars[n].Date <= date {
		n++
	}
	return bars[:n]
}

// Company is what one company's backtest needs.
type Company struct {
	Company edgar.Company
	Sector  string
	Profile *edgar.Profile
	Facts   *edgar.CompanyFacts
	Bars    []prices.Bar
}

// Evaluate computes the verdict as of each date, and the returns after it.
// Dates when the company was not yet trading or filing are skipped.
func Evaluate(c Company, bench []prices.Bar, dates []string, cfg scoring.Config) []store.Observation {
	noAI := cfg
	noAI.Verdict.BuyNeedsQuality = false
	var out []store.Observation
	for _, d := range dates {
		bars := upTo(c.Bars, d)
		if len(bars) == 0 || daysBetween(bars[len(bars)-1].Date, d) > 10 {
			continue // not trading on that date
		}
		facts := AsOf(c.Facts, d)
		if len(facts.Facts["us-gaap"]) == 0 {
			continue // not filing yet
		}
		b := &ingest.Bundle{Company: c.Company, Industry: c.Profile, Prices: bars}
		b.Splits = ingest.SplitsFor(bars, facts)
		b.Financials = financials.Normalize(c.Company, facts, b.Splits)
		rep := metrics.Compute(b, cfg)

		in := verdict.Input{Growth: rep.GrowthScore.Value, FinancialHealth: rep.HealthScore.Value, Valuation: rep.ValuationScore.Value}
		if rep.LimitedSupport != nil {
			in.LimitedSupport = rep.LimitedSupport.Kind
		}
		v := verdict.Decide(in, noAI)
		if v.Label == "" || rep.ValuationScore.Value == nil {
			continue // no verdict, or no valuation to judge the price by
		}
		o := store.Observation{
			Ticker: c.Company.Ticker, Date: d, Sector: c.Sector,
			Label: v.Label, Business: v.Business, Growth: rep.GrowthScore.Value, Health: rep.HealthScore.Value,
			Valuation: rep.ValuationScore.Value, Price: bars[len(bars)-1].Close,
		}
		if rep.Sector != nil && rep.LimitedSupport == nil {
			o.Kind = rep.Sector.Kind
		}
		if bands := rep.Bands; bands != nil {
			p := bands.Percentile
			o.Percentile, o.Zone, o.Metric = &p, bands.Zone, bands.Metric
		}
		if rep.ReverseDCF != nil {
			o.Expectations = rep.ReverseDCF.Expectations
		}
		for i, years := range Horizons {
			r, ok := forward(c.Bars, d, years)
			if !ok {
				continue
			}
			o.Return[i] = &r
			if br, ok := forward(bench, d, years); ok {
				x := r - br
				o.Excess[i] = &x
			}
		}
		out = append(out, o)
	}
	return out
}

// forward is the total return (adjusted for splits and dividends) from the
// last close on or before date to the first close on or after date + years.
func forward(bars []prices.Bar, date string, years int) (float64, bool) {
	start := upTo(bars, date)
	if len(start) == 0 {
		return 0, false
	}
	from := start[len(start)-1]
	t, _ := time.Parse(time.DateOnly, date)
	target := t.AddDate(years, 0, 0).Format(time.DateOnly)
	for _, b := range bars[len(start):] {
		if b.Date >= target {
			if from.AdjClose <= 0 || daysBetween(target, b.Date) > 10 {
				return 0, false // delisted, or a gap in the data
			}
			return b.AdjClose/from.AdjClose - 1, true
		}
	}
	return 0, false
}

func daysBetween(a, b string) int {
	ta, err1 := time.Parse(time.DateOnly, a)
	tb, err2 := time.Parse(time.DateOnly, b)
	if err1 != nil || err2 != nil {
		return 1 << 30
	}
	d := tb.Sub(ta).Hours() / 24
	if d < 0 {
		d = -d
	}
	return int(d)
}
