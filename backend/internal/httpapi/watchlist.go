package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// Watchlist stores the tickers the user follows (store.Watchlist).
type Watchlist interface {
	Add(ctx context.Context, ticker, companyName string) error
	Remove(ctx context.Context, ticker string) (bool, error)
	Contains(ctx context.Context, ticker string) (bool, error)
	List(ctx context.Context) ([]store.WatchItem, error)
}

// Calendar is the market's upcoming earnings dates (estimates.Client).
type Calendar interface {
	Calendar(ctx context.Context) (map[string]estimates.CalendarEntry, error)
}

// maxRefresh is how many reports one "update" starts: each report calls the
// LLM several times and prices are limited to 50 requests an hour.
const maxRefresh = 5

// GET /api/watchlist → every watched ticker with its latest report summary.
func (a *api) listWatchlist(w http.ResponseWriter, r *http.Request) {
	items, err := a.Watchlist.List(r.Context())
	if err != nil {
		a.Logger.Error("list watchlist", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load the watchlist")
		return
	}
	a.withCalendar(r.Context(), items)
	writeJSON(w, http.StatusOK, items)
}

// withCalendar replaces each ticker's next earnings date with the calendar's
// when it has one: a date can be announced (or moved) after the report was
// built. The calendar is cached for 12 hours; if it is slow or fails, the
// reports' dates stay.
func (a *api) withCalendar(ctx context.Context, items []store.WatchItem) {
	if a.Calendar == nil || len(items) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cal, err := a.Calendar.Calendar(ctx)
	if err != nil {
		a.Logger.Warn("earnings calendar", "err", err)
		return
	}
	today := time.Now().Format(time.DateOnly)
	for i := range items {
		if e, ok := cal[items[i].Ticker]; ok && e.ReportDate >= today {
			items[i].NextEarnings = &store.NextEarnings{Date: e.ReportDate, Time: e.Time, Source: "calendar", EstimateEPS: e.EstimateEPS}
		}
	}
}

// POST /api/watchlist {"ticker": "PEP"} → adds a ticker.
func (a *api) addToWatchlist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ticker string `json:"ticker"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Ticker) == "" {
		writeError(w, http.StatusBadRequest, `body must be JSON like {"ticker": "V"}`)
		return
	}
	co, ok := a.lookup(w, r, req.Ticker)
	if !ok {
		return
	}
	if err := a.Watchlist.Add(r.Context(), co.Ticker, co.Name); err != nil {
		a.Logger.Error("add to watchlist", "err", err)
		writeError(w, http.StatusInternalServerError, "could not add to the watchlist")
		return
	}
	writeJSON(w, http.StatusCreated, watchState{Ticker: co.Ticker, Watching: true})
}

type watchState struct {
	Ticker   string `json:"ticker"`
	Watching bool   `json:"watching"`
}

// GET /api/watchlist/{ticker} → whether the ticker is watched.
func (a *api) watchStatus(w http.ResponseWriter, r *http.Request) {
	ticker := edgar.NormalizeTicker(r.PathValue("ticker"))
	ok, err := a.Watchlist.Contains(r.Context(), ticker)
	if err != nil {
		a.Logger.Error("watchlist contains", "err", err)
		writeError(w, http.StatusInternalServerError, "could not read the watchlist")
		return
	}
	writeJSON(w, http.StatusOK, watchState{Ticker: ticker, Watching: ok})
}

// DELETE /api/watchlist/{ticker} → removes a ticker.
func (a *api) removeFromWatchlist(w http.ResponseWriter, r *http.Request) {
	ticker := edgar.NormalizeTicker(r.PathValue("ticker"))
	removed, err := a.Watchlist.Remove(r.Context(), ticker)
	if err != nil {
		a.Logger.Error("remove from watchlist", "err", err)
		writeError(w, http.StatusInternalServerError, "could not remove from the watchlist")
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, ticker+" is not on the watchlist")
		return
	}
	writeJSON(w, http.StatusOK, watchState{Ticker: ticker, Watching: false})
}

type refreshResponse struct {
	Started []string `json:"started"` // tickers whose report is now being built
	Waiting []string `json:"waiting"` // stale tickers left for the next update
}

// POST /api/watchlist/refresh {"lang": "es"} → starts new reports for the
// tickers whose latest report is older than FreshFor (or missing), up to
// maxRefresh at a time. Tickers already being built are skipped.
func (a *api) refreshWatchlist(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	// Every field is optional, so an empty body is fine.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, `body must be JSON like {"lang": "en"}`)
		return
	}
	opt, msg := a.options(r, req)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	items, err := a.Watchlist.List(r.Context())
	if err != nil {
		a.Logger.Error("list watchlist", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load the watchlist")
		return
	}
	resp := refreshResponse{Started: []string{}, Waiting: []string{}}
	stale := time.Now().Add(-a.FreshFor)
	for _, it := range items {
		if it.Running != nil || (it.Latest != nil && it.Latest.CreatedAt.After(stale)) {
			continue
		}
		if len(resp.Started) >= maxRefresh {
			resp.Waiting = append(resp.Waiting, it.Ticker)
			continue
		}
		rep, err := a.Reports.Create(r.Context(), it.Ticker, it.CompanyName, opt)
		if err != nil {
			a.Logger.Error("create report", "ticker", it.Ticker, "err", err)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("could not start a report for %s", it.Ticker))
			return
		}
		a.Jobs.Start(rep.ID, it.Ticker, false, opt)
		resp.Started = append(resp.Started, it.Ticker)
	}
	writeJSON(w, http.StatusOK, resp)
}

// lookup resolves a ticker to its SEC company, writing the error response
// (with suggestions) when it is unknown.
func (a *api) lookup(w http.ResponseWriter, r *http.Request, ticker string) (edgar.Company, bool) {
	all, err := a.Tickers.Tickers(r.Context())
	if err != nil {
		a.Logger.Error("ticker list", "err", err)
		writeError(w, http.StatusBadGateway, "could not load the SEC ticker list")
		return edgar.Company{}, false
	}
	want := edgar.NormalizeTicker(ticker)
	for _, co := range all {
		if co.Ticker == want {
			return co, true
		}
	}
	writeJSON(w, http.StatusNotFound, errorResponse{
		Error:       fmt.Sprintf("unknown ticker %q: TickerLens covers US companies that file with the SEC", want),
		Suggestions: suggest(all, want),
	})
	return edgar.Company{}, false
}
