package financials

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
)

// The SPLIT fixture is a calendar-year company with a 10-for-1 split on
// 2023-12-15, between its Q3 10-Q (filed 2023-11-01, EPS in pre-split shares:
// 9M YTD 3.50) and its 10-K (filed 2024-02-20, restated: FY 0.50). Q4 EPS is
// FY − 9M, which is only right once both are in the same share terms:
// 0.50 − 3.50 ÷ 10 = 0.15, not 0.50 − 3.50 = −3.00.
var fixtureSplits = []Split{{Date: "2023-12-15", Factor: 10}}

func loadSplitFixture(t *testing.T, splits []Split) Financials {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "edgar", "companyfacts_SPLIT.json"))
	if err != nil {
		t.Fatal(err)
	}
	var facts edgar.CompanyFacts
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	return Normalize(edgar.Company{Ticker: "SPLIT", CIK: "0009999999"}, &facts, splits)
}

func TestDerivedQuarterAcrossSplit(t *testing.T) {
	f := loadSplitFixture(t, fixtureSplits)

	q4 := find(f, FiscalQuarter, "2023-12-31")
	if q4 == nil {
		t.Fatal("Q4 2023 not found")
	}
	if got := deref(q4.EPSDiluted); got != 0.15 {
		t.Errorf("Q4 EPS = %v, want 0.15 (0.50 − 3.50 ÷ 10)", got)
	}
	// The derived value is in the 10-K's (post-split) share terms.
	if got := q4.Filed["epsDiluted"]; got != "2024-02-20" {
		t.Errorf("Q4 EPS filed = %q, want the 10-K's 2024-02-20", got)
	}
	if src := q4.Sources["epsDiluted"]; !strings.Contains(src, "restated for a 10-for-1 split") {
		t.Errorf("Q4 EPS source = %q, want it to mention the split", src)
	}
	// Dollar amounts are not affected by splits.
	if got := deref(q4.NetIncome); got != 150e6 {
		t.Errorf("Q4 net income = %v, want 150e6", got)
	}
}

func TestTTMAcrossSplit(t *testing.T) {
	f := loadSplitFixture(t, fixtureSplits)
	if f.TTM == nil || f.TTM.EndDate != "2024-03-31" {
		t.Fatalf("TTM = %+v, want one ending 2024-03-31", f.TTM)
	}
	// Q2 and Q3 2023 were filed before the split (1.20, 1.30); Q4 2023 and
	// Q1 2024 after (0.15, 0.11). In post-split terms: 0.12 + 0.13 + 0.15 + 0.11.
	if got := deref(f.TTM.EPSDiluted); got != 0.51 {
		t.Errorf("TTM EPS = %v, want 0.51", got)
	}
	if got := f.TTM.Filed["epsDiluted"]; got != "2024-05-01" {
		t.Errorf("TTM EPS filed = %q, want the latest quarter's 2024-05-01", got)
	}
	if got := f.TTM.Filed["sharesDiluted"]; got != "2024-05-01" {
		t.Errorf("TTM shares filed = %q, want 2024-05-01", got)
	}
}

// Without known splits (e.g. prices failed), values are combined as filed,
// which is the previous behaviour.
func TestNoSplitsKnown(t *testing.T) {
	f := loadSplitFixture(t, nil)
	q4 := find(f, FiscalQuarter, "2023-12-31")
	if q4 == nil {
		t.Fatal("Q4 2023 not found")
	}
	if deref(q4.EPSDiluted) != -3.0 {
		t.Errorf("Q4 EPS without splits = %v, want −3.00 (0.50 − 3.50)", deref(q4.EPSDiluted))
	}
}

func TestShareTermsFactor(t *testing.T) {
	terms := shareTerms{{Date: "2021-07-20", Factor: 4}, {Date: "2024-06-10", Factor: 10}}
	tests := []struct {
		from, to string
		want     float64
	}{
		{"2021-05-26", "2021-05-27", 1},
		{"2021-05-26", "2021-07-20", 4}, // filed on the split date: restated
		{"2021-05-26", "2025-02-26", 40},
		{"2023-11-21", "2025-02-26", 10},
		{"2025-02-26", "2023-11-21", 0.1},
		{"2024-06-10", "2024-06-11", 1},
	}
	for _, tc := range tests {
		if got := terms.factor(tc.from, tc.to); got != tc.want {
			t.Errorf("factor(%s, %s) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}
