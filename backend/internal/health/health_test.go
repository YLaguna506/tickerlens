package health

import (
	"strings"
	"testing"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
)

var now = time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)

func f(v float64) *float64 { return &v }

// quarter is a healthy quarter: revenue 25, net income 5, 1,000 shares,
// EPS 5 ÷ 1,000, debt 50 (in billions, shares in millions).
func quarter(fy, q int, end string) financials.FinancialPeriod {
	return financials.FinancialPeriod{
		PeriodType: financials.FiscalQuarter, FiscalYear: fy, FiscalQuarter: q, EndDate: end,
		Revenue: f(25e9), NetIncome: f(5e9), SharesDiluted: f(1e9), EPSDiluted: f(5),
		OperatingCashFlow: f(6e9), Cash: f(10e9), TotalEquity: f(20e9), TotalDebt: f(50e9),
		Sources: map[string]string{},
	}
}

func healthy() financials.Financials {
	ends := []string{"2025-03-31", "2025-06-30", "2025-09-30", "2025-12-31", "2026-03-31", "2026-06-30"}
	var fin financials.Financials
	for i, end := range ends {
		fin.Quarterly = append(fin.Quarterly, quarter(2025+i/4, i%4+1, end))
	}
	fin.Annual = []financials.FinancialPeriod{{
		PeriodType: financials.FiscalYear, FiscalYear: 2025, StartDate: "2025-01-01", EndDate: "2025-12-31",
		Revenue: f(100e9), NetIncome: f(20e9), OperatingCashFlow: f(24e9), Sources: map[string]string{},
	}}
	ttm := quarter(2026, 2, "2026-06-30")
	ttm.PeriodType = financials.TTM
	ttm.Revenue, ttm.NetIncome, ttm.EPSDiluted = f(100e9), f(20e9), f(20)
	fin.TTM = &ttm
	return fin
}

func checks(fin financials.Financials) map[string]string {
	out := map[string]string{}
	for _, is := range Check(&ingest.Bundle{Financials: fin}, nil, false, now) {
		out[is.Check] += is.Detail + "; "
	}
	return out
}

func TestHealthyDataPasses(t *testing.T) {
	if got := checks(healthy()); len(got) != 0 {
		t.Errorf("issues on healthy data: %v", got)
	}
}

func TestChecksFindKnownProblems(t *testing.T) {
	tests := []struct {
		name   string
		break_ func(*financials.Financials)
		check  string
	}{
		{"missing quarter (PepsiCo's 16-week Q4)", func(fin *financials.Financials) {
			fin.Quarterly = append(fin.Quarterly[:3:3], fin.Quarterly[4:]...)
		}, "quarter-gap"},
		{"no TTM", func(fin *financials.Financials) { fin.TTM = nil }, "no-ttm"},
		{"quarters do not add up to the year", func(fin *financials.Financials) {
			fin.Quarterly[3].Revenue = f(-5e9)
		}, "year-sum"},
		{"negative revenue (Dollar Tree)", func(fin *financials.Financials) {
			fin.Quarterly[3].Revenue = f(-5e9)
		}, "negative"},
		{"debt for one quarter (PepsiCo's 10-Qs)", func(fin *financials.Financials) {
			fin.Quarterly[2].TotalDebt = f(15e9)
		}, "spike"},
		{"shares filed in millions (McDonald's)", func(fin *financials.Financials) {
			fin.Quarterly[2].SharesDiluted = f(1000)
		}, "share-units"},
		{"EPS against net income ÷ shares (Medtronic)", func(fin *financials.Financials) {
			fin.TTM.NetIncome = f(0.4e9)
		}, "eps"},
		{"stale", func(fin *financials.Financials) {
			fin.Quarterly = fin.Quarterly[:2]
		}, "stale"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fin := healthy()
			tc.break_(&fin)
			got := checks(fin)
			if _, ok := got[tc.check]; !ok {
				t.Errorf("want a %q issue, got %v", tc.check, got)
			}
		})
	}
}

// A year whose quarters were derived from it as originally filed adds up to
// that, not to the restated figure.
func TestRestatedYearIsNotFlagged(t *testing.T) {
	fin := healthy()
	fin.Annual[0].Revenue = f(60e9)
	fin.Quarterly[3].Sources["revenue"] = "Revenues (derived: …, using the year as filed on 2024-02-15, before it was restated)"
	if got := checks(fin); strings.Contains(got["year-sum"], "revenue") {
		t.Errorf("restated year flagged: %v", got)
	}
}

// Honeywell's Q2 2025 share count, restated after its 1-for-2 reverse split.
func TestShareJumpBySplitIsNotFlagged(t *testing.T) {
	fin := healthy()
	fin.Quarterly[2].SharesDiluted = f(0.5e9)
	b := &ingest.Bundle{Financials: fin, Splits: []financials.Split{{Date: "2026-06-29", Factor: 0.5}}}
	for _, is := range Check(b, nil, false, now) {
		if is.Check == "spike" {
			t.Errorf("flagged: %s", is.Detail)
		}
	}
}
