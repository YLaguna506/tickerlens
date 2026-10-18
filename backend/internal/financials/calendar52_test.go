package financials

import (
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
)

// A PepsiCo-like 52-week year (2024-12-29 to 2025-12-27) whose quarters run
// 12, 12, 12 and 16 weeks. Q4 is 112 days long and must still be derived as
// FY − 9M, or there are no four consecutive quarters and no TTM.
func pepFacts() *edgar.CompanyFacts {
	fact := func(start, end string, val float64, accn string, fy int, fp, form, filed string) edgar.Fact {
		return edgar.Fact{Start: start, End: end, Val: val, Accn: accn, FY: fy, FP: fp, Form: form, Filed: filed}
	}
	q1, q2, q3 := "0000077476-25-000001", "0000077476-25-000002", "0000077476-25-000003"
	k, q126 := "0000077476-26-000001", "0000077476-26-000002"
	revenue := []edgar.Fact{
		fact("2024-12-29", "2025-03-22", 18e9, q1, 2025, "Q1", "10-Q", "2025-04-24"),
		fact("2025-03-23", "2025-06-14", 22e9, q2, 2025, "Q2", "10-Q", "2025-07-17"),
		fact("2024-12-29", "2025-06-14", 40e9, q2, 2025, "Q2", "10-Q", "2025-07-17"),
		fact("2025-06-15", "2025-09-06", 23e9, q3, 2025, "Q3", "10-Q", "2025-10-09"),
		fact("2024-12-29", "2025-09-06", 63e9, q3, 2025, "Q3", "10-Q", "2025-10-09"),
		fact("2024-12-29", "2025-12-27", 92e9, k, 2025, "FY", "10-K", "2026-02-03"),
		fact("2025-12-28", "2026-03-21", 18.5e9, q126, 2026, "Q1", "10-Q", "2026-04-23"),
	}
	instant := func(end string, val float64, accn string, fy int, fp, form, filed string) edgar.Fact {
		return fact("", end, val, accn, fy, fp, form, filed)
	}
	usd := func(facts ...edgar.Fact) edgar.Concept {
		return edgar.Concept{Units: map[string][]edgar.Fact{"USD": facts}}
	}
	return &edgar.CompanyFacts{Facts: map[string]map[string]edgar.Concept{"us-gaap": {
		"Revenues": usd(revenue...),
		// The 10-K tags all current debt; the 10-Q does not.
		"DebtCurrent":            usd(instant("2025-12-27", 6.861e9, k, 2025, "FY", "10-K", "2026-02-03")),
		"LongTermDebtNoncurrent": usd(instant("2025-12-27", 42.321e9, k, 2025, "FY", "10-K", "2026-02-03")),
		// In the 10-Q, short-term borrowings include commercial paper and the
		// current portion of long-term debt.
		"LongTermDebtAndCapitalLeaseObligations": usd(instant("2026-03-21", 42.6e9, q126, 2026, "Q1", "10-Q", "2026-04-23")),
		"ShortTermBorrowings":                    usd(instant("2026-03-21", 9.223e9, q126, 2026, "Q1", "10-Q", "2026-04-23")),
		"CommercialPaper":                        usd(instant("2026-03-21", 4.8e9, q126, 2026, "Q1", "10-Q", "2026-04-23")),
		"LongTermDebtCurrent":                    usd(instant("2026-03-21", 1.6e9, q126, 2026, "Q1", "10-Q", "2026-04-23")),
	}}}
}

func TestSixteenWeekQ4IsDerived(t *testing.T) {
	f := Normalize(edgar.Company{Ticker: "PEP", CIK: "0000077476"}, pepFacts(), nil)

	q4 := find(f, FiscalQuarter, "2025-12-27")
	if q4 == nil {
		t.Fatal("Q4 2025 (16 weeks) not found")
	}
	if got := deref(q4.Revenue); got != 29e9 {
		t.Errorf("Q4 revenue = %v, want 29e9 (92 − 63)", got)
	}
	if f.TTM == nil || f.TTM.EndDate != "2026-03-21" {
		t.Fatalf("TTM = %+v, want one ending 2026-03-21", f.TTM)
	}
	if got := deref(f.TTM.Revenue); got != 92.5e9 {
		t.Errorf("TTM revenue = %v, want 92.5e9 (22 + 23 + 29 + 18.5)", got)
	}
}

func TestDebtWhenShortTermBorrowingsIncludeCommercialPaper(t *testing.T) {
	f := Normalize(edgar.Company{Ticker: "PEP", CIK: "0000077476"}, pepFacts(), nil)

	fy := find(f, FiscalYear, "2025-12-27")
	if fy == nil {
		t.Fatal("FY2025 not found")
	}
	if got := deref(fy.TotalDebt); got != 49.182e9 {
		t.Errorf("FY debt = %v, want 49.182e9 (DebtCurrent + LongTermDebtNoncurrent)", got)
	}
	q1 := find(f, FiscalQuarter, "2026-03-21")
	if q1 == nil {
		t.Fatal("Q1 2026 not found")
	}
	// 42.6 non-current + 9.223 short-term borrowings, which already hold the
	// 4.8 of commercial paper and the 1.6 current portion.
	if got := deref(q1.TotalDebt); got != 51.823e9 {
		t.Errorf("Q1 debt = %v, want 51.823e9; source %q", got, q1.Sources["totalDebt"])
	}
}

// Without four consecutive quarters, the latest fiscal year stands in for
// the TTM so that margins and valuation are not all empty.
func TestTTMFallsBackToLatestYear(t *testing.T) {
	facts := pepFacts()
	rev := facts.Facts["us-gaap"]["Revenues"]
	// Drop the Q3 10-Q: Q3 and Q4 can no longer be told apart.
	var kept []edgar.Fact
	for _, f := range rev.Units["USD"] {
		if f.FP != "Q3" {
			kept = append(kept, f)
		}
	}
	rev.Units["USD"] = kept
	facts.Facts["us-gaap"]["Revenues"] = rev

	f := Normalize(edgar.Company{Ticker: "PEP", CIK: "0000077476"}, facts, nil)
	if f.TTM == nil || f.TTM.EndDate != "2025-12-27" || deref(f.TTM.Revenue) != 92e9 {
		t.Fatalf("TTM = %+v, want FY2025", f.TTM)
	}
	if f.TTM.PeriodType != TTM {
		t.Errorf("TTM period type = %v", f.TTM.PeriodType)
	}
}
