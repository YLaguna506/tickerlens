package metrics

import (
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
)

func TestNextEarnings(t *testing.T) {
	history := []estimates.QuarterlyEarnings{
		{FiscalDateEnding: "2025-09-30", ReportedDate: "2025-10-28", ReportTime: "post-market"},
		{FiscalDateEnding: "2025-12-31", ReportedDate: "2026-02-03"},
		{FiscalDateEnding: "2026-03-31", ReportedDate: "2026-04-28"},
		{FiscalDateEnding: "2026-06-30", ReportedDate: "2026-07-28"},
	}
	// From the calendar when listed.
	est := 2.17
	cal := &estimates.CalendarEntry{ReportDate: "2026-10-22", FiscalDateEnding: "2026-09-30", EstimateEPS: &est, Time: "pre-market"}
	if n := nextEarnings(cal, history, "2026-10-16"); n == nil || n.Date != "2026-10-22" || n.Source != "calendar" || *n.EstimateEPS != 2.17 {
		t.Errorf("calendar: %+v", n)
	}
	// Otherwise a year after last year's report of the same quarter
	// (2025-10-28 → 2026-10-28, a Wednesday).
	if n := nextEarnings(nil, history, "2026-10-16"); n == nil || n.Date != "2026-10-28" || n.Source != "estimated" || n.FiscalDateEnding != "2026-09-30" {
		t.Errorf("estimated: %+v", n)
	}
	// A weekend anniversary moves to Monday: 2025-11-01 + 1 year is a Sunday.
	history[0].ReportedDate = "2025-11-01"
	if n := nextEarnings(nil, history, "2026-10-16"); n == nil || n.Date != "2026-11-02" {
		t.Errorf("weekend: %+v", n)
	}
	// Already past: unknown.
	if n := nextEarnings(nil, history, "2026-12-01"); n != nil {
		t.Errorf("past date: %+v", n)
	}
}
