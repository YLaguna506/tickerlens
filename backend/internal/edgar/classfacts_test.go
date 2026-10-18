package edgar

import (
	"os"
	"path/filepath"
	"testing"
)

var concepts = []string{"EarningsPerShareDiluted", "WeightedAverageNumberOfDilutedSharesOutstanding"}

func TestParseInstance(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "testdata", "edgar", "instance_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	facts, err := ParseInstance(f, concepts)
	if err != nil {
		t.Fatal(err)
	}

	// 3 EPS (A, B1, two-dimension) + 2 shares; the duplicate, the nil value
	// and NetIncomeLoss are excluded.
	if len(facts) != 5 {
		t.Fatalf("got %d facts, want 5: %+v", len(facts), facts)
	}
	a := facts[0]
	if a.Concept != "EarningsPerShareDiluted" || a.Val != 10.20 || a.Start != "2024-10-01" ||
		a.End != "2025-09-30" || a.Dims["StatementClassOfStockAxis"] != "CommonClassAMember" {
		t.Errorf("first fact = %+v", a)
	}
}

func dim(concept, member string, v float64) DimFact {
	return DimFact{Concept: concept, Start: "2024-10-01", End: "2025-09-30", Val: v,
		Dims: map[string]string{classAxis: member}}
}

func TestSelectClass(t *testing.T) {
	filing := Filing{Accession: "0001-25-1", Form: "10-K", FilingDate: "2025-11-06"}
	tests := []struct {
		name       string
		facts      []DimFact
		wantEPS    float64
		wantShares float64
		wantNote   string
		wantNone   bool
	}{
		{
			name: "EPS differs by class: use Class A (Visa)",
			facts: []DimFact{
				dim("EarningsPerShareDiluted", "CommonClassAMember", 10.20),
				dim("EarningsPerShareDiluted", "CommonClassB1Member", 15.95),
				dim("WeightedAverageNumberOfDilutedSharesOutstanding", "CommonClassAMember", 1966e6),
				dim("WeightedAverageNumberOfDilutedSharesOutstanding", "CommonClassB1Member", 5e6),
			},
			wantEPS: 10.20, wantShares: 1966e6, wantNote: "Class A",
		},
		{
			name: "same EPS for every class: sum the shares (Alphabet)",
			facts: []DimFact{
				dim("EarningsPerShareDiluted", "CommonClassAMember", 8.04),
				dim("EarningsPerShareDiluted", "CommonClassBMember", 8.04),
				dim("EarningsPerShareDiluted", "CapitalClassCMember", 8.04),
				dim("WeightedAverageNumberOfDilutedSharesOutstanding", "CommonClassAMember", 5.8e9),
				dim("WeightedAverageNumberOfDilutedSharesOutstanding", "CommonClassBMember", 0.9e9),
				dim("WeightedAverageNumberOfDilutedSharesOutstanding", "CapitalClassCMember", 5.6e9),
			},
			wantEPS: 8.04, wantShares: 12.3e9, wantNote: "all classes (same EPS)",
		},
		{
			name: "EPS differs: use the common stock beside a Class B (Hershey)",
			facts: []DimFact{
				dim("EarningsPerShareDiluted", "CommonStockMember", 2.26),
				dim("EarningsPerShareDiluted", "CommonClassBMember", 2.11),
				dim("WeightedAverageNumberOfDilutedSharesOutstanding", "CommonStockMember", 203e6),
				dim("WeightedAverageNumberOfDilutedSharesOutstanding", "CommonClassBMember", 60e6),
			},
			wantEPS: 2.26, wantShares: 203e6, wantNote: "common stock",
		},
		{
			name: "EPS differs and there is no Class A: skip",
			facts: []DimFact{
				dim("EarningsPerShareDiluted", "CommonClassBMember", 1.0),
				dim("EarningsPerShareDiluted", "CommonClassCMember", 2.0),
			},
			wantNone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eps, shares, note := SelectClass(filing, tc.facts)
			if tc.wantNone {
				if len(eps) != 0 || note != "" {
					t.Fatalf("expected nothing, got eps=%+v note=%q", eps, note)
				}
				return
			}
			if len(eps) != 1 || eps[0].Val != tc.wantEPS {
				t.Fatalf("eps = %+v, want %v", eps, tc.wantEPS)
			}
			if len(shares) != 1 || shares[0].Val != tc.wantShares {
				t.Fatalf("shares = %+v, want %v", shares, tc.wantShares)
			}
			if note != tc.wantNote {
				t.Errorf("note = %q, want %q", note, tc.wantNote)
			}
			if eps[0].Accn != filing.Accession || eps[0].Filed != filing.FilingDate {
				t.Errorf("fact should carry the filing's accession and date: %+v", eps[0])
			}
		})
	}
}
