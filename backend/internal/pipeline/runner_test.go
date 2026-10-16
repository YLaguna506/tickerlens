package pipeline

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

// fakeStore records what the runner writes.
type fakeStore struct {
	mu       sync.Mutex
	stages   []string
	body     json.RawMessage
	key      string
	outcome  *store.Outcome
	previous *store.Report // returned by FindByCacheKey
}

func (s *fakeStore) SetStage(_ context.Context, _, stage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stages = append(s.stages, stage)
	return nil
}

func (s *fakeStore) SaveBody(_ context.Context, _ string, body json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = body
	return nil
}

func (s *fakeStore) SetCacheKey(_ context.Context, _, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key = key
	return nil
}

func (s *fakeStore) Finish(_ context.Context, _ string, o store.Outcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outcome = &o
	return nil
}

func (s *fakeStore) FindByCacheKey(context.Context, string, string, string) (store.Report, error) {
	if s.previous == nil {
		return store.Report{}, store.ErrNotFound
	}
	return *s.previous, nil
}

// fakeFetcher returns a fixed bundle or error, optionally waiting for the
// context to end (to simulate a slow provider during shutdown).
type fakeFetcher struct {
	bundle *ingest.Bundle
	err    error
	block  bool
}

func (f fakeFetcher) Fetch(ctx context.Context, _ string) (*ingest.Bundle, error) {
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.bundle, f.err
}

func visaBundle(t *testing.T) *ingest.Bundle {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "edgar", "companyfacts_V.json"))
	if err != nil {
		t.Fatal(err)
	}
	var facts edgar.CompanyFacts
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	co := edgar.Company{Ticker: "V", CIK: "0001403161", Name: "VISA INC."}
	var bars []prices.Bar
	raw, err = os.ReadFile(filepath.Join("..", "..", "testdata", "tiingo", "prices_V.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &bars); err != nil {
		t.Fatal(err)
	}
	return &ingest.Bundle{Company: co, Financials: financials.Normalize(co, &facts, ingest.Splits(bars)), Prices: bars}
}

// runJob starts a job and collects its events until the terminal one.
func runJob(t *testing.T, s *fakeStore, f Fetcher, refresh bool) []Event {
	t.Helper()
	broker := NewBroker()
	r := NewRunner(s, f, broker, scoring.Default(), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Minute)
	events, unsubscribe := broker.Subscribe("r1")
	defer unsubscribe()

	r.Start("r1", "V", refresh, store.Options{})
	var got []Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case e := <-events:
			got = append(got, e)
			if e.Terminal() {
				return got
			}
		case <-timeout:
			t.Fatalf("no terminal event; got %+v", got)
		}
	}
}

func types(events []Event) []string {
	var out []string
	for _, e := range events {
		out = append(out, e.Type+":"+e.Stage+e.Section)
	}
	return out
}

