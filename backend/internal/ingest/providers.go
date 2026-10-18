package ingest

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
)

// Config holds provider credentials, usually read from the environment.
type Config struct {
	SECUserAgent    string // "TickerLens you@example.com"
	TiingoKey       string // optional
	AlphaVantageKey string // optional
}

// Validate checks the settings every run needs.
func (c Config) Validate() error {
	if !strings.Contains(c.SECUserAgent, "@") || strings.Contains(c.SECUserAgent, "you@example.com") {
		return errors.New("SEC_USER_AGENT must identify you, e.g. \"TickerLens you@yourmail.com\" (SEC requires it)")
	}
	return nil
}

// NewFetcher wires the real provider clients. cache and budget may be nil
// (no caching; no Alpha Vantage daily budget tracking).
func NewFetcher(cfg Config, cache fetch.Cache, budget estimates.Budget, logger *slog.Logger) *Fetcher {
	sec := fetch.New(fetch.Options{
		Name: "sec",
		// SEC allows 10 requests/second; stay a little under.
		Limiter: rate.NewLimiter(8, 1),
		Cache:   cache,
		Header:  http.Header{"User-Agent": {cfg.SECUserAgent}},
		Logger:  logger,
	})
	secClient := edgar.NewClient(sec, edgar.Options{Cache: cache})
	f := &Fetcher{SEC: secClient, Fundamentals: secClient, ClassFacts: secClient, Fresh: secClient, Profiles: secClient}

	if cfg.TiingoKey != "" {
		tiingo := fetch.New(fetch.Options{
			Name: "tiingo",
			// Free tier: 50 requests/hour.
			Limiter: rate.NewLimiter(rate.Every(time.Hour/50), 5),
			Cache:   cache,
			Header:  http.Header{"Authorization": {"Token " + cfg.TiingoKey}},
			Logger:  logger,
		})
		f.Prices = prices.NewTiingo(tiingo, "")
	}

	if cfg.AlphaVantageKey != "" {
		opts := fetch.Options{
			Name: estimates.Provider,
			// Free tier: 25 requests/day, and bursts are throttled.
			Limiter: rate.NewLimiter(rate.Every(12*time.Second), 1),
			Cache:   cache,
			Logger:  logger,
		}
		if budget != nil {
			opts.BeforeNetwork = estimates.BudgetHook(budget, estimates.DailyLimit, time.Now)
		}
		f.Earnings = estimates.NewClient(fetch.New(opts), cfg.AlphaVantageKey, "")
	}
	return f
}
