package financials

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
)

// Run `go test ./internal/financials -update` to rewrite the golden files
// after an intentional change, then review the diff.
var update = flag.Bool("update", false, "rewrite golden files")

var companies = map[string]edgar.Company{
	"V":    {Ticker: "V", CIK: "0001403161", Name: "VISA INC."},
	"AAPL": {Ticker: "AAPL", CIK: "0000320193", Name: "Apple Inc."},
	"SNOW": {Ticker: "SNOW", CIK: "0001640147", Name: "Snowflake Inc."},
}

func load(t *testing.T, ticker string) Financials {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "edgar", "companyfacts_"+ticker+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var facts edgar.CompanyFacts
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	return Normalize(companies[ticker], &facts)
}

// TestGolden compares the full normalized output with a reviewed snapshot, so
// any change in behaviour shows up as a diff.
func TestGolden(t *testing.T) {
	for ticker := range companies {
		t.Run(ticker, func(t *testing.T) {
			got, err := json.MarshalIndent(load(t, ticker), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("..", "..", "testdata", "golden", "financials_"+ticker+".json")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden file (run with -update): %v", err)
			}
			if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
				t.Errorf("output differs from %s; run with -update and review the diff", path)
			}
		})
	}
}
