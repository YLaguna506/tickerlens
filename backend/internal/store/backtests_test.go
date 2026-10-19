package store

import (
	"context"
	"testing"
)

func TestBacktests(t *testing.T) {
	pool := openTestDB(t)
	s := NewBacktests(pool)
	ctx := t.Context()
	bt, err := s.Create(ctx, "custom", BacktestParams{Dates: []string{"2020-06-30"}, Benchmark: "SPY"}, []ScreenMember{{Ticker: "AAA"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM backtests WHERE id = $1", bt.ID) })

	x := 0.1
	if err := s.SaveResult(ctx, bt.ID, BacktestResult{Ticker: "AAA", Observations: []Observation{{Ticker: "AAA", Date: "2020-06-30", Label: "Buy", Excess: [3]*float64{&x}}}}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Results(ctx, bt.ID)
	if err != nil || len(res) != 1 || *res[0].Observations[0].Excess[0] != 0.1 {
		t.Fatalf("results = %+v, %v", res, err)
	}
	if un, _ := s.Unfinished(ctx); len(un) == 0 {
		t.Error("a running backtest is unfinished")
	}
	_ = s.SetStatus(ctx, bt.ID, "done", "")
	got, err := s.Get(ctx, bt.ID)
	if err != nil || got.Status != "done" || got.FinishedAt == nil || got.Params.Dates[0] != "2020-06-30" {
		t.Errorf("get = %+v, %v", got, err)
	}
}
