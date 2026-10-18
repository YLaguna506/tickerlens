// Command tickerlens is the developer CLI.
//
//	tickerlens fetch V            print normalized financials, prices and earnings
//	tickerlens metrics V          print the Financial overview, phase and scores
//	tickerlens fetch -json AAPL   print the full data as JSON
//	tickerlens health             check the data of many tickers for errors
//	tickerlens peers PEP          print the company's comparison group
//
// Settings come from the environment (`task cli -- fetch V` loads .env).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"

	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/metrics"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
	"github.com/YLaguna506/tickerlens/backend/internal/verdict"
)

const usage = `usage: tickerlens <command> [flags] TICKER

commands:
  fetch     fetch and print normalized financials, prices and earnings
  metrics   compute and print the Financial overview, phase and scores
  health    check many tickers' data for signs it was read wrongly
  peers     print the comparison group (same industry code) and its medians

run "tickerlens <command> -h" for flags`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "fetch":
		err = runFetch(ctx, args[1:], stdout, stderr)
	case "metrics":
		err = runMetrics(ctx, args[1:], stdout, stderr)
	case "peers":
		err = runPeers(ctx, args[1:], stdout, stderr)
	case "health":
		var failed bool
		failed, err = runHealth(ctx, args[1:], stdout, stderr)
		if err == nil && failed {
			return 1
		}
	case "-h", "--help", "help":
		fmt.Fprintln(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s\n", args[0], usage)
		return 2
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// common holds the flags every command shares.
type common struct {
	asJSON  *bool
	noCache *bool
	verbose *bool
}

// newFlags creates a command's flag set with the shared flags.
//
// C# note: the flag package is Go's built-in argument parser; each call
// returns a pointer that is filled in by fs.Parse.
func newFlags(name string, stderr io.Writer) (*flag.FlagSet, common) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	c := common{
		asJSON:  fs.Bool("json", false, "print the result as JSON"),
		noCache: fs.Bool("no-cache", false, "skip Postgres: no caching and no Alpha Vantage budget tracking"),
		verbose: fs.Bool("v", false, "log provider requests and retries"),
	}
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: tickerlens %s [flags] TICKER\n", name)
		fs.PrintDefaults()
	}
	return fs, c
}

func parse(fs *flag.FlagSet, args []string) (string, error) {
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return "", errors.New("expected exactly one ticker")
	}
	return fs.Arg(0), nil
}

func runFetch(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs, c := newFlags("fetch", stderr)
	years := fs.Int("years", 5, "fiscal years to show")
	quarters := fs.Int("quarters", 4, "fiscal quarters to show")
	ticker, err := parse(fs, args)
	if err != nil {
		return err
	}
	bundle, err := loadBundle(ctx, ticker, c, stderr)
	if err != nil {
		return err
	}
	if *c.asJSON {
		return writeJSON(stdout, bundle)
	}
	printBundle(stdout, bundle, *years, *quarters)
	return nil
}

func runMetrics(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs, c := newFlags("metrics", stderr)
	ticker, err := parse(fs, args)
	if err != nil {
		return err
	}
	bundle, err := loadBundle(ctx, ticker, c, stderr)
	if err != nil {
		return err
	}
	cfg, err := scoringConfig()
	if err != nil {
		return err
	}
	report := metrics.Compute(bundle, cfg)
	// The CLI runs no AI agents, so the verdict has no business quality score.
	v := verdict.Decide(verdict.Input{
		Growth:             report.GrowthScore.Value,
		FinancialHealth:    report.HealthScore.Value,
		Valuation:          report.ValuationScore.Value,
		ValuationFellBack:  report.Bands != nil && report.Bands.FellBackFrom != "",
		PhaseLowConfidence: report.Phase.LowConfidence,
		MissingData:        bundle.Failed,
		LimitedSupport:     limitedKind(report),
		Insiders:           insidersInput(report),
	}, cfg)
	if *c.asJSON {
		return writeJSON(stdout, map[string]any{"metrics": report, "verdict": v})
	}
	printReport(stdout, &report, bundle)
	printVerdict(stdout, v)
	return nil
}

// loadBundle wires the providers from the environment and fetches a ticker.
func loadBundle(ctx context.Context, ticker string, c common, stderr io.Writer) (*ingest.Bundle, error) {
	f, err := newFetcher(ctx, c, stderr)
	if err != nil {
		return nil, err
	}
	return f.Fetch(ctx, ticker)
}

// newFetcher wires the providers from the environment.
func newFetcher(ctx context.Context, c common, stderr io.Writer) (*ingest.Fetcher, error) {
	level := slog.LevelWarn
	if *c.verbose {
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	cfg := ingest.Config{
		SECUserAgent:    os.Getenv("SEC_USER_AGENT"),
		TiingoKey:       os.Getenv("TIINGO_API_KEY"),
		AlphaVantageKey: os.Getenv("ALPHA_VANTAGE_API_KEY"),
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Interfaces holding nil: declared with the interface type so that "no
	// cache" is a true nil interface, not a nil *PGCache inside an interface
	// (a classic Go gotcha: that would be non-nil).
	var cache fetch.Cache
	var budget estimates.Budget
	if !*c.noCache {
		dbURL := os.Getenv("DATABASE_URL")
		if dbURL == "" {
			return nil, errors.New("DATABASE_URL is not set (run `task up` first, or pass -no-cache)")
		}
		pool, err := store.Open(ctx, dbURL)
		if err != nil {
			return nil, fmt.Errorf("%w\n(is Postgres running? try `task up`, or pass -no-cache)", err)
		}
		// The pool lives until the process exits; the CLI runs one command.
		cache, budget = store.NewPGCache(pool), store.NewUsage(pool)
	}

	return ingest.NewFetcher(cfg, cache, budget, logger), nil
}

// scoringConfig reads SCORING_CONFIG, or the built-in defaults.
func scoringConfig() (scoring.Config, error) {
	if path := os.Getenv("SCORING_CONFIG"); path != "" {
		return scoring.Load(path)
	}
	return scoring.Default(), nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func limitedKind(r metrics.Report) string {
	if r.LimitedSupport == nil {
		return ""
	}
	return r.LimitedSupport.Kind
}

func insidersInput(r metrics.Report) *verdict.Insiders {
	if r.Insiders == nil {
		return nil
	}
	return &verdict.Insiders{BoughtUSD: r.Insiders.BoughtUSD, SoldUSD: r.Insiders.SoldUSD,
		PlannedSoldUSD: r.Insiders.PlannedSoldUSD, MarketCap: r.MarketCap.Value}
}
