package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClearTicker(t *testing.T) {
	pool := openTestDB(t)
	repo, cache := NewReports(pool), NewPGCache(pool)
	ctx := t.Context()
	stamp := time.Now().Format("150405.000")
	ticker, other := "CLR"+stamp, "KEEP"+stamp
	cik, acc := "000"+strings.ReplaceAll(stamp, ".", ""), "0000000000-26-"+strings.ReplaceAll(stamp, ".", "")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM reports WHERE ticker = ANY($1)", []string{ticker, other})
		_, _ = pool.Exec(context.Background(), "DELETE FROM http_cache WHERE key LIKE '%' || $1 || '%'", strings.ReplaceAll(stamp, ".", ""))
		_, _ = pool.Exec(context.Background(), "DELETE FROM http_cache WHERE key LIKE '%' || $1 || '%'", stamp)
	})

	done, err := repo.Create(ctx, ticker, "Clear Co", Options{})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"company":    map[string]string{"cik": cik},
		"filing":     map[string]string{"accession": acc},
		"financials": map[string]any{"annual": []map[string]string{{"accession": acc}}, "quarterly": []any{}, "ttm": nil},
	})
	if err := repo.SaveBody(ctx, done.ID, body); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(ctx, done.ID, Outcome{Status: StatusDone}); err != nil {
		t.Fatal(err)
	}
	running, _ := repo.Create(ctx, ticker, "Clear Co", Options{}) // still queued: must survive
	kept, _ := repo.Create(ctx, other, "Other Co", Options{})

	cleared := []string{
		"agent:" + ticker + ":" + acc + ":segments:v2:m",
		"sec:sections:v1:" + acc,
		"sec:companyfacts:" + cik,
		"tiingo:daily:" + ticker + ":2015-01-01",
		"alphavantage:earnings:" + ticker,
	}
	keptKeys := []string{"agent:" + other + ":x:segments:v2:m", "tiingo:daily:" + other + ":2015-01-01"}
	for _, k := range append(append([]string{}, cleared...), keptKeys...) {
		if err := cache.Set(ctx, k, []byte("{}"), time.Hour); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.ClearTicker(ctx, strings.ToLower(ticker))
	if err != nil {
		t.Fatal(err)
	}
	if got.Reports != 1 || got.CacheEntries != int64(len(cleared)) {
		t.Errorf("cleared %+v, want 1 report and %d cache entries", got, len(cleared))
	}
	for _, k := range cleared {
		if _, ok, _ := cache.Get(ctx, k); ok {
			t.Errorf("%s still cached", k)
		}
	}
	for _, k := range keptKeys {
		if _, ok, _ := cache.Get(ctx, k); !ok {
			t.Errorf("%s was removed", k)
		}
	}
	if _, err := repo.Get(ctx, done.ID); err == nil {
		t.Error("finished report still exists")
	}
	for _, id := range []string{running.ID, kept.ID} {
		if _, err := repo.Get(ctx, id); err != nil {
			t.Errorf("report %s was removed: %v", id, err)
		}
	}
}
