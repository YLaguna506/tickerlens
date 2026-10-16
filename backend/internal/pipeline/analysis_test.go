package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/agentclient"
	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

type fakeFilings struct{ downloads int }

func (f *fakeFilings) LatestAnnualReport(context.Context, string) (edgar.Filing, error) {
	return edgar.Filing{Accession: "0001403161-25-000089", Form: "10-K", ReportDate: "2025-09-30", PrimaryDocument: "v-20250930.htm"}, nil
}

func (f *fakeFilings) PrimaryDocument(context.Context, string, edgar.Filing) ([]byte, error) {
	f.downloads++
	return []byte("<html>10-K</html>"), nil
}

func (f *fakeFilings) DocumentURL(string, edgar.Filing) string {
	return "https://sec.example/v-20250930.htm"
}

// fakeAgents answers each section from a map of raw outputs.
type fakeAgents struct {
	hang    string // section that never answers (waits for its context)
	mu      sync.Mutex
	outputs map[string]string // section → output JSON ("" = agent reports failure)
	calls   []agentclient.Task
}

func (f *fakeAgents) Sections(context.Context, []byte) (map[string]string, error) {
	return map[string]string{"1": "Visa is a payments network.", "7": "Net revenue increased."}, nil
}

func (f *fakeAgents) Analyze(ctx context.Context, t agentclient.Task) (agentclient.Result, error) {
	if t.Section == f.hang {
		<-ctx.Done()
		return agentclient.Result{}, ctx.Err()
	}
	f.mu.Lock()
	f.calls = append(f.calls, t)
	f.mu.Unlock()
	out, ok := f.outputs[t.Section]
	if !ok {
		return agentclient.Result{}, errors.New("agents service unreachable")
	}
	if out == "" {
		return agentclient.Result{Section: t.Section, Status: "failed", Errors: []string{"quote is not verbatim"}}, nil
	}
	return agentclient.Result{Section: t.Section, Status: "ok", Output: json.RawMessage(out),
		Usage: agentclient.Usage{Model: "m", CostUSD: 0.001}}, nil
}

const (
	overviewOut = `{"mission":null,"whatItDoes":"Payments network.","howItMakesMoney":"Fees.","payingCustomers":"Banks.","whyTheyBuy":"Acceptance."}`
	qualityOut  = `{"revenuePredictability":{"answer":"Predictable"},"pricingPower":{"answer":"Easily"},"recessionResistance":{"answer":"Okay"},"competitivePosition":{"answer":"Dominant"}}`
	// Visa FY2025 net revenue is $40,000M; gross segment revenue is higher.
	segmentsOut  = `{"unit":"USD millions","fiscalYear":"FY2025","segments":[{"name":"Service","revenue":18000,"priorRevenue":16000},{"name":"Data processing","revenue":21000},{"name":"International","revenue":15000}],"geography":[]}`
	narrativeOut = `{"tagline":"Great business, full price.","rationale":"Quality and health are strong; the price is high."}`
)

// allAgents answers every agent successfully.
func allAgents() map[string]string {
	return map[string]string{AgentOverview: overviewOut, AgentQuality: qualityOut, AgentSegments: segmentsOut, AgentNarrative: narrativeOut}
}

func runWithAgents(t *testing.T, agents *fakeAgents, cache fetch.Cache) (*fakeStore, Body) {
	return runWithAgentsTimeout(t, agents, cache, 0)
}

func runWithAgentsTimeout(t *testing.T, agents *fakeAgents, cache fetch.Cache, stage time.Duration) (*fakeStore, Body) {
	t.Helper()
	s := &fakeStore{}
	broker := NewBroker()
	r := NewRunner(s, fakeFetcher{bundle: visaBundle(t)}, broker, scoring.Default(), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Minute)
	r.Agents = &Agents{Filings: &fakeFilings{}, Service: agents, Cache: cache, FastModel: "m", PromptVersion: "v1", StageTimeout: stage}
	events, unsubscribe := broker.Subscribe("r1")
	defer unsubscribe()
	r.Start("r1", "V", true)
	for e := range events {
		if e.Terminal() {
			break
		}
	}
	var body Body
	if err := json.Unmarshal(s.body, &body); err != nil {
		t.Fatal(err)
	}
	return s, body
}

