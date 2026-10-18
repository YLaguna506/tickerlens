package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWatchlist(t *testing.T) {
	pool := openTestDB(t)
	w, reports := NewWatchlist(pool), NewReports(pool)
	ctx := t.Context()
	ticker := "W" + time.Now().Format("150405.000")
	t.Cleanup(func() {
		_, _ = pool.Exec(t.Context(), "DELETE FROM watchlist WHERE ticker = $1", ticker)
		_, _ = pool.Exec(t.Context(), "DELETE FROM reports WHERE ticker = $1", ticker)
	})

	if err := w.Add(ctx, ticker, "Watch Co"); err != nil {
		t.Fatal(err)
	}
	if err := w.Add(ctx, ticker, "Watch Co"); err != nil { // adding twice is fine
		t.Fatal(err)
	}
	item := find(t, w, ticker)
	if item.Latest != nil || item.Running != nil {
		t.Errorf("before any report: %+v", item)
	}

	// A finished report fills the summary; a running one shows as running.
	done, _ := reports.Create(ctx, ticker, "Watch Co", Options{})
	body := `{"verdict":{"label":"Buy","business":4.2,"valuation":4},
		"metrics":{"price":131.49,"priceDate":"2026-10-16","phase":{"name":"Capital return"},
		  "valuationBands":{"metric":"P/E","zone":"cheap","percentile":0.05,"current":16.5,"cheapPrice":186.5,"medianPrice":207,"expensivePrice":223.3}},
		"moat":{"widthName":"Wide"},"risk":{"score":3}}`
	_ = reports.SaveBody(ctx, done.ID, json.RawMessage(body))
	_ = reports.Finish(ctx, done.ID, Outcome{Status: StatusDone})
	running, _ := reports.Create(ctx, ticker, "Watch Co", Options{})
	_ = reports.SetStage(ctx, running.ID, "analyzing")

	item = find(t, w, ticker)
	s := item.Latest
	if s == nil || s.ReportID != done.ID || s.Verdict != "Buy" || *s.Business != 4.2 || *s.Price != 131.49 || s.Moat != "Wide" || *s.RiskScore != 3 {
		t.Fatalf("summary = %+v", s)
	}
	if s.Bands == nil || s.Bands.Zone != "cheap" || s.Bands.MedianPrice != 207 {
		t.Errorf("bands = %+v", s.Bands)
	}
	if item.Running == nil || item.Running.ID != running.ID || item.Running.Stage != "analyzing" {
		t.Errorf("running = %+v", item.Running)
	}

	if ok, err := w.Contains(ctx, ticker); err != nil || !ok {
		t.Errorf("contains = %v, %v", ok, err)
	}
	if removed, err := w.Remove(ctx, ticker); err != nil || !removed {
		t.Errorf("remove = %v, %v", removed, err)
	}
	if removed, _ := w.Remove(ctx, ticker); removed {
		t.Error("removing twice should report false")
	}
}

func find(t *testing.T, w *Watchlist, ticker string) WatchItem {
	t.Helper()
	items, err := w.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Ticker == ticker {
			return it
		}
	}
	t.Fatalf("%s not in the watchlist", ticker)
	return WatchItem{}
}
