package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/pipeline"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// GET /api/tickers?q=vis → up to 10 matching companies.
func (a *api) searchTickers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "query parameter q is required")
		return
	}
	all, err := a.Tickers.Tickers(r.Context())
	if err != nil {
		a.Logger.Error("ticker list", "err", err)
		writeError(w, http.StatusBadGateway, "could not load the SEC ticker list")
		return
	}
	writeJSON(w, http.StatusOK, search(all, q, 10))
}

// search ranks companies: exact ticker, ticker prefix, name prefix, then
// name contains.
func search(all []edgar.Company, q string, limit int) []edgar.Company {
	ticker := edgar.NormalizeTicker(q)
	name := strings.ToLower(q)
	type hit struct {
		co   edgar.Company
		rank int
	}
	var hits []hit
	for _, co := range all {
		switch {
		case co.Ticker == ticker:
			hits = append(hits, hit{co, 0})
		case strings.HasPrefix(co.Ticker, ticker):
			hits = append(hits, hit{co, 1})
		case strings.HasPrefix(strings.ToLower(co.Name), name):
			hits = append(hits, hit{co, 2})
		case strings.Contains(strings.ToLower(co.Name), name):
			hits = append(hits, hit{co, 3})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		if len(hits[i].co.Ticker) != len(hits[j].co.Ticker) {
			return len(hits[i].co.Ticker) < len(hits[j].co.Ticker)
		}
		return hits[i].co.Ticker < hits[j].co.Ticker
	})
	out := []edgar.Company{}
	for i := 0; i < len(hits) && i < limit; i++ {
		out = append(out, hits[i].co)
	}
	return out
}

// suggest finds near matches for an unknown ticker, dropping letters from the
// end until something matches (a typo like "MSFTT" still finds MSFT).
func suggest(all []edgar.Company, ticker string) []edgar.Company {
	for n := len(ticker); n > 0; n-- {
		if hits := search(all, ticker[:n], 5); len(hits) > 0 {
			return hits
		}
	}
	return nil
}

type createRequest struct {
	Ticker  string `json:"ticker"`
	Refresh bool   `json:"refresh"`
}

type createResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Cached is true when an existing, recent report was returned instead of
	// starting a new one. Send "refresh": true to force a new one.
	Cached bool `json:"cached"`
}

// POST /api/reports {"ticker": "V", "refresh": false}
func (a *api) createReport(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Ticker) == "" {
		writeError(w, http.StatusBadRequest, `body must be JSON like {"ticker": "V"}`)
		return
	}

	all, err := a.Tickers.Tickers(r.Context())
	if err != nil {
		a.Logger.Error("ticker list", "err", err)
		writeError(w, http.StatusBadGateway, "could not load the SEC ticker list")
		return
	}
	ticker := edgar.NormalizeTicker(req.Ticker)
	var company *edgar.Company
	for i := range all {
		if all[i].Ticker == ticker {
			company = &all[i]
			break
		}
	}
	if company == nil {
		writeJSON(w, http.StatusNotFound, errorResponse{
			Error:       fmt.Sprintf("unknown ticker %q: TickerLens covers US companies that file with the SEC", ticker),
			Suggestions: suggest(all, ticker),
		})
		return
	}

	if !req.Refresh {
		prev, err := a.Reports.LatestFinished(r.Context(), ticker, time.Now().Add(-a.FreshFor))
		if err == nil {
			w.Header().Set("Location", "/api/reports/"+prev.ID)
			writeJSON(w, http.StatusOK, createResponse{ID: prev.ID, Status: prev.Status, Cached: true})
			return
		}
		if !errors.Is(err, store.ErrNotFound) {
			a.Logger.Error("latest report", "err", err)
		}
	}

	rep, err := a.Reports.Create(r.Context(), ticker, company.Name)
	if err != nil {
		a.Logger.Error("create report", "err", err)
		writeError(w, http.StatusInternalServerError, "could not create the report")
		return
	}
	a.Jobs.Start(rep.ID, ticker, req.Refresh)
	w.Header().Set("Location", "/api/reports/"+rep.ID)
	writeJSON(w, http.StatusAccepted, createResponse{ID: rep.ID, Status: rep.Status})
}

// GET /api/reports/{id} → the report, partial while it is running.
func (a *api) getReport(w http.ResponseWriter, r *http.Request) {
	rep, err := a.Reports.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "report not found")
		return
	}
	if err != nil {
		a.Logger.Error("get report", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load the report")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// GET /api/reports?ticker=V → recent reports for a ticker (without bodies).
func (a *api) listReports(w http.ResponseWriter, r *http.Request) {
	ticker := edgar.NormalizeTicker(r.URL.Query().Get("ticker"))
	if ticker == "" {
		writeError(w, http.StatusBadRequest, "query parameter ticker is required")
		return
	}
	list, err := a.Reports.ListByTicker(r.Context(), ticker, 20)
	if err != nil {
		a.Logger.Error("list reports", "err", err)
		writeError(w, http.StatusInternalServerError, "could not list reports")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// GET /api/reports/{id}/events → Server-Sent Events with live progress.
//
// The stream starts with a "snapshot" of the current state, then relays
// events until the job finishes. A finished report gets the snapshot and its
// final event straight away.
func (a *api) streamEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Subscribe before reading the state, so no event is lost in between.
	events, unsubscribe := a.Events.Subscribe(id)
	defer unsubscribe()

	rep, err := a.Reports.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "report not found")
		return
	}
	if err != nil {
		a.Logger.Error("get report", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load the report")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // tell proxies (nginx) not to buffer
	w.WriteHeader(http.StatusOK)

	// http.ResponseController reaches the underlying connection to flush each
	// event immediately instead of when the response ends.
	rc := http.NewResponseController(w)
	send := func(e pipeline.Event) bool {
		data, _ := json.Marshal(e)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data); err != nil {
			return false
		}
		return rc.Flush() == nil
	}

	if !send(pipeline.Event{Type: "snapshot", Stage: rep.Stage, Status: rep.Status, Time: time.Now().UTC()}) {
		return
	}
	if rep.Finished() {
		final := pipeline.Event{Type: pipeline.JobDone, Status: rep.Status, Message: rep.Error, Time: time.Now().UTC()}
		if rep.Status == store.StatusFailed {
			final.Type = pipeline.JobFailed
		}
		send(final)
		return
	}

	// A comment line every 15s keeps proxies from closing an idle stream.
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return // the client went away
		case e := <-events:
			if !send(e) || e.Terminal() {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
