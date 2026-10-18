package financials

import (
	"strings"
	"testing"
)

// These values were checked by hand against the companies' 10-K/10-Q filings
// and earnings releases. The golden files catch any change; these catch a
// golden file that was regenerated with a wrong number.
func TestHandCheckedValues(t *testing.T) {
	type want struct {
		ticker  string
		pt      PeriodType
		end     string
		fy, fq  int
		field   string
		value   float64
		derived bool // value must be derived (YTD subtraction or a formula), not tagged directly
	}
	tests := []want{
		// Apple FY2024 10-K (52 weeks ended 2024-09-28).
		{"AAPL", FiscalYear, "2024-09-28", 2024, 0, "revenue", 391_035e6, false},
		{"AAPL", FiscalYear, "2024-09-28", 2024, 0, "netIncome", 93_736e6, false},
		{"AAPL", FiscalYear, "2024-09-28", 2024, 0, "epsDiluted", 6.08, false},
		{"AAPL", FiscalYear, "2024-09-28", 2024, 0, "operatingCashFlow", 118_254e6, false},
		{"AAPL", FiscalYear, "2024-09-28", 2024, 0, "capex", 9_447e6, false},
		{"AAPL", FiscalYear, "2024-09-28", 2024, 0, "freeCashFlow", 108_807e6, true}, // OCF − capex
		{"AAPL", FiscalYear, "2025-09-27", 2025, 0, "revenue", 416_161e6, false},
		{"AAPL", FiscalYear, "2025-09-27", 2025, 0, "netIncome", 112_010e6, false},
		// Apple Q4 FY2025 is never filed on its own: FY − 9M YTD.
		{"AAPL", FiscalQuarter, "2025-09-27", 2025, 4, "revenue", 102_466e6, true},
		{"AAPL", FiscalQuarter, "2025-09-27", 2025, 4, "epsDiluted", 1.84, true},

		// Visa FY2024 10-K (fiscal year ended 2024-09-30).
		{"V", FiscalYear, "2024-09-30", 2024, 0, "revenue", 35_926e6, false},
		{"V", FiscalYear, "2024-09-30", 2024, 0, "netIncome", 19_743e6, false},
		{"V", FiscalYear, "2025-09-30", 2025, 0, "revenue", 40_000e6, false},

		// Snowflake FY2025 10-K (fiscal year ended 2025-01-31): loss-making.
		{"SNOW", FiscalYear, "2025-01-31", 2025, 0, "revenue", 3_626_396e3, false},
		// Net loss attributable to Snowflake; the total including
		// non-controlling interests is −1,289,212 (ProfitLoss, and what the
		// 2026 proxy statement tags as NetIncomeLoss).
		{"SNOW", FiscalYear, "2025-01-31", 2025, 0, "netIncome", -1_285_640e3, false},
		{"SNOW", FiscalYear, "2025-01-31", 2025, 0, "epsDiluted", -3.86, false},
		// Q4 FY2026 = FY2026 − 9M YTD = 4,683,946 − 3,399,952 (thousands).
		{"SNOW", FiscalQuarter, "2026-01-31", 2026, 4, "revenue", 1_283_994e3, true},
		// Quarter ended 2026-07-31 is Q2 of fiscal 2027 (FY ends January).
		{"SNOW", FiscalQuarter, "2026-07-31", 2027, 2, "revenue", 1_546_793e3, false},
		// TTM to 2026-07-31 = 1,212,909 + 1,283,994 + 1,390,951 + 1,546,793.
		{"SNOW", TTM, "2026-07-31", 2027, 2, "revenue", 5_434_647e3, false},
	}

	loaded := map[string]Financials{}
	for _, tc := range tests {
		f, ok := loaded[tc.ticker]
		if !ok {
			f = load(t, tc.ticker)
			loaded[tc.ticker] = f
		}
		p := find(f, tc.pt, tc.end)
		name := tc.ticker + " " + string(tc.pt) + " " + tc.end + " " + tc.field
		if p == nil {
			t.Errorf("%s: period not found", name)
			continue
		}
		if p.FiscalYear != tc.fy || p.FiscalQuarter != tc.fq {
			t.Errorf("%s: labelled FY%d Q%d, want FY%d Q%d", name, p.FiscalYear, p.FiscalQuarter, tc.fy, tc.fq)
		}
		got := p.Get(tc.field)
		if got == nil || *got != tc.value {
			t.Errorf("%s = %v, want %v", name, deref(got), tc.value)
		}
		if isDerived := strings.Contains(p.Sources[tc.field], "derived"); isDerived != tc.derived {
			t.Errorf("%s: source %q, derived=%v want %v", name, p.Sources[tc.field], isDerived, tc.derived)
		}
	}
}

func TestVisaEPSIsMissingWithExplanation(t *testing.T) {
	f := load(t, "V")
	for _, p := range f.Annual {
		if p.EPSDiluted != nil {
			t.Fatalf("Visa EPS should be nil (not in companyfacts), got %v for %s", *p.EPSDiluted, p.EndDate)
		}
	}
	found := false
	for _, w := range f.Warnings {
		if strings.HasPrefix(w, "epsDiluted:") && strings.Contains(w, "share classes") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an epsDiluted warning explaining share classes; got %v", f.Warnings)
	}
}

func TestQuartersAreConsecutive(t *testing.T) {
	for ticker := range companies {
		f := load(t, ticker)
		recent := f.Quarterly[len(f.Quarterly)-12:]
		for i := 1; i < len(recent); i++ {
			prev, cur := recent[i-1], recent[i]
			wantQ := prev.FiscalQuarter%4 + 1
			wantFY := prev.FiscalYear
			if wantQ == 1 {
				wantFY++
			}
			if cur.FiscalQuarter != wantQ || cur.FiscalYear != wantFY {
				t.Errorf("%s: after FY%d Q%d came FY%d Q%d (end %s)", ticker,
					prev.FiscalYear, prev.FiscalQuarter, cur.FiscalYear, cur.FiscalQuarter, cur.EndDate)
			}
		}
	}
}

func find(f Financials, pt PeriodType, end string) *FinancialPeriod {
	switch pt {
	case TTM:
		if f.TTM != nil && f.TTM.EndDate == end {
			return f.TTM
		}
		return nil
	case FiscalYear:
		return findByEnd(f.Annual, end)
	default:
		return findByEnd(f.Quarterly, end)
	}
}

func deref(p *float64) any {
	if p == nil {
		return "nil"
	}
	return *p
}
