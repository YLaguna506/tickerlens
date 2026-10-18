package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
	"github.com/YLaguna506/tickerlens/backend/internal/pipeline"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

type fakeWatchlist struct {
	mu    sync.Mutex
	items []store.WatchItem
}

func (f *fakeWatchlist) Add(_ context.Context, ticker, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.ContainsFunc(f.items, func(it store.WatchItem) bool { return it.Ticker == ticker }) {
		f.items = append(f.items, store.WatchItem{Ticker: ticker, CompanyName: name})
	}
	return nil
}

func (f *fakeWatchlist) Remove(_ context.Context, ticker string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.items)
	f.items = slices.DeleteFunc(f.items, func(it store.WatchItem) bool { return it.Ticker == ticker })
	return len(f.items) < n, nil
}

func (f *fakeWatchlist) Contains(_ context.Context, ticker string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.ContainsFunc(f.items, func(it store.WatchItem) bool { return it.Ticker == ticker }), nil
}

func (f *fakeWatchlist) List(context.Context) ([]store.WatchItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.WatchItem{}, f.items...), nil
}

func newWatchAPI(t *testing.T) (*httptest.Server, *fakeWatchlist, *fakeJobs) {
	t.Helper()
	wl, jobs := &fakeWatchlist{}, &fakeJobs{}
	srv := httptest.NewServer(NewRouter(Deps{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Reports:   &fakeReports{reports: map[string]store.Report{}},
		Jobs:      jobs,
		Events:    pipeline.NewBroker(),
		Tickers:   fakeTickers{},
		Watchlist: wl,
	}))
	t.Cleanup(srv.Close)
	return srv, wl, jobs
}

func TestWatchlistAddStatusRemove(t *testing.T) {
	srv, wl, _ := newWatchAPI(t)

	if res, body := do(t, "POST", srv.URL+"/api/watchlist", `{"ticker":"visa"}`); res.StatusCode != 404 {
		t.Errorf("unknown ticker: %d %s", res.StatusCode, body)
	}
	if res, body := do(t, "POST", srv.URL+"/api/watchlist", `{"ticker":"v"}`); res.StatusCode != 201 {
		t.Fatalf("add: %d %s", res.StatusCode, body)
	}
	if wl.items[0].Ticker != "V" || wl.items[0].CompanyName != "VISA INC." {
		t.Errorf("stored %+v", wl.items[0])
	}
	_, body := do(t, "GET", srv.URL+"/api/watchlist/V", "")
	var st watchState
	if json.Unmarshal(body, &st) != nil || !st.Watching {
		t.Errorf("status = %s", body)
	}
	if res, _ := do(t, "DELETE", srv.URL+"/api/watchlist/V", ""); res.StatusCode != 200 {
		t.Errorf("remove: %d", res.StatusCode)
	}
	if res, _ := do(t, "DELETE", srv.URL+"/api/watchlist/V", ""); res.StatusCode != 404 {
		t.Errorf("remove twice: %d", res.StatusCode)
	}
}

func TestWatchlistRefreshStartsStaleReports(t *testing.T) {
	srv, wl, jobs := newWatchAPI(t)
	old := &store.WatchSummary{CreatedAt: time.Now().Add(-48 * time.Hour)}
	fresh := &store.WatchSummary{CreatedAt: time.Now().Add(-time.Hour)}
	wl.items = []store.WatchItem{
		{Ticker: "FRESH", Latest: fresh},
		{Ticker: "BUSY", Latest: old, Running: &store.RunningReport{ID: "r1"}},
		{Ticker: "NEW"}, // never reported
	}
	for _, tk := range []string{"A", "B", "C", "D", "E"} {
		wl.items = append(wl.items, store.WatchItem{Ticker: tk, Latest: old})
	}

	res, body := do(t, "POST", srv.URL+"/api/watchlist/refresh", "")
	if res.StatusCode != 200 {
		t.Fatalf("refresh: %d %s", res.StatusCode, body)
	}
	var got refreshResponse
	_ = json.Unmarshal(body, &got)
	// Fresh and running tickers are skipped; at most five start.
	if want := []string{"NEW", "A", "B", "C", "D"}; !slices.Equal(got.Started, want) || !slices.Equal(jobs.list(), want) {
		t.Errorf("started %v (jobs %v), want %v", got.Started, jobs.list(), want)
	}
	if !slices.Equal(got.Waiting, []string{"E"}) {
		t.Errorf("waiting = %v", got.Waiting)
	}
}

type fakeCalendar map[string]estimates.CalendarEntry

func (f fakeCalendar) Calendar(context.Context) (map[string]estimates.CalendarEntry, error) {
	return f, nil
}

func TestWatchlistTakesCalendarDates(t *testing.T) {
	future := time.Now().AddDate(0, 0, 9).Format(time.DateOnly)
	past := time.Now().AddDate(0, 0, -2).Format(time.DateOnly)
	a := &api{Deps: Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Calendar: fakeCalendar{
		"KO":  {ReportDate: future, Time: "pre-market"},
		"PEP": {ReportDate: past}, // already reported: keep the report's date
	}}}
	reportDate := &store.NextEarnings{Date: "2027-02-10", Source: "estimated"}
	items := []store.WatchItem{{Ticker: "KO"}, {Ticker: "PEP", NextEarnings: reportDate}, {Ticker: "V"}}
	a.withCalendar(context.Background(), items)
	if n := items[0].NextEarnings; n == nil || n.Date != future || n.Source != "calendar" || n.Time != "pre-market" {
		t.Errorf("KO = %+v", n)
	}
	if items[1].NextEarnings != reportDate || items[2].NextEarnings != nil {
		t.Errorf("PEP = %+v, V = %+v", items[1].NextEarnings, items[2].NextEarnings)
	}
}
