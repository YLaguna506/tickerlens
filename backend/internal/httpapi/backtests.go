package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/YLaguna506/tickerlens/backend/internal/backtest"
	"github.com/YLaguna506/tickerlens/backend/internal/screener"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// Backtester runs backtests in the background (backtest.Runner).
type Backtester interface {
	Start(ctx context.Context, universe string, members []store.ScreenMember) (store.Backtest, error)
	Cancel(id string) bool
	Running(id string) bool
}

// BacktestStore reads backtests and their results (store.Backtests).
type BacktestStore interface {
	Get(ctx context.Context, id string) (store.Backtest, error)
	Latest(ctx context.Context) (store.Backtest, error)
	Results(ctx context.Context, id string) ([]store.BacktestResult, error)
}

type backtestRequest struct {
	Universe   string `json:"universe"`   // sp500 (default), watchlist or custom
	SampleSize int    `json:"sampleSize"` // sp500: companies drawn (default 100)
	Tickers    string `json:"tickers"`    // custom
}

type backtestProgress struct {
	Total        int `json:"total"`
	Done         int `json:"done"`
	Failed       int `json:"failed"`
	Observations int `json:"observations"`
}

type backtestFailure struct {
	Ticker string `json:"ticker"`
	Error  string `json:"error"`
}

type backtestResponse struct {
	Backtest store.Backtest    `json:"backtest"`
	Running  bool              `json:"running"`
	Progress backtestProgress  `json:"progress"`
	Summary  *backtest.Summary `json:"summary"` // nil before the first observation
	Failures []backtestFailure `json:"failures"`
}

// defaultSample is how many S&P 500 companies a backtest draws: enough for
// the statistics, within the price feed's 500 symbols a month.
const defaultSample = 100

// POST /api/backtests → starts a backtest.
func (a *api) startBacktest(w http.ResponseWriter, r *http.Request) {
	var req backtestRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `body must be JSON like {"universe": "sp500", "sampleSize": 100}`)
		return
	}
	if req.SampleSize < 0 || req.SampleSize > 500 {
		writeError(w, http.StatusBadRequest, "sampleSize must be between 1 and 500")
		return
	}
	var members []store.ScreenMember
	switch req.Universe {
	case "", "sp500":
		req.Universe = "sp500"
		n := req.SampleSize
		if n == 0 {
			n = defaultSample
		}
		members = backtest.Sample(a.Universe(r.Context()), n)
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
		writeError(w, http.StatusBadRequest, "no tickers to test")
		return
	}
	bt, err := a.Backtester.Start(r.Context(), req.Universe, members)
	if errors.Is(err, backtest.ErrBusy) {
		writeError(w, http.StatusConflict, "a backtest is already running; cancel it or wait for it to finish")
		return
	}
	if err != nil {
		a.Logger.Error("start backtest", "err", err)
		writeError(w, http.StatusInternalServerError, "could not start the backtest")
		return
	}
	writeJSON(w, http.StatusAccepted, backtestResponse{Backtest: bt, Running: true, Progress: backtestProgress{Total: bt.Total}, Failures: []backtestFailure{}})
}

// GET /api/backtests/latest and /api/backtests/{id} → progress and summary.
func (a *api) getBacktest(w http.ResponseWriter, r *http.Request) {
	bt, results, ok := a.loadBacktest(w, r)
	if !ok {
		return
	}
	resp := backtestResponse{Backtest: bt, Running: a.Backtester.Running(bt.ID), Progress: backtestProgress{Total: bt.Total, Done: len(results)}, Failures: []backtestFailure{}}
	var obs []store.Observation
	for _, res := range results {
		if res.Error != "" {
			resp.Progress.Failed++
			resp.Failures = append(resp.Failures, backtestFailure{Ticker: res.Ticker, Error: res.Error})
		}
		obs = append(obs, res.Observations...)
	}
	sort.Slice(resp.Failures, func(i, j int) bool { return resp.Failures[i].Ticker < resp.Failures[j].Ticker })
	resp.Progress.Observations = len(obs)
	if len(obs) > 0 {
		s := backtest.Summarize(obs)
		resp.Summary = &s
	}
	writeJSON(w, http.StatusOK, resp)
}

// GET /api/backtests/{id}/observations.csv → every observation, for a
// spreadsheet.
func (a *api) backtestCSV(w http.ResponseWriter, r *http.Request) {
	bt, results, ok := a.loadBacktest(w, r)
	if !ok {
		return
	}
	var obs []store.Observation
	for _, res := range results {
		obs = append(obs, res.Observations...)
	}
	sort.Slice(obs, func(i, j int) bool {
		if obs[i].Date != obs[j].Date {
			return obs[i].Date < obs[j].Date
		}
		return obs[i].Ticker < obs[j].Ticker
	})
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="tickerlens-backtest-%s.csv"`, bt.CreatedAt.Format("2006-01-02")))
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"date", "ticker", "sector", "kind", "verdict", "business", "growth", "health", "valuation", "percentile", "zone", "metric",
		"expectations", "price", "return_1y", "return_2y", "return_3y", "excess_1y", "excess_2y", "excess_3y"})
	num := func(v *float64) string {
		if v == nil {
			return ""
		}
		return strconv.FormatFloat(*v, 'f', 4, 64)
	}
	for _, o := range obs {
		_ = cw.Write([]string{o.Date, o.Ticker, o.Sector, o.Kind, o.Label, num(o.Business), num(o.Growth), num(o.Health), num(o.Valuation),
			num(o.Percentile), o.Zone, o.Metric, o.Expectations, strconv.FormatFloat(o.Price, 'f', 2, 64),
			num(o.Return[0]), num(o.Return[1]), num(o.Return[2]), num(o.Excess[0]), num(o.Excess[1]), num(o.Excess[2])})
	}
	cw.Flush()
}

// POST /api/backtests/{id}/cancel → stops a running backtest.
func (a *api) cancelBacktest(w http.ResponseWriter, r *http.Request) {
	if !a.Backtester.Cancel(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "that backtest is not running")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
}

func (a *api) loadBacktest(w http.ResponseWriter, r *http.Request) (store.Backtest, []store.BacktestResult, bool) {
	ctx := r.Context()
	var (
		bt  store.Backtest
		err error
	)
	if id := r.PathValue("id"); id != "" && id != "latest" {
		bt, err = a.Backtests.Get(ctx, id)
	} else {
		bt, err = a.Backtests.Latest(ctx)
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no backtest yet")
		return bt, nil, false
	}
	if err != nil {
		a.Logger.Error("get backtest", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load the backtest")
		return bt, nil, false
	}
	results, err := a.Backtests.Results(ctx, bt.ID)
	if err != nil {
		a.Logger.Error("backtest results", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load the backtest")
		return bt, nil, false
	}
	return bt, results, true
}
