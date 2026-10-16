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
	"syscall"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/agentclient"
	"github.com/YLaguna506/tickerlens/backend/internal/httpapi"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/pipeline"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// jobTimeout bounds one report. The first report for a multi-class company
// reads ~30 filings from SEC, and the AI agents can take a few minutes on a
// free model; later reports hit the caches.
const jobTimeout = 8 * time.Minute

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

	cache := store.NewPGCache(pool)
	fetcher := ingest.NewFetcher(cfg, cache, store.NewUsage(pool), logger)
	broker := pipeline.NewBroker()
	runner := pipeline.NewRunner(reports, fetcher, broker, scoring.Default(), logger, jobTimeout)

	// The AI agents stage runs only when the agents service is configured.
	if url := os.Getenv("AGENTS_URL"); url != "" {
		runner.Agents = &pipeline.Agents{
			Filings:       fetcher.SEC,
			Service:       agentclient.New(url),
			Cache:         cache,
			FastModel:     os.Getenv("LLM_MODEL_FAST"),
			PromptVersion: "v1",
			MaxDocChars:   40_000,
		}
		logger.Info("AI agents enabled", "url", url, "model", os.Getenv("LLM_MODEL_FAST"))
	} else {
		logger.Warn("AGENTS_URL not set: reports will not include AI analysis")
	}

	router := httpapi.NewRouter(httpapi.Deps{
		Logger:  logger,
		Reports: reports,
		Jobs:    runner,
		Events:  broker,
		Tickers: fetcher.SEC,
		Ready:   pool.Ping,
	})

	// Request contexts derive from baseCtx, so cancelling it on shutdown also
	// ends long-lived requests such as event streams.
	baseCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
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
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
