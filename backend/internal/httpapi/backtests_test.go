package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/pipeline"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

type fakeBacktester struct{ started []store.ScreenMember }

func (f *fakeBacktester) Start(_ context.Context, u string, m []store.ScreenMember) (store.Backtest, error) {
	f.started = m
	return store.Backtest{ID: "b1", Universe: u, Status: "running", Total: len(m)}, nil
}
func (f *fakeBacktester) Cancel(id string) bool  { return id == "b1" }
func (f *fakeBacktester) Running(id string) bool { return false }

type fakeBacktests struct{}

func (fakeBacktests) Get(context.Context, string) (store.Backtest, error) {
	return store.Backtest{}, store.ErrNotFound
}
func (fakeBacktests) Latest(context.Context) (store.Backtest, error) {
	return store.Backtest{ID: "b1", Status: "done", Total: 2}, nil
}
func (fakeBacktests) Results(context.Context, string) ([]store.BacktestResult, error) {
	x, v := 0.05, 4.0
	return []store.BacktestResult{
		{Ticker: "AAA", Observations: []store.Observation{{Ticker: "AAA", Date: "2020-06-30", Label: "Buy", Valuation: &v, Business: &v, Excess: [3]*float64{&x}}}},
		{Ticker: "BBB", Error: "unknown ticker"},
	}, nil
}

func TestBacktestRoutes(t *testing.T) {
	bt := &fakeBacktester{}
	srv := httptest.NewServer(NewRouter(Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Reports: &fakeReports{reports: map[string]store.Report{}},
		Jobs: &fakeJobs{}, Events: pipeline.NewBroker(), Tickers: fakeTickers{},
		Backtester: bt, Backtests: fakeBacktests{},
	}))
	defer srv.Close()

	if res, body := do(t, "POST", srv.URL+"/api/backtests", `{"sampleSize":40}`); res.StatusCode != 202 || len(bt.started) != 40 {
		t.Fatalf("start: %d %s (%d members)", res.StatusCode, body, len(bt.started))
	}
	res, body := do(t, "GET", srv.URL+"/api/backtests/latest", "")
	var got backtestResponse
	if res.StatusCode != 200 || json.Unmarshal(body, &got) != nil {
		t.Fatalf("latest: %d %s", res.StatusCode, body)
	}
	if got.Progress.Done != 2 || got.Progress.Failed != 1 || got.Summary == nil || got.Summary.Observations != 1 || got.Failures[0].Ticker != "BBB" {
		t.Errorf("latest = %+v", got)
	}
	res, body = do(t, "GET", srv.URL+"/api/backtests/latest/observations.csv", "")
	lines := strings.Fields(string(body))
	if res.StatusCode != 200 || !strings.HasPrefix(lines[0], "date,ticker,") || len(lines) != 2 || !strings.HasPrefix(lines[1], "2020-06-30,AAA,") {
		t.Errorf("csv: %d %s", res.StatusCode, body)
	}
}
