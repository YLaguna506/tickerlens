package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

func load(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func visa(t *testing.T) Company {
	t.Helper()
	var facts edgar.CompanyFacts
	load(t, "edgar/companyfacts_V.json", &facts)
	var class map[string][]edgar.Fact
	load(t, "edgar/classfacts_V.json", &class)
	edgar.MergeClassFacts(&facts, class["eps"], class["shares"])
	var bars []prices.Bar
	load(t, "tiingo/prices_V.json", &bars)
	return Company{Company: edgar.Company{Ticker: "V", CIK: "0001403161", Name: "VISA INC."}, Facts: &facts, Bars: bars}
}

func TestDates(t *testing.T) {
	d := Dates("2026-10-11")
	if d[0] != "2016-06-30" || d[len(d)-1] != "2025-06-30" || len(d) != 19 {
		t.Errorf("dates = %v", d)
	}
}

// A fact filed after the date is invisible, including a restatement of an
// earlier period: the date sees the figure as first filed.
func TestAsOfUsesFactsAsFiled(t *testing.T) {
	cf := &edgar.CompanyFacts{Facts: map[string]map[string]edgar.Concept{"us-gaap": {
		"Revenues": {Units: map[string][]edgar.Fact{"USD": {
			{Start: "2019-01-01", End: "2019-12-31", Val: 100, Filed: "2020-02-15"},
			{Start: "2019-01-01", End: "2019-12-31", Val: 90, Filed: "2021-02-15"}, // restated later
			{Start: "2020-01-01", End: "2020-12-31", Val: 120, Filed: "2021-02-15"},
		}}},
		"Future": {Units: map[string][]edgar.Fact{"USD": {{End: "2022-12-31", Val: 1, Filed: "2023-02-15"}}}},
	}}}
	got := AsOf(cf, "2020-06-30").Facts["us-gaap"]
	if facts := got["Revenues"].Units["USD"]; len(facts) != 1 || facts[0].Val != 100 {
		t.Errorf("revenue as of mid-2020 = %+v", facts)
	}
	if _, ok := got["Future"]; ok {
		t.Error("a concept first filed later should not exist yet")
	}
}

func TestForward(t *testing.T) {
	bars := []prices.Bar{
		{Date: "2020-06-29", AdjClose: 99},
		{Date: "2020-06-30", AdjClose: 100},
		{Date: "2021-06-28", AdjClose: 105},
		{Date: "2021-07-01", AdjClose: 120}, // first close on or after 2021-06-30
		{Date: "2022-06-30", AdjClose: 90},
	}
	if r, ok := forward(bars, "2020-06-30", 1); !ok || math.Abs(r-0.20) > 1e-9 {
		t.Errorf("1y = %v %v", r, ok)
	}
	if r, ok := forward(bars, "2020-06-30", 2); !ok || math.Abs(r+0.10) > 1e-9 {
		t.Errorf("2y = %v %v", r, ok)
	}
	if _, ok := forward(bars, "2020-06-30", 3); ok {
		t.Error("3y is not known yet")
	}
}

// The verdict as of a date cannot see the future: changing every price and
// filing after the date changes only the forward returns.
func TestEvaluateHasNoLookahead(t *testing.T) {
	c := visa(t)
	bench := c.Bars
	dates := []string{"2021-06-30"}
	base := Evaluate(c, bench, dates, scoring.Default())
	if len(base) != 1 || base[0].Label == "" || base[0].Valuation == nil || base[0].Return[0] == nil {
		t.Fatalf("observation = %+v", base)
	}

	// Rewrite the future: prices after the date tripled, a wild revenue
	// restatement filed later.
	future := c
	future.Bars = slices.Clone(c.Bars)
	for i := range future.Bars {
		if future.Bars[i].Date > dates[0] {
			future.Bars[i].Close *= 3
			future.Bars[i].AdjClose *= 3
		}
	}
	raw, _ := json.Marshal(c.Facts)
	var facts edgar.CompanyFacts
	_ = json.Unmarshal(raw, &facts)
	rev := facts.Facts["us-gaap"]["Revenues"]
	rev.Units["USD"] = append(rev.Units["USD"], edgar.Fact{Start: "2019-10-01", End: "2020-09-30", Val: 1e15, Filed: "2025-11-01", Form: "10-K", FP: "FY"})
	facts.Facts["us-gaap"]["Revenues"] = rev
	future.Facts = &facts

	got := Evaluate(future, bench, dates, scoring.Default())
	a, b := base[0], got[0]
	if a.Label != b.Label || *a.Business != *b.Business || *a.Valuation != *b.Valuation || *a.Percentile != *b.Percentile || a.Price != b.Price {
		t.Errorf("the past changed:\n before %+v\n after  %+v", a, b)
	}
	if *b.Return[0] <= *a.Return[0] {
		t.Errorf("forward return should reflect the tripled prices: %v vs %v", *b.Return[0], *a.Return[0])
	}
}

func TestSummarize(t *testing.T) {
	var obs []store.Observation
	f := func(v float64) *float64 { return &v }
	// Two dates, twelve stocks each: a higher V did better.
	for _, d := range []string{"2020-06-30", "2020-12-31", "2021-06-30"} {
		for i := range 12 {
			v := float64(i%5 + 1)
			x := (v - 3) / 10
			label := "Hold"
			if v >= 4 {
				label = "Buy"
			}
			obs = append(obs, store.Observation{
				Ticker: string(rune('A' + i)), Date: d, Label: label, Valuation: f(v), Business: f(3.5), Growth: f(3), Health: f(4),
				Return: [3]*float64{f(x + 0.1), nil, nil}, Excess: [3]*float64{f(x), nil, nil},
			})
		}
	}
	s := Summarize(obs)
	if s.Observations != 36 || s.Companies != 12 || len(s.Dates) != 3 {
		t.Errorf("counts = %d obs, %d companies, %v", s.Observations, s.Companies, s.Dates)
	}
	if s.ByLabel[0].Key != "Buy" || *s.ByLabel[0].MeanExcess[0] <= *s.ByLabel[1].MeanExcess[0] {
		t.Errorf("by label = %+v", s.ByLabel)
	}
	if s.ByValuation[0].Key != "5" || math.Abs(*s.ByValuation[0].MeanExcess[0]-0.2) > 1e-9 || *s.ByValuation[0].HitRate[0] != 1 {
		t.Errorf("V=5 bucket = %+v", s.ByValuation[0])
	}
	for _, sig := range s.Signals {
		switch sig.Input {
		case "valuation":
			if sig.IC[0] == nil || *sig.IC[0] < 0.99 || sig.Verdict[0] != "helps" {
				t.Errorf("valuation signal = %+v", sig)
			}
		case "growth":
			if sig.Verdict[0] != "too little data" { // no spread: correlation undefined
				t.Errorf("growth signal = %+v", sig)
			}
		}
		if sig.Verdict[1] != "too little data" {
			t.Errorf("%s 2y = %s", sig.Input, sig.Verdict[1])
		}
	}
}

func TestSpearmanTies(t *testing.T) {
	if r, ok := spearman([]float64{1, 2, 2, 3}, []float64{10, 20, 20, 30}); !ok || math.Abs(r-1) > 1e-12 {
		t.Errorf("perfect with ties = %v", r)
	}
	if r, _ := spearman([]float64{1, 2, 3, 4}, []float64{4, 3, 2, 1}); math.Abs(r+1) > 1e-12 {
		t.Errorf("reversed = %v", r)
	}
}

func TestSample(t *testing.T) {
	var all []store.ScreenMember
	for _, tk := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
		all = append(all, store.ScreenMember{Ticker: tk})
	}
	a, b := Sample(all, 3), Sample(slices.Clone(all), 3)
	if len(a) != 3 || !slices.Equal(a, b) {
		t.Errorf("sample = %v vs %v", a, b)
	}
}

