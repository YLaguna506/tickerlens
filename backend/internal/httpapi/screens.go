package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/YLaguna506/tickerlens/backend/internal/screener"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// Screener runs screens in the background (screener.Screener).
type Screener interface {
	Start(ctx context.Context, universe string, members []store.ScreenMember, params store.ScreenParams) (store.Screen, error)
	Cancel(id string) bool
	Running(id string) bool
}

// ScreenStore reads screens and their results (store.Screens).
type ScreenStore interface {
	Get(ctx context.Context, id string) (store.Screen, error)
	Latest(ctx context.Context) (store.Screen, error)
	Results(ctx context.Context, id string) ([]store.ScreenResult, error)
}

type screenRequest struct {
	Universe    string  `json:"universe"` // sp500 (default), watchlist or custom
	Tickers     string  `json:"tickers"`  // custom: "AAPL, MSFT …"
	MinBusiness float64 `json:"minBusiness"`
	MaxPriced   int     `json:"maxPriced"`
}

// Progress counts results by stage.
type screenProgress struct {
	Total   int `json:"total"`
	Scanned int `json:"scanned"` // fundamentals read (any stage)
	Failed  int `json:"failed"`
	Queued  int `json:"queued"` // waiting for a price
	Priced  int `json:"priced"`
	Below   int `json:"below"`
	Capped  int `json:"capped"`
}

type screenResponse struct {
	Screen   store.Screen         `json:"screen"`
	Running  bool                 `json:"running"`
	Progress screenProgress       `json:"progress"`
	Results  []store.ScreenResult `json:"results"`
}

// POST /api/screens → starts a screen.
func (a *api) startScreen(w http.ResponseWriter, r *http.Request) {
	var req screenRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `body must be JSON like {"universe": "sp500"}`)
		return
	}
	if req.MinBusiness != 0 && (req.MinBusiness < 1 || req.MinBusiness > 5) {
		writeError(w, http.StatusBadRequest, "minBusiness must be between 1 and 5")
		return
	}
	if req.MaxPriced < 0 || req.MaxPriced > 500 {
		writeError(w, http.StatusBadRequest, "maxPriced must be between 0 and 500")
		return
	}

	var members []store.ScreenMember
	switch req.Universe {
	case "", "sp500":
		req.Universe = "sp500"
		members = a.Universe(r.Context())
	case "watchlist":
		if a.Watchlist == nil {
			writeError(w, http.StatusBadRequest, "the watchlist needs the database")
			return
		}
		items, err := a.Watchlist.List(r.Context())
		if err != nil {
			a.Logger.Error("list watchlist", "err", err)
			writeError(w, http.StatusInternalServerError, "could not load the watchlist")
			return
		}
		for _, it := range items {
			members = append(members, store.ScreenMember{Ticker: it.Ticker, Name: it.CompanyName})
		}
	case "custom":
		members = screener.ParseTickers(req.Tickers)
	default:
		writeError(w, http.StatusBadRequest, "universe must be sp500, watchlist or custom")
		return
	}
	if len(members) == 0 {
		writeError(w, http.StatusBadRequest, "no tickers to screen")
		return
	}

	sc, err := a.Screener.Start(r.Context(), req.Universe, members, store.ScreenParams{MinBusiness: req.MinBusiness, MaxPriced: req.MaxPriced})
	if errors.Is(err, screener.ErrBusy) {
		writeError(w, http.StatusConflict, "a screen is already running; cancel it or wait for it to finish")
		return
	}
	if err != nil {
		a.Logger.Error("start screen", "err", err)
		writeError(w, http.StatusInternalServerError, "could not start the screen")
		return
	}
	writeJSON(w, http.StatusAccepted, screenResponse{Screen: sc, Running: true, Progress: screenProgress{Total: sc.Total}, Results: []store.ScreenResult{}})
}

// GET /api/screens/latest and GET /api/screens/{id} → the screen with its
// results, strongest business first.
func (a *api) getScreen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var (
		sc  store.Screen
		err error
	)
	if id := r.PathValue("id"); id != "" {
		sc, err = a.Screens.Get(ctx, id)
	} else {
		sc, err = a.Screens.Latest(ctx)
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no screen yet")
		return
	}
	if err != nil {
		a.Logger.Error("get screen", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load the screen")
		return
	}
	results, err := a.Screens.Results(ctx, sc.ID)
	if err != nil {
		a.Logger.Error("screen results", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load the screen")
		return
	}
	writeJSON(w, http.StatusOK, screenResponse{Screen: sc, Running: a.Screener.Running(sc.ID), Progress: progressOf(sc, results), Results: sorted(results)})
}

// POST /api/screens/{id}/cancel → stops a running screen.
func (a *api) cancelScreen(w http.ResponseWriter, r *http.Request) {
	if !a.Screener.Cancel(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "that screen is not running")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

func progressOf(sc store.Screen, results []store.ScreenResult) screenProgress {
	p := screenProgress{Total: sc.Total, Scanned: len(results)}
	for _, r := range results {
		switch r.Stage {
		case screener.StageFailed:
			p.Failed++
		case screener.StageQueued:
			p.Queued++
		case screener.StagePriced:
			p.Priced++
		case screener.StageBelow:
			p.Below++
		case screener.StageCapped:
			p.Capped++
		}
	}
	return p
}

func sorted(results []store.ScreenResult) []store.ScreenResult {
	b := func(r store.ScreenResult) float64 {
		if r.Business == nil {
			return -1
		}
		return *r.Business
	}
	sort.SliceStable(results, func(i, j int) bool {
		if b(results[i]) != b(results[j]) {
			return b(results[i]) > b(results[j])
		}
		return results[i].Ticker < results[j].Ticker
	})
	return results
}
