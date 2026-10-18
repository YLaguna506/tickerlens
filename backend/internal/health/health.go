// Package health checks a ticker's normalized data for signs that something
// was read wrongly from its filings: missing or overlapping quarters,
// quarters that do not add up to the year, balances that jump for a single
// quarter, EPS that disagrees with net income ÷ shares, and so on.
//
// Each company tags its filings a little differently, so these checks are run
// over many tickers at once (`tickerlens health`) to find the next odd case
// before a report shows it.
package health

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/metrics"
)

// Severity says how sure a check is that the data is wrong.
type Severity string

const (
	// Error: almost certainly a data problem.
	Error Severity = "error"
	// Warn: often a data problem, sometimes a real event (an acquisition).
	Warn Severity = "warn"
)

// Issue is one failed check.
type Issue struct {
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	Detail   string   `json:"detail"`
}

// Recent periods are the ones a report uses; older history is not checked.
const (
	recentQuarters = 12
	recentYears    = 3
)

// Check runs every check. r may be nil (financials only); prices says whether
// prices were fetched, so their absence is not flagged otherwise.
func Check(b *ingest.Bundle, r *metrics.Report, prices bool, now time.Time) []Issue {
	var c checker
	f := b.Financials
	c.ttm(f)
	c.calendar(f, now)
	c.yearSums(f)
	c.signs(f)
	c.spikes(f, b.Splits)
	c.eps(f)
	c.margins(f)
	if r != nil {
		c.report(f, r, prices, now)
	}
	return c.issues
}

type checker struct{ issues []Issue }

func (c *checker) add(check string, sev Severity, format string, args ...any) {
	c.issues = append(c.issues, Issue{Check: check, Severity: sev, Detail: fmt.Sprintf(format, args...)})
}

func (c *checker) ttm(f financials.Financials) {
	if f.TTM == nil {
		c.add("no-ttm", Error, "no trailing twelve months: %s", strings.Join(f.Warnings, "; "))
		return
	}
	for _, w := range f.Warnings {
		if strings.HasPrefix(w, "TTM unavailable") {
			c.add("ttm-fallback", Error, "%s", w)
		}
	}
	core := []string{"revenue", "netIncome", "epsDiluted", "sharesDiluted", "operatingCashFlow", "cash", "totalEquity"}
	var missing []string
	for _, name := range core {
		if f.TTM.Get(name) == nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		c.add("ttm-missing", Warn, "TTM has no %s", strings.Join(missing, ", "))
	}
}

// calendar: quarters must follow each other with no gaps or overlaps, and the
// latest one should be recent.
func (c *checker) calendar(f financials.Financials, now time.Time) {
	qs := last(f.Quarterly, recentQuarters)
	if len(qs) == 0 {
		c.add("no-quarters", Error, "no fiscal quarters")
		return
	}
	for i := 1; i < len(qs); i++ {
		gap := daysBetween(qs[i-1].EndDate, qs[i].EndDate)
		switch {
		case gap > 130:
			c.add("quarter-gap", Error, "%d days between quarters ending %s and %s", gap, qs[i-1].EndDate, qs[i].EndDate)
		case gap < 70:
			c.add("quarter-overlap", Error, "quarters ending %s and %s are only %d days apart", qs[i-1].EndDate, qs[i].EndDate, gap)
		}
	}
	latest := qs[len(qs)-1].EndDate
	if t, err := time.Parse(time.DateOnly, latest); err == nil && now.Sub(t) > 200*24*time.Hour {
		c.add("stale", Warn, "latest quarter ended %s", latest)
	}
}

