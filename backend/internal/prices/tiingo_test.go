package prices

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

func TestDailyPrices(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "tiingo", "daily_V.json"))
	if err != nil {
		t.Fatal(err)
	}
	var gotAuth, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotQuery = r.Header.Get("Authorization"), r.URL.RawQuery
		switch r.URL.Path {
		case "/tiingo/daily/V/prices":
			_, _ = w.Write(fixture)
		default:
			http.Error(w, `{"detail":"Error: Ticker 'NOPE' not found"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	f := fetch.New(fetch.Options{Name: "tiingo", Header: http.Header{"Authorization": {"Token test-key"}}})
	c := NewTiingo(f, srv.URL)
	start := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)

	bars, err := c.DailyPrices(t.Context(), "V", start)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Token test-key" || gotQuery != "startDate=2026-09-28" {
		t.Errorf("auth=%q query=%q", gotAuth, gotQuery)
	}
	if len(bars) != 10 {
		t.Fatalf("got %d bars, want 10", len(bars))
	}
	first := bars[0]
	if first.Date != "2026-09-28" || first.Close != 367.74 || first.AdjClose != 367.74 {
		t.Errorf("first bar = %+v", first)
	}

	if _, err := c.DailyPrices(t.Context(), "NOPE", start); !errors.Is(err, ErrUnknownTicker) {
		t.Errorf("unknown ticker err = %v, want ErrUnknownTicker", err)
	}
}

func TestIsSplit(t *testing.T) {
	for f, want := range map[float64]bool{
		2: true, 10: true, 1.5: true, 0.5: true, 0.1: true, 1.25: true, 20: true,
		1: false, 0: false, 1.061: false, 0.9535: false, 1.032: false,
	} {
		if got := (Bar{SplitFactor: f}).IsSplit(); got != want {
			t.Errorf("IsSplit(%v) = %v, want %v", f, got, want)
		}
	}
}
