package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/pipeline"
	"github.com/YLaguna506/tickerlens/backend/internal/screener"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

type fakeScreener struct {
	started []store.ScreenMember
	params  store.ScreenParams
	busy    bool
}

func (f *fakeScreener) Start(_ context.Context, universe string, members []store.ScreenMember, p store.ScreenParams) (store.Screen, error) {
	if f.busy {
		return store.Screen{}, screener.ErrBusy
	}
	f.started, f.params = members, p
	return store.Screen{ID: "s1", Universe: universe, Status: "scanning", Total: len(members)}, nil
}
func (f *fakeScreener) Cancel(id string) bool  { return id == "s1" }
func (f *fakeScreener) Running(id string) bool { return id == "s1" }

type fakeScreens struct{}

func (fakeScreens) Get(context.Context, string) (store.Screen, error) {
	return store.Screen{}, store.ErrNotFound
}
func (fakeScreens) Latest(context.Context) (store.Screen, error) {
	return store.Screen{ID: "s1", Status: "pricing", Total: 3}, nil
}
func (fakeScreens) Results(context.Context, string) ([]store.ScreenResult, error) {
	b1, b2 := 3.2, 4.4
	return []store.ScreenResult{
		{Ticker: "A", Stage: screener.StageBelow, Business: &b1},
		{Ticker: "B", Stage: screener.StagePriced, Business: &b2},
		{Ticker: "C", Stage: screener.StageFailed},
	}, nil
}

func TestScreenRoutes(t *testing.T) {
	scr := &fakeScreener{}
	srv := httptest.NewServer(NewRouter(Deps{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Reports:  &fakeReports{reports: map[string]store.Report{}},
		Jobs:     &fakeJobs{},
		Events:   pipeline.NewBroker(),
		Tickers:  fakeTickers{},
		Screener: scr,
		Screens:  fakeScreens{},
	}))
	defer srv.Close()

	if res, body := do(t, "POST", srv.URL+"/api/screens", `{"universe":"custom","tickers":"aapl, msft brk.b","minBusiness":4}`); res.StatusCode != 202 {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	if len(scr.started) != 3 || scr.started[2].Ticker != "BRK-B" || scr.params.MinBusiness != 4 {
		t.Errorf("started %+v with %+v", scr.started, scr.params)
	}
	// The S&P 500 by default (bundled list without a fetch client).
	if res, _ := do(t, "POST", srv.URL+"/api/screens", `{}`); res.StatusCode != 202 || len(scr.started) < 490 {
		t.Errorf("sp500: %d, %d members", res.StatusCode, len(scr.started))
	}
	if res, _ := do(t, "POST", srv.URL+"/api/screens", `{"minBusiness":9}`); res.StatusCode != 400 {
		t.Errorf("bad minBusiness: %d", res.StatusCode)
	}
	scr.busy = true
	if res, _ := do(t, "POST", srv.URL+"/api/screens", `{}`); res.StatusCode != 409 {
		t.Errorf("busy: %d", res.StatusCode)
	}

	res, body := do(t, "GET", srv.URL+"/api/screens/latest", "")
	if res.StatusCode != 200 {
		t.Fatalf("latest: %d %s", res.StatusCode, body)
	}
	var got screenResponse
	_ = json.Unmarshal(body, &got)
	if !got.Running || got.Progress.Scanned != 3 || got.Progress.Priced != 1 || got.Progress.Failed != 1 || got.Results[0].Ticker != "B" {
		t.Errorf("latest = %+v", got)
	}
	if res, _ := do(t, "GET", srv.URL+"/api/screens/nope", ""); res.StatusCode != 404 {
		t.Errorf("unknown screen: %d", res.StatusCode)
	}
}
