package financials

import (
	"strings"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
)

// Each test here reproduces one filer quirk found by `tickerlens health`.

type factsBuilder map[string]map[string][]edgar.Fact // concept → unit → facts

func (b factsBuilder) add(concept, unit, start, end string, val float64, form, filed string) factsBuilder {
	if b[concept] == nil {
		b[concept] = map[string][]edgar.Fact{}
	}
	accn := "0000000001-" + strings.ReplaceAll(filed, "-", "")
	fp := "Q"
	if strings.HasPrefix(form, "10-K") {
		fp = "FY"
	}
	b[concept][unit] = append(b[concept][unit], edgar.Fact{
		Start: start, End: end, Val: val, Accn: accn, FY: 2025, FP: fp, Form: form, Filed: filed,
	})
	return b
}

func (b factsBuilder) normalize() Financials {
	gaap := map[string]edgar.Concept{}
	for concept, units := range b {
		gaap[concept] = edgar.Concept{Units: units}
	}
	facts := &edgar.CompanyFacts{Facts: map[string]map[string]edgar.Concept{"us-gaap": gaap}}
	return Normalize(edgar.Company{Ticker: "TEST", CIK: "0000000001"}, facts, nil)
}

// calendarYear adds a calendar fiscal year's revenue: three quarters and the
// year, so that the year and its quarters exist.
func (b factsBuilder) calendarYear(year string, q float64) factsBuilder {
	b.add("Revenues", "USD", year+"-01-01", year+"-03-31", q, "10-Q", year+"-05-01")
	b.add("Revenues", "USD", year+"-04-01", year+"-06-30", q, "10-Q", year+"-08-01")
	b.add("Revenues", "USD", year+"-07-01", year+"-09-30", q, "10-Q", year+"-11-01")
	b.add("Revenues", "USD", year+"-01-01", year+"-12-31", 4*q, "10-K", nextYear(year)+"-02-15")
	return b
}

func nextYear(y string) string {
	return map[string]string{"2023": "2024", "2024": "2025", "2025": "2026"}[y]
}

// Medtronic's proxy statement tags net income in millions without the scale,
// and is filed after the 10-K.
func TestProxyStatementFactsAreIgnored(t *testing.T) {
	f := factsBuilder{}.calendarYear("2025", 9e9).
		add("NetIncomeLoss", "USD", "2025-01-01", "2025-12-31", 4_662e6, "10-K", "2026-02-15").
		add("NetIncomeLoss", "USD", "2025-01-01", "2025-12-31", 4_662, "DEF 14A", "2026-08-17").
		normalize()
	if got := deref(find(f, FiscalYear, "2025-12-31").NetIncome); got != 4_662e6 {
		t.Errorf("FY net income = %v, want the 10-K's 4,662e6", got)
	}
}

// McDonald's tags diluted shares in millions without the scale.
func TestShareCountsFiledWithoutScale(t *testing.T) {
	f := factsBuilder{}.calendarYear("2025", 6e9).
		add("NetIncomeLoss", "USD", "2025-04-01", "2025-06-30", 2_253e6, "10-Q", "2025-08-01").
		add("EarningsPerShareDiluted", "USD/shares", "2025-04-01", "2025-06-30", 3.14, "10-Q", "2025-08-01").
		add("WeightedAverageNumberOfDilutedSharesOutstanding", "shares", "2025-04-01", "2025-06-30", 717.6, "10-Q", "2025-08-01").
		add("WeightedAverageNumberOfDilutedSharesOutstanding", "shares", "2025-07-01", "2025-09-30", 715.9, "10-Q", "2025-11-01").
		normalize()
	for _, end := range []string{"2025-06-30", "2025-09-30"} {
		q := find(f, FiscalQuarter, end)
		if q == nil || q.SharesDiluted == nil || *q.SharesDiluted < 715e6 || *q.SharesDiluted > 718e6 {
			t.Fatalf("%s shares: %+v, want ~716e6", end, q)
		}
		if src := q.Sources["sharesDiluted"]; !strings.Contains(src, "filed in millions") {
			t.Errorf("%s shares source = %q", end, src)
		}
	}
}

