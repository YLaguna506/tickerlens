package screener

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// --- Fakes --------------------------------------------------------------------------

type memStore struct {
	mu      sync.Mutex
	screens map[string]*store.Screen
	results map[string]map[string]store.ScreenResult
}

func newMemStore() *memStore {
	return &memStore{screens: map[string]*store.Screen{}, results: map[string]map[string]store.ScreenResult{}}
}

func (m *memStore) Create(_ context.Context, universe string, p store.ScreenParams, members []store.ScreenMember) (store.Screen, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sc := store.Screen{ID: "s" + string(rune('0'+len(m.screens))), Universe: universe, Status: "scanning", Params: p, Members: members, Total: len(members)}
	m.screens[sc.ID] = &sc
	m.results[sc.ID] = map[string]store.ScreenResult{}
	return sc, nil
}

func (m *memStore) Unfinished(context.Context) ([]store.Screen, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Screen
	for _, sc := range m.screens {
		if sc.Status == "scanning" || sc.Status == "pricing" {
			out = append(out, *sc)
		}
	}
	return out, nil
}

func (m *memStore) SetStatus(_ context.Context, id, status, errorText string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.screens[id].Status, m.screens[id].Error = status, errorText
	return nil
}

func (m *memStore) SaveResult(_ context.Context, id string, r store.ScreenResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[id][r.Ticker] = r
	return nil
}

func (m *memStore) Results(_ context.Context, id string) ([]store.ScreenResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.ScreenResult
	for _, r := range m.results[id] {
		out = append(out, r)
	}
	return out, nil
}

func (m *memStore) status(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.screens[id].Status
}

// fixtures builds bundles from the recorded test data, with or without prices.
type fixtures struct {
	t      *testing.T
	prices bool
	calls  []string
	mu     sync.Mutex
	block  chan struct{} // when set, Fetch waits on it (or ctx)
}

var fixtureCompanies = map[string]edgar.Company{
	"V":    {Ticker: "V", CIK: "0001403161", Name: "VISA INC."},
	"AAPL": {Ticker: "AAPL", CIK: "0000320193", Name: "Apple Inc."},
	"SNOW": {Ticker: "SNOW", CIK: "0001640147", Name: "Snowflake Inc."},
	"JPM":  {Ticker: "JPM", CIK: "0000019617", Name: "JPMORGAN CHASE & CO"},
}

func (f *fixtures) Fetch(ctx context.Context, ticker string) (*ingest.Bundle, error) {
	f.mu.Lock()
	f.calls = append(f.calls, ticker)
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	co, ok := fixtureCompanies[ticker]
	if !ok {
		return nil, errors.New("unknown ticker " + ticker)
	}
	dir := filepath.Join("..", "..", "testdata")
	var facts edgar.CompanyFacts
	readJSON(f.t, filepath.Join(dir, "edgar", "companyfacts_"+ticker+".json"), &facts)
	var class map[string][]edgar.Fact
	if readJSON(f.t, filepath.Join(dir, "edgar", "classfacts_"+ticker+".json"), &class) {
		edgar.MergeClassFacts(&facts, class["eps"], class["shares"])
	}
	var bars []prices.Bar
	readJSON(f.t, filepath.Join(dir, "tiingo", "prices_"+ticker+".json"), &bars)
	b := &ingest.Bundle{Company: co, Financials: financials.Normalize(co, &facts, ingest.Splits(bars))}
	if ticker == "JPM" {
		b.Industry = &edgar.Profile{SIC: "6021"}
	}
	if f.prices {
		b.Prices = bars
	}
	return b, nil
}

func readJSON(t *testing.T, path string, v any) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return true
}

func members(tickers ...string) []store.ScreenMember {
	var out []store.ScreenMember
	for _, t := range tickers {
		out = append(out, store.ScreenMember{Ticker: t})
	}
	return out
}

