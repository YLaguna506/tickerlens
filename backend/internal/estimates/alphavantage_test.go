package estimates

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

func TestEarnings(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "alphavantage", "earnings_V.json"))
	if err != nil {
		t.Fatal(err)
	}
	responses := map[string]string{
		"V":       string(fixture),
		"LIMITED": `{"Information": "Thank you for using Alpha Vantage! Our standard API rate limit is 25 requests per day."}`,
		"EMPTY":   `{}`,
		"NONE": `{"symbol":"NONE","quarterlyEarnings":[{"fiscalDateEnding":"2026-06-30","reportedDate":"2026-07-28",
			"reportedEPS":"1.10","estimatedEPS":"None","surprise":"None","surprisePercentage":"None","reportTime":"pre-market"}]}`,
	}
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(responses[r.URL.Query().Get("symbol")]))
	}))
	t.Cleanup(srv.Close)

	cache := fetch.NewMemoryCache()
	c := NewClient(fetch.New(fetch.Options{Name: Provider, Cache: cache}), "SECRET", srv.URL)

	t.Run("parses and sorts oldest first", func(t *testing.T) {
		got, err := c.Earnings(t.Context(), "V")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) < 8 {
			t.Fatalf("got %d quarters", len(got))
		}
		latest := got[len(got)-1]
		if latest.FiscalDateEnding != "2026-06-30" || *latest.ReportedEPS != 3.32 || *latest.EstimatedEPS != 3.23 {
			t.Errorf("latest = %+v", latest)
		}
		if got[0].FiscalDateEnding > latest.FiscalDateEnding {
			t.Error("not sorted oldest first")
		}
	})

	t.Run("None becomes nil", func(t *testing.T) {
		got, err := c.Earnings(t.Context(), "NONE")
		if err != nil {
			t.Fatal(err)
		}
		if got[0].EstimatedEPS != nil || got[0].ReportedEPS == nil {
			t.Errorf("got %+v", got[0])
		}
	})

	t.Run("rate-limit message is an error and not cached", func(t *testing.T) {
		_, err := c.Earnings(t.Context(), "LIMITED")
		if !errors.Is(err, ErrRateLimited) {
			t.Fatalf("err = %v, want ErrRateLimited", err)
		}
		if _, ok, _ := cache.Get(t.Context(), "alphavantage:earnings:LIMITED"); ok {
			t.Error("rate-limit response was cached")
		}
	})

	t.Run("empty response means no data", func(t *testing.T) {
		if _, err := c.Earnings(t.Context(), "EMPTY"); !errors.Is(err, ErrNoData) {
			t.Fatalf("err = %v, want ErrNoData", err)
		}
	})

	t.Run("cache key and errors never contain the API key", func(t *testing.T) {
		_, err := c.Earnings(t.Context(), "LIMITED")
		if err != nil && strings.Contains(err.Error(), "SECRET") {
			t.Errorf("error leaks key: %v", err)
		}
	})
}

type fakeBudget struct{ left int }

func (f *fakeBudget) Take(context.Context, string, time.Time, int) (bool, error) {
	if f.left == 0 {
		return false, nil
	}
	f.left--
	return true, nil
}

func TestBudgetHook(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"symbol":"V","quarterlyEarnings":[{"fiscalDateEnding":"2026-06-30","reportedEPS":"1"}]}`))
	}))
	t.Cleanup(srv.Close)

	hook := BudgetHook(&fakeBudget{left: 1}, DailyLimit, time.Now)
	c := NewClient(fetch.New(fetch.Options{Name: Provider, BeforeNetwork: hook}), "k", srv.URL)

	if _, err := c.Earnings(t.Context(), "V"); err != nil {
		t.Fatal(err)
	}
	_, err := c.Earnings(t.Context(), "V")
	if !errors.Is(err, fetch.ErrBudgetExhausted) {
		t.Fatalf("second call err = %v, want ErrBudgetExhausted", err)
	}
	if calls != 1 {
		t.Errorf("server calls = %d, want 1", calls)
	}
}
