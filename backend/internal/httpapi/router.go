// Package httpapi holds the HTTP handlers for the TickerLens API.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/agentclient"
	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/pipeline"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// Interfaces for what the handlers use, declared here where they are
// consumed so tests can pass fakes.

// Reports reads and creates report rows (store.Reports).
type Reports interface {
	Create(ctx context.Context, ticker, companyName string, opt store.Options) (store.Report, error)
	Get(ctx context.Context, id string) (store.Report, error)
	LatestFinished(ctx context.Context, ticker string, opt store.Options, since time.Time) (store.Report, error)
	ListByTicker(ctx context.Context, ticker string, limit int) ([]store.Report, error)
	ClearTicker(ctx context.Context, ticker string) (store.Cleared, error)
}

// Jobs starts report jobs in the background (pipeline.Runner).
type Jobs interface {
	Start(reportID, ticker string, refresh bool, opt store.Options)
}

// Models lists the AI models a report can use (agentclient.Client).
type Models interface {
	Models(ctx context.Context) ([]agentclient.ModelChoice, error)
}

// Events streams progress for running jobs (pipeline.Broker).
type Events interface {
	Subscribe(reportID string) (<-chan pipeline.Event, func())
}

// Tickers lists every SEC-registered company (edgar.Client).
type Tickers interface {
	Tickers(ctx context.Context) ([]edgar.Company, error)
}

// Deps are the handlers' dependencies.
//
// C# note: there is no DI container. main.go builds these values and passes
// them in; tests pass fakes the same way.
type Deps struct {
	Logger  *slog.Logger
	Reports Reports
	Jobs    Jobs
	Events  Events
	Tickers Tickers
	// Models is nil when the AI agents are not configured.
	Models Models
	// Watchlist is nil when there is no database (its routes are left out).
	Watchlist Watchlist
	// Ready reports whether dependencies (Postgres) are reachable.
	Ready func(ctx context.Context) error
	// FreshFor is how long a finished report is returned instead of
	// starting a new one (default 12h: prices change once a day).
	FreshFor time.Duration
}

// NewRouter wires every route and returns a single http.Handler.
func NewRouter(d Deps) http.Handler {
	if d.FreshFor == 0 {
		d.FreshFor = 12 * time.Hour
	}
	a := &api{Deps: d}
	mux := http.NewServeMux()

	// Since Go 1.22 the standard mux supports method + path patterns,
	// similar to [HttpGet("/healthz")] in ASP.NET.
	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("GET /readyz", a.handleReady)

	mux.HandleFunc("GET /api/tickers", a.searchTickers)
	mux.HandleFunc("GET /api/models", a.listModels)
	mux.HandleFunc("POST /api/reports", a.createReport)
	mux.HandleFunc("GET /api/reports", a.listReports)
	mux.HandleFunc("GET /api/reports/{id}", a.getReport)
	mux.HandleFunc("GET /api/reports/{id}/events", a.streamEvents)
	mux.HandleFunc("DELETE /api/tickers/{ticker}/cache", a.clearTicker)
	if d.Watchlist != nil {
		mux.HandleFunc("GET /api/watchlist", a.listWatchlist)
		mux.HandleFunc("POST /api/watchlist", a.addToWatchlist)
		mux.HandleFunc("POST /api/watchlist/refresh", a.refreshWatchlist)
		mux.HandleFunc("GET /api/watchlist/{ticker}", a.watchStatus)
		mux.HandleFunc("DELETE /api/watchlist/{ticker}", a.removeFromWatchlist)
	}

	return logRequests(d.Logger, mux)
}

type api struct {
	Deps
}

type healthResponse struct {
	// Struct tags control the JSON field name, like [JsonPropertyName("status")].
	Status string `json:"status"`
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (a *api) handleReady(w http.ResponseWriter, r *http.Request) {
	if a.Ready != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := a.Ready(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "unavailable: " + err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

type errorResponse struct {
	Error       string          `json:"error"`
	Suggestions []edgar.Company `json:"suggestions,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status line is already sent, so an encode error (usually a client
	// that disconnected) cannot change the response. `_ =` ignores it explicitly.
	_ = json.NewEncoder(w).Encode(body)
}

// logRequests is middleware: a function that wraps one handler in another,
// the Go equivalent of an ASP.NET middleware delegate.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		// slog's JSON handler writes a time.Duration as integer nanoseconds,
		// so log milliseconds explicitly for readability.
		elapsedMs := float64(time.Since(start).Microseconds()) / 1000
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration_ms", elapsedMs)
	})
}
