package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestReportLifecycle(t *testing.T) {
	pool := openTestDB(t)
	repo := NewReports(pool)
	ctx := t.Context()
	ticker := "TEST" + time.Now().Format("150405.000")
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM reports WHERE ticker = $1", ticker) })

	r, err := repo.Create(ctx, ticker, "Test Co", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusQueued || r.ID == "" {
		t.Fatalf("created = %+v", r)
	}

	if err := repo.SetStage(ctx, r.ID, "ingesting"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveBody(ctx, r.ID, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetCacheKey(ctx, r.ID, "acc-1|2026-10-14"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(ctx, r.ID, Outcome{Status: StatusPartial, FailedSections: []string{"prices"}}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Get(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPartial || !got.Finished() || len(got.FailedSections) != 1 || got.FinishedAt == nil {
		t.Errorf("finished report = %+v", got)
	}
	var body map[string]bool
	if err := json.Unmarshal(got.Body, &body); err != nil || !body["ok"] {
		t.Errorf("body = %s", got.Body)
	}

	// Only complete ("done") reports are reused by cache key.
	if _, err := repo.FindByCacheKey(ctx, ticker, "acc-1|2026-10-14", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("partial report must not be reused: %v", err)
	}
	if got, err := repo.LatestFinished(ctx, ticker, Options{}, time.Now().Add(-time.Hour)); err != nil || got.Lang != "en" {
		t.Errorf("latest finished: %+v, %v", got, err)
	}
	// A report in another language or built with another model is not reused.
	for _, opt := range []Options{{Lang: "es"}, {Model: "gemini:gemini-3.5-flash"}} {
		if _, err := repo.LatestFinished(ctx, ticker, opt, time.Now().Add(-time.Hour)); !errors.Is(err, ErrNotFound) {
			t.Errorf("latest finished for %+v: %v, want not found", opt, err)
		}
	}

	list, err := repo.ListByTicker(ctx, ticker, 10)
	if err != nil || len(list) != 1 || list[0].Body != nil {
		t.Errorf("list = %+v, %v (bodies must be left out)", list, err)
	}
}

func TestGetUnknownOrMalformedID(t *testing.T) {
	repo := NewReports(openTestDB(t))
	for _, id := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if _, err := repo.Get(t.Context(), id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) err = %v, want ErrNotFound", id, err)
		}
	}
}

func TestMarkInterrupted(t *testing.T) {
	pool := openTestDB(t)
	repo := NewReports(pool)
	ticker := "INT" + time.Now().Format("150405.000")
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM reports WHERE ticker = $1", ticker) })

	running, _ := repo.Create(t.Context(), ticker, "", Options{})
	_ = repo.SetStage(t.Context(), running.ID, "ingesting")
	done, _ := repo.Create(t.Context(), ticker, "", Options{})
	_ = repo.Finish(t.Context(), done.ID, Outcome{Status: StatusDone})

	if _, err := repo.MarkInterrupted(t.Context()); err != nil {
		t.Fatal(err)
	}
	r1, _ := repo.Get(t.Context(), running.ID)
	r2, _ := repo.Get(t.Context(), done.ID)
	if r1.Status != StatusFailed || r1.Error == "" {
		t.Errorf("running report should be failed with a reason, got %+v", r1)
	}
	if r2.Status != StatusDone {
		t.Errorf("finished report must be untouched, got %s", r2.Status)
	}
}