func TestAnalyzingStage(t *testing.T) {
	agents := &fakeAgents{outputs: allAgents()}
	s, body := runWithAgents(t, agents, nil)

	if s.outcome == nil || s.outcome.Status != store.StatusDone {
		t.Fatalf("outcome = %+v", s.outcome)
	}
	if len(s.stages) != 4 || s.stages[2] != StageAnalyzing || s.stages[3] != StageScoring {
		t.Errorf("stages = %v", s.stages)
	}
	// Predictable 2 + Easily 2 + Okay 1 + Dominant 2 = 7 → 1 + 4 × 7/8 = 4.5
	if body.BusinessQuality == nil || body.BusinessQuality.Points != 7 || body.BusinessQuality.Score != 4.5 {
		t.Errorf("business quality = %+v", body.BusinessQuality)
	}
	if body.Segments == nil || len(body.Segments.Segments) != 3 || body.Segments.Segments[0].Revenue != 18_000e6 {
		t.Fatalf("segments = %+v", body.Segments)
	}
	// 18,000 vs 16,000 the year before: +12.5%; no prior figure → no YoY.
	if yoy := body.Segments.Segments[0].YoY; yoy == nil || *yoy != 0.125 || body.Segments.Segments[1].YoY != nil {
		t.Errorf("segment YoY = %v, %v", yoy, body.Segments.Segments[1].YoY)
	}
	if body.Filing == nil || body.Filing.Accession == "" || body.CostUSD == 0 {
		t.Errorf("filing = %+v cost = %v", body.Filing, body.CostUSD)
	}
	for _, task := range agents.calls {
		if task.Section == AgentNarrative {
			if task.Metrics["verdict"] == nil || len(task.Documents) != 0 {
				t.Errorf("narrative input = %v", task.Metrics)
			}
			continue
		}
		if task.Metrics["netMargin"] == nil || len(task.Documents) == 0 {
			t.Errorf("%s task is missing metrics or documents", task.Section)
		}
	}
	// The verdict uses the agents' quality score (4.5) with growth and health.
	v := body.Verdict
	if v == nil || v.Label == "" || v.Components[0].Score == nil || *v.Components[0].Score != 4.5 {
		t.Fatalf("verdict = %+v", v)
	}
	if v.NarrativeStatus != SectionStateReady || v.Tagline != "Great business, full price." {
		t.Errorf("narrative = %s %q %v", v.NarrativeStatus, v.Tagline, v.NarrativeErrors)
	}
}

func TestNarrativeFailureKeepsVerdict(t *testing.T) {
	outputs := allAgents()
	delete(outputs, AgentNarrative) // service unreachable for the narrative
	s, body := runWithAgents(t, &fakeAgents{outputs: outputs}, nil)

	if s.outcome.Status != store.StatusDone {
		t.Errorf("a missing narrative must not make the report partial: %+v", s.outcome)
	}
	if body.Verdict == nil || body.Verdict.Label == "" || body.Verdict.NarrativeStatus != SectionStateFailed {
		t.Errorf("verdict = %+v", body.Verdict)
	}
}

func TestAgentFailuresMakeReportPartial(t *testing.T) {
	agents := &fakeAgents{outputs: map[string]string{
		AgentOverview: overviewOut,
		AgentQuality:  "", // agent's validation failed
		// segments: service unreachable
	}}
	s, body := runWithAgents(t, agents, nil)

	if s.outcome == nil || s.outcome.Status != store.StatusPartial {
		t.Fatalf("outcome = %+v", s.outcome)
	}
	// Segments are best-effort and never make a report partial.
	if got := strings.Join(s.outcome.FailedSections, ","); got != "business_quality" {
		t.Errorf("failed sections = %s", got)
	}
	if body.Agents[AgentSegments].Status != SectionStateFailed {
		t.Error("the segments failure must still be recorded in the section")
	}
	if body.Agents[AgentOverview].Status != SectionStateReady || body.Metrics == nil {
		t.Error("the overview and metrics must still be in the report")
	}
}