// Palantir repaid its loans in 2021: later periods have no debt, not unknown
// debt. A company still paying interest keeps unknown debt when its balances
// are under its own concepts (Caterpillar).
func TestDebtFreeAfterRepayment(t *testing.T) {
	b := factsBuilder{}.calendarYear("2025", 1e9).
		add("LongTermDebtNoncurrent", "USD", "", "2021-03-31", 198e6, "10-Q", "2021-05-01")
	f := b.normalize()
	if got := deref(find(f, FiscalYear, "2025-12-31").TotalDebt); got != 0.0 {
		t.Errorf("FY2025 debt = %v, want 0", got)
	}
	if src := find(f, FiscalYear, "2025-12-31").Sources["totalDebt"]; !strings.Contains(src, "since 2021-03-31") {
		t.Errorf("debt source = %q", src)
	}

	b.add("InterestExpense", "USD", "2025-01-01", "2025-12-31", 50e6, "10-K", "2026-02-15")
	if got := deref(find(b.normalize(), FiscalYear, "2025-12-31").TotalDebt); got != "nil" {
		t.Errorf("FY2025 debt with interest expense = %v, want unknown", got)
	}
}

// General Mills' "Revenues" holds one stray FY2024 figure from its 10-K; the
// revenue it re-reports in later 10-Ks is under the contract-revenue concept.
func TestRevenueConceptReReportedLaterWins(t *testing.T) {
	f := factsBuilder{}.calendarYear("2024", 5e9).
		add("Revenues", "USD", "2024-01-01", "2024-12-31", 2.04e9, "10-K", "2025-02-15").
		add("RevenueFromContractWithCustomerExcludingAssessedTax", "USD", "2024-01-01", "2024-12-31", 19.86e9, "10-K", "2025-02-15").
		add("RevenueFromContractWithCustomerExcludingAssessedTax", "USD", "2024-01-01", "2024-12-31", 19.86e9, "10-K", "2026-02-15").
		normalize()
	fy := find(f, FiscalYear, "2024-12-31")
	if got := deref(fy.Revenue); got != 19.86e9 {
		t.Errorf("FY2024 revenue = %v (%s), want 19.86e9", got, fy.Sources["revenue"])
	}
}

// Dollar Tree restated FY2023 without Family Dollar after its quarters were
// filed: Q4 = restated year − original 9 months would be negative.
func TestQuarterFromYearRestatedLater(t *testing.T) {
	f := factsBuilder{}.
		add("Revenues", "USD", "2023-01-01", "2023-03-31", 7.32e9, "10-Q", "2023-05-01").
		add("Revenues", "USD", "2023-01-01", "2023-06-30", 14.65e9, "10-Q", "2023-08-01").
		add("Revenues", "USD", "2023-01-01", "2023-09-30", 21.96e9, "10-Q", "2023-11-01").
		add("Revenues", "USD", "2023-01-01", "2023-09-30", 21.96e9, "10-Q", "2024-11-01").
		add("Revenues", "USD", "2023-01-01", "2023-12-31", 30.6e9, "10-K", "2024-02-15").
		add("Revenues", "USD", "2023-01-01", "2023-12-31", 16.78e9, "10-K", "2025-02-15").
		normalize()
	q4 := find(f, FiscalQuarter, "2023-12-31")
	if q4 == nil {
		t.Fatal("Q4 2023 not found")
	}
	if got := deref(q4.Revenue); got != 8.64e9 {
		t.Errorf("Q4 revenue = %v, want 8.64e9 (30.60 − 21.96, as originally filed)", got)
	}
	if src := q4.Sources["revenue"]; !strings.Contains(src, "before it was restated") {
		t.Errorf("Q4 revenue source = %q", src)
	}
	// The year itself is the restated figure.
	if got := deref(find(f, FiscalYear, "2023-12-31").Revenue); got != 16.78e9 {
		t.Errorf("FY revenue = %v, want the restated 16.78e9", got)
	}
}
