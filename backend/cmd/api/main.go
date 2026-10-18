// Command api runs the TickerLens HTTP API and the report pipeline.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/agentclient"
	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
	"github.com/YLaguna506/tickerlens/backend/internal/httpapi"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/pipeline"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
	"github.com/YLaguna506/tickerlens/backend/internal/screener"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// jobTimeout bounds one report. The first report for a multi-class company
// reads ~30 filings from SEC, and the AI agents can take a few minutes on a
// free model; later reports hit the caches.
const jobTimeout = 12 * time.Minute

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Go has no exceptions: run returns an error and main decides what to do.
	if err := run(logger); err != nil {
		logger.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	// ctx is cancelled on Ctrl+C or `docker stop` (SIGTERM). context.Context
	// plays the role of a CancellationToken.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := ingest.Config{
		SECUserAgent:    os.Getenv("SEC_USER_AGENT"),
		TiingoKey:       os.Getenv("TIINGO_API_KEY"),
		AlphaVantageKey: os.Getenv("ALPHA_VANTAGE_API_KEY"),
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set")
	}

	// --- Wiring: build every dependency by hand and pass it in. ---------------
	pool, err := store.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	reports := store.NewReports(pool)
	if n, err := reports.MarkInterrupted(ctx); err != nil {
		return err
	} else if n > 0 {
		logger.Warn("marked reports interrupted by the last shutdown as failed", "count", n)
	}

	scoringCfg, err := loadScoring(logger)
	if err != nil {
		return err
	}

	cache := store.NewPGCache(pool)
	fetcher := ingest.NewFetcher(cfg, cache, store.NewUsage(pool), logger)
	broker := pipeline.NewBroker()
	runner := pipeline.NewRunner(reports, fetcher, broker, scoringCfg, logger, jobTimeout)

	// The AI agents stage runs only when the agents service is configured.
	var models httpapi.Models // stays nil (no model choice) without agents
	if url := os.Getenv("AGENTS_URL"); url != "" {
		agents := agentclient.New(url)
		models = agents
		runner.Agents = &pipeline.Agents{
			Filings:       fetcher.SEC,
			Service:       agents,
			Peers:         fetcher.SEC,
			Cache:         cache,
			FastModel:     fastModel(),
			StrongModel:   envOr("LLM_MODEL_STRONG", fastModel()),
			PromptVersion: "v1",
			// segments v2 adds prior-year figures for year-over-year growth.
			PromptVersions: map[string]string{pipeline.AgentSegments: "v2"},
			MaxDocChars:    40_000,
			// Six agents; on a free model they can take a minute each.
			StageTimeout: 8 * time.Minute,
			Concurrency:  int(envFloat("AGENT_CONCURRENCY", 0)),
			CostCapUSD:   envFloat("LLM_JOB_COST_CAP_USD", 0.25),
		}
		logger.Info("AI agents enabled", "url", url, "provider", os.Getenv("LLM_PROVIDER"), "model", fastModel())
	} else {
		logger.Warn("AGENTS_URL not set: reports will not include AI analysis")
	}

	// Request contexts derive from baseCtx, so cancelling it on shutdown also
	// ends long-lived requests such as event streams, and running screens.
	baseCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()

	// The screener scans without the AI: SEC data for every company, then
	// prices for the strongest. Neither stage needs earnings estimates or
	// insider trades (Alpha Vantage allows 25 requests a day).
	fundamentals, priced := *fetcher, *fetcher
	fundamentals.Prices = nil
	for _, f := range []*ingest.Fetcher{&fundamentals, &priced} {
		f.Earnings, f.Insiders, f.Calendar = nil, nil, nil
	}
	screens := store.NewScreens(pool)
	scr := &screener.Screener{
		Fundamentals: &fundamentals,
		Priced:       &priced,
		Store:        screens,
		Config:       scoringCfg,
		Logger:       logger,
		Base:         baseCtx,
		Workers:      4,
	}
	if err := scr.Resume(ctx); err != nil {
		logger.Warn("could not resume screens", "err", err)
	}
	datasets := fetch.New(fetch.Options{Name: "datasets", Cache: cache, Logger: logger})

	router := httpapi.NewRouter(httpapi.Deps{
		Logger:    logger,
		Reports:   reports,
		Jobs:      runner,
		Events:    broker,
		Tickers:   fetcher.SEC,
		Models:    models,
		Watchlist: store.NewWatchlist(pool),
		Screener:  scr,
		Screens:   screens,
		Universe:  func(ctx context.Context) []store.ScreenMember { return screener.SP500(ctx, datasets) },
		Calendar:  fetcher.Calendar,
		Ready:     pool.Ping,
	})

	addr := envOr("HTTP_ADDR", ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No WriteTimeout: event streams stay open for the length of a report.
		BaseContext: func(net.Listener) context.Context { return baseCtx },
	}

	// Run the server in a goroutine (a lightweight thread) and report its
	// result on a channel, so main can wait for either a shutdown signal or a
	// server failure.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", addr)
		serverErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		// Wrapping with %w keeps the original error inspectable via errors.Is/As,
		// like an InnerException.
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	// 1. Stop running reports; each records that it was interrupted and
	//    tells its listeners. 2. Close the server and any open streams.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runner.Shutdown(shutdownCtx); err != nil {
		logger.Warn("reports still running at shutdown", "err", err)
	}
	cancelRequests()
	scr.Wait() // a cancelled screen stays "scanning"/"pricing" and resumes next start
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// loadScoring reads SCORING_CONFIG (config/scoring.yaml). Without it the
// built-in defaults apply, which match the shipped file.
func loadScoring(logger *slog.Logger) (scoring.Config, error) {
	path := os.Getenv("SCORING_CONFIG")
	if path == "" {
		logger.Warn("SCORING_CONFIG not set: using built-in scoring defaults")
		return scoring.Default(), nil
	}
	cfg, err := scoring.Load(path)
	if err != nil {
		return cfg, err
	}
	logger.Info("scoring config loaded", "path", path, "version", cfg.Version)
	return cfg, nil
}

// fastModel mirrors the agents service: LLM_MODEL_FAST, or the provider's own
// model variable (e.g. GEMINI_LLM_MODEL) when that is empty. It is part of the
// agent cache key, so switching models gives fresh answers.
func fastModel() string {
	provider := strings.ToUpper(strings.TrimSpace(os.Getenv("LLM_PROVIDER")))
	return envOr("LLM_MODEL_FAST", os.Getenv(provider+"_LLM_MODEL"))
}

func envFloat(key string, fallback float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil {
		return v
	}
	return fallback
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
