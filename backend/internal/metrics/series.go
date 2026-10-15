package metrics

import (
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
)

// --- Stock splits -------------------------------------------------------------
//
// Everything per-share is converted to *today's* share terms. A 10-for-1 split
// makes old prices 10× too high and old EPS 10× too high; dividing both by the
// splits that happened since puts them on the same footing as today's price.

type split struct {
	date   string
	factor float64
}

type splits []split

func splitsFrom(bars []prices.Bar) splits {
	var out splits
	for _, b := range bars {
		if b.SplitFactor != 0 && b.SplitFactor != 1 {
			out = append(out, split{date: b.Date, factor: b.SplitFactor})
		}
	}
	return out
}

// after multiplies the factors of every split dated after date.
func (s splits) after(date string) float64 {
	f := 1.0
	for _, sp := range s {
		if sp.date > date {
			f *= sp.factor
		}
	}
	return f
}

// price converts a historical close to today's share terms.
func (s splits) price(b prices.Bar) float64 { return b.Close / s.after(b.Date) }

// perShare converts a per-share value from a filing to today's share terms.
// Filings made after a split already report restated (post-split) values, so
// only splits after the filing date apply.
func (s splits) perShare(p financials.FinancialPeriod, field string) *float64 {
	v := p.Get(field)
	if v == nil {
		return nil
	}
	adj := *v / s.after(filedOrEnd(p, field))
	return &adj
}

// shareCount converts a share count from a filing to today's share terms.
func (s splits) shareCount(p financials.FinancialPeriod, field string) *float64 {
	v := p.Get(field)
	if v == nil {
		return nil
	}
	adj := *v * s.after(filedOrEnd(p, field))
	return &adj
}

func filedOrEnd(p financials.FinancialPeriod, field string) string {
	if d := p.Filed[field]; d != "" {
		return d
	}
	return p.EndDate
}

// --- Rolling TTM ---------------------------------------------------------------

// ttm is one trailing-twelve-month window, as of a quarter end.
type ttm struct {
	end       string
	available string // first day the numbers were public (approximate)
	revenue   *float64
	netIncome *float64
	fcf       *float64
	eps       *float64 // today's share terms
	shares    *float64 // today's share terms
}

// rollingTTM builds a TTM window for every run of four consecutive quarters,
// oldest first.
func rollingTTM(quarters []financials.FinancialPeriod, annual []financials.FinancialPeriod, sp splits) []ttm {
	var out []ttm
	for i := 3; i < len(quarters); i++ {
		w := quarters[i-3 : i+1]
		if !consecutive(w) {
			continue
		}
		last := w[3]
		t := ttm{
			end:       last.EndDate,
			available: availableDate(last),
			revenue:   sumField(w, func(p financials.FinancialPeriod) *float64 { return p.Revenue }),
			netIncome: sumField(w, func(p financials.FinancialPeriod) *float64 { return p.NetIncome }),
			fcf:       sumField(w, func(p financials.FinancialPeriod) *float64 { return p.FreeCashFlow }),
			eps:       sumField(w, func(p financials.FinancialPeriod) *float64 { return sp.perShare(p, "epsDiluted") }),
		}
		t.eps = round4(t.eps)
		// Diluted shares: the latest quarter's weighted average. Q4 is rarely
		// reported alone, so fall back to the fiscal year ending the same day,
		// then to the previous quarter.
		t.shares = sp.shareCount(last, "sharesDiluted")
		if t.shares == nil {
			for _, fy := range annual {
				if fy.EndDate == last.EndDate {
					t.shares = sp.shareCount(fy, "sharesDiluted")
				}
			}
		}
		if t.shares == nil {
			t.shares = sp.shareCount(w[2], "sharesDiluted")
		}
		out = append(out, t)
	}
	return out
}

// latestWith returns the most recent TTM window that satisfies ok.
func latestWith(rolling []ttm, ok func(ttm) bool) (ttm, bool) {
	for i := len(rolling) - 1; i >= 0; i-- {
		if ok(rolling[i]) {
			return rolling[i], true
		}
	}
	return ttm{}, false
}

// availableDate approximates when a quarter's results became public: SEC
// deadlines are ~40–45 days for a 10-Q and ~60–75 days for a 10-K.
func availableDate(q financials.FinancialPeriod) string {
	end, err := time.Parse(time.DateOnly, q.EndDate)
	if err != nil {
		return q.EndDate
	}
	lag := 45
	if q.FiscalQuarter == 4 {
		lag = 75
	}
	return end.AddDate(0, 0, lag).Format(time.DateOnly)
}

func consecutive(w []financials.FinancialPeriod) bool {
	for i := 1; i < len(w); i++ {
		prevEnd, err1 := time.Parse(time.DateOnly, w[i-1].EndDate)
		start, err2 := time.Parse(time.DateOnly, w[i].StartDate)
		if err1 != nil || err2 != nil {
			return false
		}
		gap := start.Sub(prevEnd.AddDate(0, 0, 1))
		if gap > 7*24*time.Hour || gap < -7*24*time.Hour {
			return false
		}
	}
	return true
}

func sumField(w []financials.FinancialPeriod, get func(financials.FinancialPeriod) *float64) *float64 {
	var total float64
	for _, p := range w {
		v := get(p)
		if v == nil {
			return nil
		}
		total += *v
	}
	return &total
}
