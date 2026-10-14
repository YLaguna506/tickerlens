// Command fixturegen records SEC EDGAR test fixtures: it downloads the ticker
// list and companyfacts for a few companies and trims them to what the
// normalizer reads, so testdata stays small.
//
//	SEC_USER_AGENT="TickerLens you@example.com" go run ./tools/fixturegen V AAPL SNOW
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
	"github.com/YLaguna506/tickerlens/backend/internal/financials"
)

// Facts older than this are dropped; 10-year CAGRs need data back to ~2015.
const oldest = "2013-01-01"

// Extra tickers kept in the trimmed ticker list for lookup tests.
var extraTickers = []string{"BRK-B"}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fixturegen:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, tickers []string) error {
	ua := os.Getenv("SEC_USER_AGENT")
	if ua == "" || len(tickers) == 0 {
		return fmt.Errorf("usage: SEC_USER_AGENT=... fixturegen TICKER [TICKER...]")
	}
	f := fetch.New(fetch.Options{
		Name:    "sec",
		Limiter: rate.NewLimiter(rate.Every(200*time.Millisecond), 1),
		Header:  http.Header{"User-Agent": {ua}},
	})
	sec := edgar.NewClient(f, edgar.Options{})
	outDir := filepath.Join("testdata", "edgar")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	all, err := sec.Tickers(ctx)
	if err != nil {
		return err
	}
	keep := append(slices.Clone(tickers), extraTickers...)
	subset := map[string]map[string]any{}
	for _, co := range all {
		if !slices.Contains(keep, co.Ticker) {
			continue
		}
		var cik int
		_, _ = fmt.Sscan(strings.TrimLeft(co.CIK, "0"), &cik)
		subset[fmt.Sprint(len(subset))] = map[string]any{"cik_str": cik, "ticker": co.Ticker, "title": co.Name}
	}
	if err := writeJSON(filepath.Join(outDir, "company_tickers.json"), subset); err != nil {
		return err
	}

	wanted := financials.Concepts()
	for _, t := range tickers {
		co, err := sec.LookupTicker(ctx, t)
		if err != nil {
			return err
		}
		facts, err := sec.CompanyFacts(ctx, co.CIK)
		if err != nil {
			return err
		}
		trimmed := &edgar.CompanyFacts{
			CIK:        facts.CIK,
			EntityName: facts.EntityName,
			Facts:      map[string]map[string]edgar.Concept{"us-gaap": {}},
		}
		for name, c := range facts.Facts["us-gaap"] {
			if !slices.Contains(wanted, name) {
				continue
			}
			units := map[string][]edgar.Fact{}
			for unit, list := range c.Units {
				for _, fact := range list {
					if fact.End >= oldest {
						units[unit] = append(units[unit], fact)
					}
				}
			}
			trimmed.Facts["us-gaap"][name] = edgar.Concept{Label: c.Label, Units: units}
		}
		path := filepath.Join(outDir, "companyfacts_"+co.Ticker+".json")
		if err := writeJSON(path, trimmed); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
