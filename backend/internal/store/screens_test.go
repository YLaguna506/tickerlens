package store

import (
	"context"
	"errors"
	"testing"
)

func TestScreens(t *testing.T) {
	pool := openTestDB(t)
	s := NewScreens(pool)
	ctx := t.Context()

	members := []ScreenMember{{Ticker: "AAA", Name: "A Co", Sector: "Utilities"}, {Ticker: "BBB"}}
	sc, err := s.Create(ctx, "custom", ScreenParams{MinBusiness: 3.5, MaxPriced: 10}, members)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM screens WHERE id = $1", sc.ID) })

	b := 4.1
	_ = s.SaveResult(ctx, sc.ID, ScreenResult{Ticker: "AAA", Stage: "scanned", Business: &b})
	_ = s.SaveResult(ctx, sc.ID, ScreenResult{Ticker: "AAA", Stage: "priced", Business: &b, Zone: "cheap"}) // replaces
	_ = s.SaveResult(ctx, sc.ID, ScreenResult{Ticker: "BBB", Stage: "failed", Error: "no filings"})

	got, err := s.Get(ctx, sc.ID)
	if err != nil || got.Status != "scanning" || got.Total != 2 || got.Members[0].Sector != "Utilities" || got.Params.MaxPriced != 10 {
		t.Fatalf("get = %+v, %v", got, err)
	}
	results, err := s.Results(ctx, sc.ID)
	if err != nil || len(results) != 2 {
		t.Fatalf("results = %+v, %v", results, err)
	}
	for _, r := range results {
		if r.Ticker == "AAA" && (r.Stage != "priced" || r.Zone != "cheap") {
			t.Errorf("AAA = %+v", r)
		}
	}

	unfinished, _ := s.Unfinished(ctx)
	if !containsScreen(unfinished, sc.ID) {
		t.Error("a scanning screen should be unfinished")
	}
	if err := s.SetStatus(ctx, sc.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, sc.ID)
	if got.Status != "done" || got.FinishedAt == nil {
		t.Errorf("after done: %+v", got)
	}
	unfinished, _ = s.Unfinished(ctx)
	if containsScreen(unfinished, sc.ID) {
		t.Error("a done screen is not unfinished")
	}
	if latest, err := s.Latest(ctx); err != nil || latest.ID != sc.ID {
		t.Errorf("latest = %+v, %v", latest, err)
	}
	if _, err := s.Get(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing screen: %v", err)
	}
}

func containsScreen(list []Screen, id string) bool {
	for _, sc := range list {
		if sc.ID == id {
			return true
		}
	}
	return false
}