func TestRunSuccess(t *testing.T) {
	s := &fakeStore{}
	events := runJob(t, s, fakeFetcher{bundle: visaBundle(t)}, false)

	last := events[len(events)-1]
	if last.Type != JobDone || last.Status != store.StatusDone {
		t.Fatalf("last event = %+v; all: %v", last, types(events))
	}
	if s.outcome == nil || s.outcome.Status != store.StatusDone {
		t.Errorf("outcome = %+v", s.outcome)
	}
	if len(s.stages) != 3 || s.stages[0] != StageIngesting || s.stages[1] != StageComputing || s.stages[2] != StageScoring {
		t.Errorf("stages = %v", s.stages)
	}
	var body Body
	if err := json.Unmarshal(s.body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Metrics == nil || body.Sections["metrics"] != SectionStateReady || body.Company.Ticker != "V" {
		t.Errorf("body incomplete: sections=%v", body.Sections)
	}
	if s.key == "" {
		t.Error("cache key not recorded")
	}
	// Without AI agents the verdict uses growth and health only, with lower confidence.
	if body.Verdict == nil || body.Verdict.Label == "" || body.Verdict.ConfidenceLevel == "high" || body.Verdict.NarrativeStatus != "skipped" {
		t.Errorf("verdict = %+v", body.Verdict)
	}
}

func TestRunPartialWhenAProviderFails(t *testing.T) {
	b := visaBundle(t)
	b.Prices = nil
	b.Warnings = []string{"prices: tiingo: HTTP 500"}
	b.Failed = []string{"prices"}
	s := &fakeStore{}

	events := runJob(t, s, fakeFetcher{bundle: b}, false)

	if s.outcome == nil || s.outcome.Status != store.StatusPartial || len(s.outcome.FailedSections) != 1 {
		t.Fatalf("outcome = %+v", s.outcome)
	}
	sawFailed := false
	for _, e := range events {
		if e.Type == SectionFailed && e.Section == "prices" && e.Message != "" {
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Errorf("expected a section.failed event for prices; got %v", types(events))
	}
	var body Body
	_ = json.Unmarshal(s.body, &body)
	if body.Metrics == nil {
		t.Error("the rest of the report must still be computed")
	}
}

func TestRunUnknownTickerFails(t *testing.T) {
	s := &fakeStore{}
	events := runJob(t, s, fakeFetcher{err: edgar.ErrUnknownTicker}, false)

	last := events[len(events)-1]
	if last.Type != JobFailed || last.Message != "unknown ticker: V" {
		t.Errorf("last event = %+v", last)
	}
	if s.outcome == nil || s.outcome.Status != store.StatusFailed {
		t.Errorf("outcome = %+v", s.outcome)
	}
}

func TestRunReusesIdenticalReport(t *testing.T) {
	prev := store.Report{ID: "r0", Body: json.RawMessage(`{"version":"old"}`)}
	s := &fakeStore{previous: &prev}

	runJob(t, s, fakeFetcher{bundle: visaBundle(t)}, false)

	if s.outcome == nil || s.outcome.ReusedFrom != "r0" || string(s.body) != `{"version":"old"}` {
		t.Errorf("outcome = %+v body = %s", s.outcome, s.body)
	}
	if len(s.stages) != 1 {
		t.Errorf("metrics should not be recomputed; stages = %v", s.stages)
	}
}

func TestRefreshSkipsReuse(t *testing.T) {
	prev := store.Report{ID: "r0", Body: json.RawMessage(`{}`)}
	s := &fakeStore{previous: &prev}

	runJob(t, s, fakeFetcher{bundle: visaBundle(t)}, true)

	if s.outcome == nil || s.outcome.ReusedFrom != "" {
		t.Errorf("refresh must recompute; outcome = %+v", s.outcome)
	}
}

func TestShutdownInterruptsRunningJobs(t *testing.T) {
	s := &fakeStore{}
	broker := NewBroker()
	r := NewRunner(s, fakeFetcher{block: true}, broker, scoring.Default(), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Minute)
	events, unsubscribe := broker.Subscribe("r1")
	defer unsubscribe()
	r.Start("r1", "V", false, store.Options{})
	<-events // stage.started

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := r.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if s.outcome == nil || s.outcome.Status != store.StatusFailed || s.outcome.Error != "interrupted: the API was shutting down" {
		t.Errorf("outcome = %+v", s.outcome)
	}
}

func TestBrokerUnsubscribe(t *testing.T) {
	b := NewBroker()
	ch, unsubscribe := b.Subscribe("x")
	b.Publish("x", Event{Type: JobDone})
	if e := <-ch; e.Type != JobDone || e.Time.IsZero() {
		t.Errorf("event = %+v", e)
	}
	unsubscribe()
	unsubscribe() // safe to call twice
	b.Publish("x", Event{Type: JobDone})
	select {
	case e := <-ch:
		t.Errorf("received after unsubscribe: %+v", e)
	default:
	}
}