// yearSums: a fiscal year's four quarters must add up to it.
func (c *checker) yearSums(f financials.Financials) {
	for _, fy := range last(f.Annual, recentYears) {
		var in []financials.FinancialPeriod
		for _, q := range f.Quarterly {
			if q.EndDate > fy.StartDate && q.EndDate <= fy.EndDate {
				in = append(in, q)
			}
		}
		if len(in) == 0 {
			continue
		}
		if len(in) != 4 {
			c.add("year-quarters", Error, "FY%d (%s to %s) has %d quarters", fy.FiscalYear, fy.StartDate, fy.EndDate, len(in))
			continue
		}
		for _, name := range []string{"revenue", "netIncome", "operatingCashFlow"} {
			want := fy.Get(name)
			if want == nil {
				continue
			}
			var sum, size float64
			complete, restated := true, false
			for _, q := range in {
				v := q.Get(name)
				if v == nil {
					complete = false
					break
				}
				sum += *v
				size += math.Abs(*v)
				restated = restated || strings.Contains(q.Sources[name], "before it was restated")
			}
			// Quarters derived from the year as originally reported add up
			// to that, not to the restated year.
			tolerance := max(0.01*math.Abs(*want), 0.005*size, 1e6)
			if complete && !restated && math.Abs(sum-*want) > tolerance {
				c.add("year-sum", Error, "FY%d %s: quarters add up to %s, the year is %s",
					fy.FiscalYear, name, money(sum), money(*want))
			}
		}
	}
}

// signs: values that cannot be negative.
func (c *checker) signs(f financials.Financials) {
	periods := append(last(f.Annual, recentYears), last(f.Quarterly, recentQuarters)...)
	for _, p := range periods {
		for _, name := range []string{"revenue", "sharesDiluted", "totalDebt", "cash", "capex"} {
			if v := p.Get(name); v != nil && *v < 0 {
				c.add("negative", Error, "%s %s is %s (%s)", label(p), name, money(*v), p.Sources[name])
			}
		}
		if v := p.SharesDiluted; v != nil && *v > 0 && *v < 1e6 {
			c.add("share-units", Error, "%s diluted shares %.0f (thousands or millions?)", label(p), *v)
		}
	}
}

// spikes: a share count that jumps for one quarter while its neighbours
// agree is a tagging or share-terms error; for debt it is usually a tagging
// change in that one filing (PepsiCo), sometimes a repayment and refinancing
// (Adobe in 2024). Cash and equity swing for real too often to check.
//
// A share count restated in a later filing's terms after a split (Honeywell's
// Q2 2025, restated after its 2026 reverse split) is expected: a jump by a
// known split ratio is skipped.
func (c *checker) spikes(f financials.Financials, splits []financials.Split) {
	qs := last(f.Quarterly, recentQuarters)
	for _, name := range []string{"totalDebt", "sharesDiluted"} {
		sev := Error
		if name == "totalDebt" {
			sev = Warn
		}
		for i := 1; i+1 < len(qs); i++ {
			a, b, d := qs[i-1].Get(name), qs[i].Get(name), qs[i+1].Get(name)
			if a == nil || b == nil || d == nil || *a <= 0 || *b <= 0 || *d <= 0 {
				continue
			}
			mid := (*a + *d) / 2
			if name == "sharesDiluted" && bySplit(*b/mid, splits) {
				continue
			}
			if math.Abs(*a-*d)/math.Max(*a, *d) < 0.2 && math.Abs(*b-mid)/mid > 0.4 {
				c.add("spike", sev, "%s %s is %s between %s and %s (%s)",
					label(qs[i]), name, money(*b), money(*a), money(*d), qs[i].Sources[name])
			}
		}
	}
}

// eps: diluted EPS should be close to net income ÷ diluted shares. Wrong share
// terms (splits, share classes) or a mismatched share count show up here.
func (c *checker) eps(f financials.Financials) {
	periods := last(f.Quarterly, 8)
	if f.TTM != nil {
		periods = append(periods, *f.TTM)
	}
	for _, p := range periods {
		ni, sh, eps := p.NetIncome, p.SharesDiluted, p.EPSDiluted
		if ni == nil || sh == nil || eps == nil || *sh <= 0 || math.Abs(*ni) < 1e6 {
			continue
		}
		implied := *ni / *sh
		if diff := math.Abs(*eps - implied); diff > 0.05 && diff/math.Abs(implied) > 0.2 {
			c.add("eps", Warn, "%s EPS %.2f, but net income ÷ shares is %.2f (%s)",
				label(p), *eps, implied, p.Sources["epsDiluted"])
		}
	}
}

