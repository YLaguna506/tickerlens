package main

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/YLaguna506/tickerlens/backend/internal/health"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/metrics"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

// defaultTickers is a spread of companies with unusual calendars (52/53-week
// years, 16-week quarters, January year-ends), several share classes, splits
// and heavy debt: the cases that have broken normalization before.
//
//go:embed health_tickers.txt
var defaultTickers string

// tickerResult is one ticker's outcome, in input order.
type tickerResult struct {
	Ticker string         `json:"ticker"`
	Error  string         `json:"error,omitempty"`
	Issues []health.Issue `json:"issues"`
}

// runHealth checks each ticker's data. It reports failed=true when any ticker
// has an error-level issue or could not be fetched.
func runHealth(ctx context.Context, args []string, stdout, stderr io.Writer) (failed bool, err error) {
	fs, c := newFlags("health", stderr)
	withPrices := fs.Bool("prices", true, "fetch prices too (Tiingo allows 50 requests/hour) and check the valuation")
	file := fs.String("file", "", "read tickers from this file (one per line, # comments) instead of the built-in list")
	warnings := fs.Bool("warn", true, "show warnings as well as errors")
	workers := fs.Int("workers", 4, "tickers fetched at once")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: tickerlens health [flags] [TICKER...]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return false, err
	}

	tickers := fs.Args()
	if len(tickers) == 0 {
		list := defaultTickers
		if *file != "" {
			raw, err := os.ReadFile(*file)
			if err != nil {
				return false, err
			}
			list = string(raw)
		}
		tickers = parseTickers(list)
	}

	f, err := newFetcher(ctx, c, stderr)
	if err != nil {
		return false, err
	}
	// Earnings are not checked, and Alpha Vantage allows 25 requests a day.
	// Insider trades are not checked either (a request per Form 4).
	f.Earnings, f.Insiders, f.Calendar = nil, nil, nil
	if !*withPrices {
		f.Prices = nil
	}
	cfg, err := scoringConfig()
	if err != nil {
		return false, err
	}

	results := make([]tickerResult, len(tickers))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(max(*workers, 1))
	for i, t := range tickers {
		g.Go(func() error {
			results[i] = checkTicker(gctx, f, t, cfg, *withPrices)
			if !*c.asJSON {
				fmt.Fprintf(stderr, "checked %s\n", t)
			}
			return nil
		})
	}
	_ = g.Wait() // checkTicker records its own errors

	for i := range results {
		if !*warnings {
			results[i].Issues = slices.DeleteFunc(results[i].Issues, func(is health.Issue) bool { return is.Severity != health.Error })
		}
		if results[i].Error != "" || slices.ContainsFunc(results[i].Issues, func(is health.Issue) bool { return is.Severity == health.Error }) {
			failed = true
		}
	}
	if *c.asJSON {
		return failed, writeJSON(stdout, results)
	}
	printHealth(stdout, results)
	return failed, nil
}

func checkTicker(ctx context.Context, f *ingest.Fetcher, ticker string, cfg scoring.Config, prices bool) tickerResult {
	res := tickerResult{Ticker: strings.ToUpper(ticker)}
	b, err := f.Fetch(ctx, ticker)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	r := metrics.Compute(b, cfg)
	res.Issues = health.Check(b, &r, prices, time.Now())
	return res
}

func parseTickers(list string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(list))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		out = append(out, strings.Fields(line)...)
	}
	return out
}

func printHealth(w io.Writer, results []tickerResult) {
	counts := map[string]int{}
	var bad int
	for _, r := range results {
		switch {
		case r.Error != "":
			bad++
			fmt.Fprintf(w, "%-6s FAILED  %s\n", r.Ticker, r.Error)
		case len(r.Issues) == 0:
			fmt.Fprintf(w, "%-6s ok\n", r.Ticker)
		default:
			bad++
			for i, is := range r.Issues {
				name := ""
				if i == 0 {
					name = r.Ticker
				}
				fmt.Fprintf(w, "%-6s %-5s  %-15s %s\n", name, is.Severity, is.Check, is.Detail)
				counts[is.Check]++
			}
		}
	}
	fmt.Fprintf(w, "\n%d of %d tickers have issues\n", bad, len(results))
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "  %-15s %d\n", k, counts[k])
	}
}