// --- Runner ---------------------------------------------------------------------

type fakeSource struct{ t *testing.T }

func (f fakeSource) Company(_ context.Context, ticker string) (edgar.Company, error) {
	if ticker != "V" {
		return edgar.Company{}, errors.New("unknown ticker " + ticker)
	}
	return edgar.Company{Ticker: "V", CIK: "0001403161", Name: "VISA INC."}, nil
}
func (f fakeSource) Facts(context.Context, string) (*edgar.CompanyFacts, error) {
	return visa(f.t).Facts, nil
}
func (f fakeSource) Profile(context.Context, string) (edgar.Profile, error) {
	return edgar.Profile{SIC: "7389"}, nil
}
func (f fakeSource) History(context.Context, string) ([]prices.Bar, error) {
	return visa(f.t).Bars, nil
}

type memStore struct {
	mu      sync.Mutex
	status  map[string]string
	results map[string][]store.BacktestResult
}

func (m *memStore) Create(_ context.Context, u string, p store.BacktestParams, members []store.ScreenMember) (store.Backtest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = map[string]string{"b1": "running"}
	return store.Backtest{ID: "b1", Universe: u, Status: "running", Params: p, Members: members, Total: len(members)}, nil
}
func (m *memStore) Unfinished(context.Context) ([]store.Backtest, error) { return nil, nil }
func (m *memStore) SetStatus(_ context.Context, id, status, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status[id] = status
	return nil
}
func (m *memStore) SaveResult(_ context.Context, id string, r store.BacktestResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.results == nil {
		m.results = map[string][]store.BacktestResult{}
	}
	m.results[id] = append(m.results[id], r)
	return nil
}
func (m *memStore) Results(_ context.Context, id string) ([]store.BacktestResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.results[id]), nil
}

func TestRunner(t *testing.T) {
	st := &memStore{}
	r := &Runner{Source: fakeSource{t}, Store: st, Config: scoring.Default(), Workers: 2,
		Now: func() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) }}
	bt, err := r.Start(context.Background(), "custom", []store.ScreenMember{{Ticker: "V"}, {Ticker: "NOPE"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start(context.Background(), "custom", nil); !errors.Is(err, ErrBusy) {
		t.Errorf("second start: %v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for r.Running(bt.ID) {
		if time.Now().After(deadline) {
			t.Fatal("backtest did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.status["b1"] != "done" || len(st.results["b1"]) != 2 {
		t.Fatalf("status %s, %d results", st.status["b1"], len(st.results["b1"]))
	}
	for _, res := range st.results["b1"] {
		switch res.Ticker {
		case "V":
			if len(res.Observations) < 5 {
				t.Errorf("V has %d observations", len(res.Observations))
			}
		case "NOPE":
			if res.Error == "" {
				t.Error("NOPE should fail")
			}
		}
	}
}