func (c *checker) margins(f financials.Financials) {
	p := f.TTM
	if p == nil || p.Revenue == nil || *p.Revenue <= 0 {
		return
	}
	rev := *p.Revenue
	if gp := p.GrossProfit; gp != nil && (*gp > rev*1.001 || *gp < -rev) {
		c.add("margin", Error, "TTM gross profit %s on revenue %s", money(*gp), money(rev))
	}
	if ni := p.NetIncome; ni != nil && math.Abs(*ni) > 1.5*rev {
		c.add("margin", Warn, "TTM net income %s on revenue %s", money(*ni), money(rev))
	}
}

// report: checks on the computed metrics.
func (c *checker) report(f financials.Financials, r *metrics.Report, prices bool, now time.Time) {
	if f.TTM != nil {
		rev, ni := f.TTM.Revenue, f.TTM.NetIncome
		if r.Phase.Number == 1 && rev != nil && *rev > 5e9 && ni != nil && *ni > 0 {
			c.add("phase", Error, "phase %s for a profitable company with %s of revenue", r.Phase.Name, money(*rev))
		}
	}
	if r.GrowthScore.Value == nil {
		c.add("score", Warn, "no growth score: %s", strings.Join(r.GrowthScore.Reasons, "; "))
	}
	if r.HealthScore.Value == nil && r.LimitedSupport == nil {
		c.add("score", Warn, "no health score: %s", strings.Join(r.HealthScore.Reasons, "; "))
	}
	if !prices {
		return
	}
	if r.Price == nil {
		c.add("price", Error, "no price")
		return
	}
	if t, err := time.Parse(time.DateOnly, r.PriceDate); err == nil && now.Sub(t) > 7*24*time.Hour {
		c.add("price", Warn, "latest price is from %s", r.PriceDate)
	}
	if r.MarketCap.Value == nil {
		c.add("market-cap", Error, "no market cap: %s", r.MarketCap.Note)
	}
	if r.ValuationScore.Value == nil {
		c.add("score", Warn, "no valuation score: %s", strings.Join(r.ValuationScore.Reasons, "; "))
	}
	if bd := r.Bands; bd != nil && bd.Median > 0 && bd.Current > 0 {
		if ratio := bd.Current / bd.Median; ratio > 3 || ratio < 1.0/3 {
			c.add("valuation", Warn, "%s is %.1f against a 5-year median of %.1f", bd.Metric, bd.Current, bd.Median)
		}
	}
	for _, m := range r.Valuation {
		if m.Value == nil {
			continue
		}
		if m.Key == "ps" && *m.Value > 100 {
			c.add("valuation", Warn, "P/S is %.0f (share count or revenue units?)", *m.Value)
		}
		if m.Key == "pe" && (*m.Value > 500 || (*m.Value > 0 && *m.Value < 2)) {
			c.add("valuation", Warn, "P/E is %.1f", *m.Value)
		}
	}
}

// bySplit reports whether ratio matches a split's factor or its inverse.
func bySplit(ratio float64, splits []financials.Split) bool {
	for _, s := range splits {
		for _, f := range []float64{s.Factor, 1 / s.Factor} {
			if math.Abs(ratio/f-1) < 0.05 {
				return true
			}
		}
	}
	return false
}

func last(ps []financials.FinancialPeriod, n int) []financials.FinancialPeriod {
	if len(ps) > n {
		return ps[len(ps)-n:]
	}
	return ps
}

func label(p financials.FinancialPeriod) string {
	switch p.PeriodType {
	case financials.FiscalQuarter:
		return fmt.Sprintf("Q%d %d (%s)", p.FiscalQuarter, p.FiscalYear, p.EndDate)
	case financials.TTM:
		return "TTM to " + p.EndDate
	default:
		return fmt.Sprintf("FY%d", p.FiscalYear)
	}
}

func daysBetween(a, b string) int {
	ta, err1 := time.Parse(time.DateOnly, a)
	tb, err2 := time.Parse(time.DateOnly, b)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(tb.Sub(ta).Hours() / 24)
}

// money formats an amount in billions or millions; share counts read the same.
func money(v float64) string {
	switch a := math.Abs(v); {
	case a >= 1e9:
		return fmt.Sprintf("%.2fB", v/1e9)
	case a >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}
