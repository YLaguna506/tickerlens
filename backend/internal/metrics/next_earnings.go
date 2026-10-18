package metrics

import (
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
)

// NextEarnings is the company's next quarterly report.
type NextEarnings struct {
	Date             string   `json:"date"`
	FiscalDateEnding string   `json:"fiscalDateEnding,omitempty"`
	EstimateEPS      *float64 `json:"estimateEps"`
	Time             string   `json:"time,omitempty"` // pre-market / post-market
	// Source: "calendar" (Alpha Vantage's earnings calendar) or "estimated"
	// (a year after the same quarter's report last year).
	Source string `json:"source"`
}

// nextEarnings takes the calendar's date when the company is in it (the
// next three months); otherwise it estimates one from last year's report of
// the same quarter, since companies keep a steady schedule. asOf is the
// report's date: a date before it is not "next".
func nextEarnings(cal *estimates.CalendarEntry, history []estimates.QuarterlyEarnings, asOf string) *NextEarnings {
	if cal != nil && cal.ReportDate >= asOf {
		return &NextEarnings{Date: cal.ReportDate, FiscalDateEnding: cal.FiscalDateEnding, EstimateEPS: cal.EstimateEPS,
			Time: cal.Time, Source: "calendar"}
	}
	if len(history) == 0 {
		return nil
	}
	last := history[len(history)-1] // oldest first
	end, err := time.Parse(time.DateOnly, last.FiscalDateEnding)
	if err != nil {
		return nil
	}
	// The next quarter ends three months after the last one; a year earlier,
	// the same quarter was reported on some date.
	nextEnd := end.AddDate(0, 3, 0)
	yearAgo := nextEnd.AddDate(-1, 0, 0)
	for _, q := range history {
		qEnd, err1 := time.Parse(time.DateOnly, q.FiscalDateEnding)
		reported, err2 := time.Parse(time.DateOnly, q.ReportedDate)
		if err1 != nil || err2 != nil || absDays(qEnd.Sub(yearAgo)) > 20 {
			continue
		}
		date := weekday(reported.AddDate(1, 0, 0))
		if date.Format(time.DateOnly) < asOf {
			return nil
		}
		return &NextEarnings{Date: date.Format(time.DateOnly), FiscalDateEnding: nextEnd.Format(time.DateOnly),
			Time: q.ReportTime, Source: "estimated"}
	}
	return nil
}

func absDays(d time.Duration) float64 {
	days := d.Hours() / 24
	if days < 0 {
		return -days
	}
	return days
}

// weekday moves a weekend date to the Monday after.
func weekday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, 2)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	}
	return t
}