func TestSegmentsThatDoNotAddUpAreHidden(t *testing.T) {
	tiny := `{"unit":"USD millions","fiscalYear":"FY2025","segments":[{"name":"A","revenue":10},{"name":"B","revenue":20}],"geography":[]}`
	outputs := allAgents()
	outputs[AgentSegments] = tiny
	agents := &fakeAgents{outputs: outputs}
	_, body := runWithAgents(t, agents, nil)

	sec := body.Agents[AgentSegments]
	if sec.Status != SectionStateFailed || body.Segments != nil || !strings.Contains(sec.Errors[0], "add up") {
		t.Errorf("segments section = %+v", sec)
	}
}

func TestAgentAnswersAreCached(t *testing.T) {
	cache := fetch.NewMemoryCache()
	outputs := allAgents()

	first := &fakeAgents{outputs: outputs}
	runWithAgents(t, first, cache)
	second := &fakeAgents{outputs: outputs}
	_, body := runWithAgents(t, second, cache)

	if len(first.calls) != 4 || len(second.calls) != 0 {
		t.Errorf("agent calls: first run %d, second run %d (want 4, 0)", len(first.calls), len(second.calls))
	}
	if !body.Agents[AgentOverview].Cached {
		t.Error("second run should report the cached answer")
	}
}

func TestSlowAgentFailsOnlyItsSection(t *testing.T) {
	agents := &fakeAgents{outputs: allAgents(), hang: AgentQuality}
	s, body := runWithAgentsTimeout(t, agents, nil, 200*time.Millisecond)

	if s.outcome == nil || s.outcome.Status != store.StatusPartial {
		t.Fatalf("outcome = %+v", s.outcome)
	}
	sec := body.Agents[AgentQuality]
	if sec.Status != SectionStateFailed || sec.Errors[0] != "the AI agent did not finish in time" {
		t.Errorf("quality section = %+v", sec)
	}
	if body.Agents[AgentOverview].Status != SectionStateReady || body.Verdict == nil || body.Verdict.Label == "" {
		t.Error("the other agents and the verdict must still complete")
	}
}

// countingAgents records the most agents running at the same time.
type countingAgents struct {
	*fakeAgents
	mu        sync.Mutex
	now, peak int
}

func (c *countingAgents) Analyze(ctx context.Context, t agentclient.Task) (agentclient.Result, error) {
	c.mu.Lock()
	c.now++
	c.peak = max(c.peak, c.now)
	c.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	defer func() { c.mu.Lock(); c.now--; c.mu.Unlock() }()
	return c.fakeAgents.Analyze(ctx, t)
}

func TestAgentConcurrencyLimit(t *testing.T) {
	agents := &countingAgents{fakeAgents: &fakeAgents{outputs: allAgents()}}
	s := &fakeStore{}
	broker := NewBroker()
	r := NewRunner(s, fakeFetcher{bundle: visaBundle(t)}, broker, scoring.Default(), slog.New(slog.NewTextHandler(io.Discard, nil)), time.Minute)
	r.Agents = &Agents{Filings: &fakeFilings{}, Service: agents, FastModel: "m", PromptVersion: "v1", Concurrency: 1}
	events, unsubscribe := broker.Subscribe("r1")
	defer unsubscribe()
	r.Start("r1", "V", true)
	for e := range events {
		if e.Terminal() {
			break
		}
	}
	if agents.peak != 1 {
		t.Errorf("peak concurrent agents = %d, want 1", agents.peak)
	}
	if s.outcome == nil || s.outcome.Status != store.StatusDone {
		t.Errorf("outcome = %+v", s.outcome)
	}
}

func TestQualityScoreRejectsUnknownAnswer(t *testing.T) {
	_, err := qualityScore(json.RawMessage(`{"revenuePredictability":{"answer":"Very"},"pricingPower":{"answer":"No"},"recessionResistance":{"answer":"Weak"},"competitivePosition":{"answer":"Weak"}}`))
	if err == nil {
		t.Error("an answer outside the scale must be rejected")
	}
}