func waitFor(t *testing.T, s *Screener, id string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for s.Running(id) {
		if time.Now().After(deadline) {
			t.Fatal("screen did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// --- Tests --------------------------------------------------------------------------

// Fundamentals for everyone; prices only for the strongest businesses above
// the bar, up to the cap; a ticker that cannot be read fails alone.
func TestScreenTwoStages(t *testing.T) {
	st := newMemStore()
	fund, priced := &fixtures{t: t}, &fixtures{t: t, prices: true}
	s := &Screener{Fundamentals: fund, Priced: priced, Store: st, Config: scoring.Default(), Workers: 2}

	sc, err := s.Start(context.Background(), "custom", members("V", "AAPL", "SNOW", "JPM", "NOPE"), store.ScreenParams{MinBusiness: 3, MaxPriced: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(context.Background(), "custom", members("V"), store.ScreenParams{}); !errors.Is(err, ErrBusy) {
		t.Errorf("second screen while one runs: %v", err)
	}
	waitFor(t, s, sc.ID)
	if got := st.status(sc.ID); got != "done" {
		t.Fatalf("status = %s", got)
	}

	results, _ := st.Results(context.Background(), sc.ID)
	byStage := map[string][]string{}
	for _, r := range results {
		byStage[r.Stage] = append(byStage[r.Stage], r.Ticker)
		if r.Stage == StagePriced && (r.Valuation == nil || r.Zone == "" || r.Price == nil) {
			t.Errorf("%s priced without a valuation: %+v", r.Ticker, r)
		}
		if r.Stage != StageFailed && r.Business == nil {
			t.Errorf("%s has no business score", r.Ticker)
		}
	}
	if !slices.Equal(byStage[StageFailed], []string{"NOPE"}) {
		t.Errorf("failed = %v", byStage[StageFailed])
	}
	if len(byStage[StagePriced]) != 2 {
		t.Errorf("priced = %v, want the 2 strongest above 3", byStage[StagePriced])
	}
	// Only the priced companies were fetched with prices.
	pricedCalls := slices.Clone(priced.calls)
	slices.Sort(pricedCalls)
	want := slices.Clone(byStage[StagePriced])
	slices.Sort(want)
	if !slices.Equal(pricedCalls, want) {
		t.Errorf("price lookups %v, want %v", pricedCalls, want)
	}
	// Priced first by business score: none capped or below is stronger.
	minPriced := 5.0
	for _, r := range results {
		if r.Stage == StagePriced {
			minPriced = min(minPriced, *r.Business)
		}
	}
	for _, r := range results {
		if (r.Stage == StageCapped || r.Stage == StageBelow) && *r.Business > minPriced {
			t.Errorf("%s (%.2f, %s) is stronger than a priced company (%.2f)", r.Ticker, *r.Business, r.Stage, minPriced)
		}
		if r.Stage == StageBelow && *r.Business >= 3 {
			t.Errorf("%s marked below with %.2f", r.Ticker, *r.Business)
		}
	}
	// JPM is read as a bank.
	for _, r := range results {
		if r.Ticker == "JPM" && r.Kind != "bank" {
			t.Errorf("JPM kind = %q", r.Kind)
		}
	}
}

// A shutdown leaves the screen resumable; Resume finishes it without
// reading again what was saved.
func TestScreenResumesAfterShutdown(t *testing.T) {
	st := newMemStore()
	base, shutdown := context.WithCancel(context.Background())
	fund := &fixtures{t: t, block: make(chan struct{})}
	s := &Screener{Fundamentals: fund, Priced: &fixtures{t: t, prices: true}, Store: st, Config: scoring.Default(), Base: base, Workers: 1}
	sc, err := s.Start(context.Background(), "custom", members("V", "AAPL"), store.ScreenParams{MinBusiness: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Let V through, then shut down while AAPL is being read.
	fund.block <- struct{}{}
	deadline := time.Now().Add(10 * time.Second)
	for {
		rs, _ := st.Results(context.Background(), sc.ID)
		if len(rs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("V was not saved")
		}
		time.Sleep(5 * time.Millisecond)
	}
	shutdown()
	s.Wait()
	if got := st.status(sc.ID); got != "scanning" {
		t.Fatalf("after shutdown status = %s, want scanning (resumable)", got)
	}

	fund.block, fund.calls = nil, nil
	s2 := &Screener{Fundamentals: fund, Priced: &fixtures{t: t, prices: true}, Store: st, Config: scoring.Default()}
	if err := s2.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s2, sc.ID)
	if got := st.status(sc.ID); got != "done" {
		t.Fatalf("resumed status = %s", got)
	}
	if !slices.Equal(fund.calls, []string{"AAPL"}) {
		t.Errorf("resume read %v again, want only AAPL", fund.calls)
	}
}

func TestCancel(t *testing.T) {
	st := newMemStore()
	fund := &fixtures{t: t, block: make(chan struct{})}
	s := &Screener{Fundamentals: fund, Priced: fund, Store: st, Config: scoring.Default()}
	sc, _ := s.Start(context.Background(), "custom", members("V"), store.ScreenParams{})
	if !s.Cancel(sc.ID) {
		t.Fatal("not running")
	}
	waitFor(t, s, sc.ID)
	if got := st.status(sc.ID); got != "cancelled" {
		t.Errorf("status = %s", got)
	}
}

func TestBusiness(t *testing.T) {
	w := scoring.Default().Weights // growth 0.25, health 0.20
	g, h := 5.0, 2.0
	if b := Business(w, &g, &h); b == nil || *b != 3.67 {
		t.Errorf("both = %v", b)
	}
	if b := Business(w, &g, nil); b == nil || *b != 5 {
		t.Errorf("growth only = %v", b)
	}
	if Business(w, nil, nil) != nil {
		t.Error("no scores should give no business score")
	}
}

func TestUniverse(t *testing.T) {
	m, err := ParseConstituents([]byte("Symbol,Security,GICS Sector\nBRK.B,Berkshire Hathaway,Financials\nAAPL,Apple Inc.,Information Technology\nAAPL,dup,x\n"))
	if err != nil || len(m) != 2 || m[0].Ticker != "BRK-B" || m[1].Sector != "Information Technology" {
		t.Errorf("parsed %+v, %v", m, err)
	}
	if snap := SP500(context.Background(), nil); len(snap) < 490 {
		t.Errorf("bundled S&P 500 has %d members", len(snap))
	}
	got := ParseTickers("aapl, MSFT;brk.b\nAAPL  ")
	if len(got) != 3 || got[2].Ticker != "BRK-B" {
		t.Errorf("ParseTickers = %+v", got)
	}
}

// Equal business scores: the one with more free cash per dollar of sales
// goes first; no score goes last.
func TestRank(t *testing.T) {
	five, four, thin, fat := 5.0, 4.0, 0.05, 0.30
	rs := []store.ScreenResult{
		{Ticker: "AAA", Business: &five, FCFMargin: &thin},
		{Ticker: "NONE"},
		{Ticker: "ZZZ", Business: &five, FCFMargin: &fat},
		{Ticker: "MID", Business: &four, FCFMargin: &fat},
	}
	Rank(rs)
	var got []string
	for _, r := range rs {
		got = append(got, r.Ticker)
	}
	if !slices.Equal(got, []string{"ZZZ", "AAA", "MID", "NONE"}) {
		t.Errorf("rank = %v", got)
	}
}
