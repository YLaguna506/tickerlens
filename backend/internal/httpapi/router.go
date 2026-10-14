// Package httpapi holds the HTTP handlers for the TickerLens API.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// NewRouter wires every route and returns a single http.Handler.
//
// C# note: there is no DI container here. Dependencies (stores, queues) will be
// passed in as constructor arguments and wired by hand in cmd/api/main.go.
func NewRouter(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// Since Go 1.22 the standard mux supports method + path patterns,
	// similar to [HttpGet("/healthz")] in ASP.NET.
	mux.HandleFunc("GET /healthz", handleHealth)
	// Readiness will check Postgres, Redis and NATS once the API uses them (M3).
	mux.HandleFunc("GET /readyz", handleHealth)

	return logRequests(logger, mux)
}

type healthResponse struct {
	// Struct tags control the JSON field name, like [JsonPropertyName("status")].
	Status string `json:"status"`
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		// slog's JSON handler writes a time.Duration as integer nanoseconds,
		// so log milliseconds explicitly for readability.
		elapsedMs := float64(time.Since(start).Microseconds()) / 1000
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration_ms", elapsedMs)
	})
}
