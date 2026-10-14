// Command tickerlens is the developer CLI.
//
//	tickerlens fetch V            print normalized financials, prices and earnings
//	tickerlens fetch -json AAPL   print the full bundle as JSON
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
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

const usage = `usage: tickerlens <command> [flags]

commands:
  fetch TICKER   fetch and print normalized data for a ticker

run "tickerlens fetch -h" for flags`

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

func runFetch(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	// C# note: the flag package is Go's built-in argument parser; each call
	// returns a pointer that is filled in by fs.Parse.
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the full bundle as JSON")
	noCache := fs.Bool("no-cache", false, "skip Postgres: no caching and no Alpha Vantage budget tracking")
	years := fs.Int("years", 5, "fiscal years to show")
	quarters := fs.Int("quarters", 4, "fiscal quarters to show")
	verbose := fs.Bool("v", false, "log provider requests and retries")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: tickerlens fetch [flags] TICKER")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("expected exactly one ticker")
	}

	level := slog.LevelWarn
	if *verbose {
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))

	cfg := ingest.Config{
		SECUserAgent:    os.Getenv("SEC_USER_AGENT"),
		TiingoKey:       os.Getenv("TIINGO_API_KEY"),
		AlphaVantageKey: os.Getenv("ALPHA_VANTAGE_API_KEY"),
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Interfaces holding nil: declared with the interface type so that "no
	// cache" is a true nil interface, not a nil *PGCache inside an interface
	// (a classic Go gotcha: that would be non-nil).
	var cache fetch.Cache
	var budget estimates.Budget
	if !*noCache {
		dbURL := os.Getenv("DATABASE_URL")
		if dbURL == "" {
			return errors.New("DATABASE_URL is not set (run `task up` first, or pass -no-cache)")
		}
		pool, err := store.Open(ctx, dbURL)
		if err != nil {
			return fmt.Errorf("%w\n(is Postgres running? try `task up`, or pass -no-cache)", err)
		}
		defer pool.Close()
		cache, budget = store.NewPGCache(pool), store.NewUsage(pool)
	}

	fetcher := ingest.NewFetcher(cfg, cache, budget, logger)
	bundle, err := fetcher.Fetch(ctx, fs.Arg(0))
	if err != nil {
		return err
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(bundle)
	}
	printBundle(stdout, bundle, *years, *quarters)
	return nil
}
