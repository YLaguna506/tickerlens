package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/agentclient"
	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/metrics"
	"github.com/YLaguna506/tickerlens/backend/internal/peers"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// BodyVersion changes whenever the report body's shape or computation
// changes, so old reports are not reused for new requests.
const BodyVersion = "m6.5"

// Stages, in order.
const (
	StageIngesting = "ingesting"
	StageComputing = "computing"
)

// Section states in Body.Sections.
const (
	SectionStateReady   = "ready"
	SectionStateFailed  = "failed"
	SectionStatePending = "pending"
)

// Store is what the runner needs from the reports table (store.Reports).
type Store interface {
	SetStage(ctx context.Context, id, stage string) error
	SaveBody(ctx context.Context, id string, body json.RawMessage) error
	SetCacheKey(ctx context.Context, id, key string) error
	Finish(ctx context.Context, id string, o store.Outcome) error
	FindByCacheKey(ctx context.Context, ticker, key, excludeID string) (store.Report, error)
}

// Fetcher gathers a ticker's data (ingest.Fetcher).
type Fetcher interface {
	Fetch(ctx context.Context, ticker string) (*ingest.Bundle, error)
}

// Body is the report JSON stored in reports.body and served by the API. It
// is saved after every stage, so a running report can be shown partially.
type Body struct {
	Version    string                        `json:"version"`
	Company    edgar.Company                 `json:"company"`
	Sections   map[string]string             `json:"sections"` // section â†’ ready / failed / pending
	Metrics    *metrics.Report               `json:"metrics,omitempty"`
	Financials *financials.Financials        `json:"financials,omitempty"`
	Earnings   []estimates.QuarterlyEarnings `json:"earnings,omitempty"`
	Notes      []string                      `json:"notes,omitempty"`
	Warnings   []string                      `json:"warnings,omitempty"`
	TimingsMs  map[string]int64              `json:"timingsMs"`

	// Filled by the analyzing stage (AI agents).
	Filing          *FilingRef               `json:"filing,omitempty"`
	Agents          map[string]*AgentSection `json:"agents,omitempty"`
	BusinessQuality *QualityScore            `json:"businessQuality,omitempty"`
	Segments        *SegmentBreakdown        `json:"segments,omitempty"`
	Peers           *peers.Group             `json:"peers,omitempty"`
	Moat            *MoatScore               `json:"moat,omitempty"`
	Risk            *RiskScore               `json:"risk,omitempty"`
	Management      *ManagementInfo          `json:"management,omitempty"`
	CostUSD         float64                  `json:"costUsd"`

	// Filled by the scoring stage.
	Verdict *VerdictSection `json:"verdict,omitempty"`
}

// Runner executes report jobs in background goroutines.
type Runner struct {
	// Agents enables the analyzing stage; nil skips it (e.g. no LLM configured).
	Agents *Agents

	store   Store
	fetcher Fetcher
	broker  *Broker
	cfg     scoring.Config
	logger  *slog.Logger
	timeout time.Duration

	// ctx is cancelled on shutdown; wg tracks running jobs so shutdown can
	// wait for them to record their final state.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewRunner returns a Runner. timeout bounds each job.
func NewRunner(s Store, f Fetcher, b *Broker, cfg scoring.Config, logger *slog.Logger, timeout time.Duration) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{store: s, fetcher: f, broker: b, cfg: cfg, logger: logger, timeout: timeout, ctx: ctx, cancel: cancel}
}

// Start runs a report job in the background and returns immediately. opt
// picks the language of the report's AI text and the AI model.
func (r *Runner) Start(reportID, ticker string, refresh bool, opt store.Options) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.run(reportID, ticker, refresh, opt)
	}()
}

// Shutdown cancels running jobs and waits (until ctx expires) for them to
// record that they were interrupted.
func (r *Runner) Shutdown(ctx context.Context) error {
	r.cancel()
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// job holds one run's state.
type job struct {
	*Runner
	agents     *Agents
	id, ticker string
	opt        store.Options
	body       Body
	mu         sync.Mutex // guards body.TimingsMs while agents run in parallel
	log        *slog.Logger
}

func (r *Runner) run(reportID, ticker string, refresh bool, opt store.Options) {
	j := &job{Runner: r, agents: r.Agents, id: reportID, ticker: ticker, opt: opt,
		log: r.logger.With("report", reportID, "ticker", ticker, "lang", opt.Lang, "model", opt.Model)}
	j.body = Body{Version: BodyVersion, Sections: map[string]string{}, TimingsMs: map[string]int64{}}

	ctx, cancel := context.WithTimeout(r.ctx, r.timeout)
	defer cancel()
	ctx = agentclient.WithJobID(ctx, reportID)

	// A panic in one job must not crash the API; record it as a failure.
	defer func() {
		if p := recover(); p != nil {
			j.fail(fmt.Errorf("internal error: %v", p))
		}
	}()

	start := time.Now()
	j.log.Info("report started")

	// Stage 1: fetch everything.
	bundle, err := timed(j, StageIngesting, func() (*ingest.Bundle, error) {
		return r.fetcher.Fetch(ctx, ticker)
	})
	if err != nil {
		j.fail(err)
		return
	}
	j.body.Company = bundle.Company
	j.body.Financials = &bundle.Financials
	j.body.Earnings = bundle.Earnings
	j.body.Notes = append(bundle.Notes, bundle.Financials.Warnings...)
	j.body.Warnings = bundle.Warnings
	j.section("financials", true, "")
	for _, s := range []string{"prices", "earnings"} {
		j.section(s, !slices.Contains(bundle.Failed, s), warningFor(bundle.Warnings, s))
	}
	failed := slices.Clone(bundle.Failed)

	// Reuse an identical finished report (same filing, same price day, same
	// scoring rules) instead of recomputing it.
	key := cacheKey(bundle, r.cfg, r.Agents, opt)
	if !refresh && len(failed) == 0 {
		if prev, err := r.store.FindByCacheKey(ctx, bundle.Company.Ticker, key, reportID); err == nil {
			if err := r.store.SaveBody(ctx, reportID, prev.Body); err == nil {
				j.finish(store.Outcome{Status: store.StatusDone, ReusedFrom: prev.ID}, "reused an identical report")
				return
			}
		}
	}
	if err := r.store.SetCacheKey(ctx, reportID, key); err != nil {
		j.log.Warn("could not save cache key", "err", err)
	}
	if !j.save(ctx) {
		return
	}

	// Stage 2: compute metrics, phase and scores.
	report, err := timed(j, StageComputing, func() (*metrics.Report, error) {
		m := metrics.Compute(bundle, r.cfg)
		return &m, nil
	})
	if err != nil {
		j.fail(err)
		return
	}
	j.body.Metrics = report
	j.section("metrics", true, "")
	if !j.save(ctx) {
		return
	}

	// Stage 3: AI agents read the 10-K and judge the business.
	if j.agents != nil {
		agentFailures, _ := timed(j, StageAnalyzing, func() ([]string, error) {
			return j.analyze(ctx, bundle, report), nil
		})
		failed = append(failed, agentFailures...)
		if ctx.Err() != nil {
			j.fail(ctx.Err())
			return
		}
		if !j.save(ctx) {
			return
		}
	}

	// Stage 4: the verdict (computed), then its narrative (LLM).
	v, _ := timed(j, StageScoring, func() (*VerdictSection, error) {
		return j.decide(ctx, bundle, report), nil
	})
	j.body.Verdict = v
	j.section("verdict", v.Label != "", v.Rule)
	if ctx.Err() != nil {
		j.fail(ctx.Err())
		return
	}
	j.body.TimingsMs["total"] = time.Since(start).Milliseconds()
	if !j.save(ctx) {
		return
	}

	status := store.StatusDone
	if len(failed) > 0 {
		status = store.StatusPartial
	}
	j.finish(store.Outcome{Status: status, FailedSections: failed}, "")
}

// tokens counts the LLM tokens spent by this run (cached answers excluded).
func (j *job) tokens() int {
	n := 0
	for _, s := range j.body.Agents {
		if s != nil && !s.Cached {
			n += s.Usage.InputTokens + s.Usage.OutputTokens
		}
	}
	if v := j.body.Verdict; v != nil && v.NarrativeUsage != nil {
		n += v.NarrativeUsage.InputTokens + v.NarrativeUsage.OutputTokens
	}
	return n
}

// modelOr is the report's chosen model, or def when it uses the default.
func modelOr(opt store.Options, def string) string {
	if opt.Model != "" {
		return opt.Model
	}
	return def
}

// langSuffix keeps English cache keys as they were and marks the others.
func langSuffix(opt store.Options) string {
	if opt.Lang == "" || opt.Lang == "en" {
		return ""
	}
	return ":" + opt.Lang
}

// choose fills in the report's language and model on an agent task.
func (j *job) choose(t agentclient.Task) agentclient.Task {
	if j.opt.Lang != "" && j.opt.Lang != "en" {
		t.Language = j.opt.Lang
	}
	if j.opt.Model != "" {
		t.Provider, t.Model, _ = strings.Cut(j.opt.Model, ":")
	}
	return t
}

// timed runs one stage: marks it in the database, publishes start/finish
// events and records how long it took.
func timed[T any](j *job, stage string, fn func() (T, error)) (T, error) {
	var zero T
	if err := j.store.SetStage(j.ctx, j.id, stage); err != nil {
		return zero, err
	}
	j.broker.Publish(j.id, Event{Type: StageStarted, Stage: stage})
	t0 := time.Now()
	v, err := fn()
	ms := time.Since(t0).Milliseconds()
	j.mu.Lock()
	j.body.TimingsMs[stage] = ms
	j.mu.Unlock()
	j.log.Info("stage finished", "stage", stage, "ms", ms, "err", err)
	if err != nil {
		return zero, err
	}
	j.broker.Publish(j.id, Event{Type: StageCompleted, Stage: stage})
	return v, nil
}

func (j *job) section(name string, ok bool, msg string) {
	if ok {
		j.body.Sections[name] = SectionStateReady
		j.broker.Publish(j.id, Event{Type: SectionReady, Section: name})
		return
	}
	j.body.Sections[name] = SectionStateFailed
	j.broker.Publish(j.id, Event{Type: SectionFailed, Section: name, Message: msg})
}

// save writes the body; on failure the job is failed.
func (j *job) save(ctx context.Context) bool {
	b, err := json.Marshal(j.body)
	if err == nil {
		err = j.store.SaveBody(ctx, j.id, b)
	}
	if err != nil {
		j.fail(fmt.Errorf("save report: %w", err))
		return false
	}
	return true
}

func (j *job) finish(o store.Outcome, msg string) {
	// Use a fresh context: the job's own may be about to expire.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(j.ctx), 10*time.Second)
	defer cancel()
	if err := j.store.Finish(ctx, j.id, o); err != nil {
		j.log.Error("could not record report outcome", "err", err)
	}
	j.log.Info("report finished", "status", o.Status, "failed", o.FailedSections,
		"total_ms", j.body.TimingsMs["total"], "cost_usd", j.body.CostUSD, "tokens", j.tokens(), "timings_ms", j.body.TimingsMs)
	j.broker.Publish(j.id, Event{Type: JobDone, Status: o.Status, Message: msg})
}

func (j *job) fail(err error) {
	msg := err.Error()
	switch {
	case errors.Is(err, edgar.ErrUnknownTicker):
		msg = "unknown ticker: " + j.ticker
	case errors.Is(err, context.Canceled):
		msg = "interrupted: the API was shutting down"
	case errors.Is(err, context.DeadlineExceeded):
		msg = fmt.Sprintf("timed out after %s", j.timeout)
	}
	j.log.Warn("report failed", "err", err)

	ctx, cancel := context.WithTimeout(context.WithoutCancel(j.ctx), 10*time.Second)
	defer cancel()
	if err := j.store.Finish(ctx, j.id, store.Outcome{Status: store.StatusFailed, Error: msg}); err != nil {
		j.log.Error("could not record report failure", "err", err)
	}
	j.broker.Publish(j.id, Event{Type: JobFailed, Status: store.StatusFailed, Message: msg})
}

// cacheKey identifies the data a report is built from.
func cacheKey(b *ingest.Bundle, cfg scoring.Config, agents *Agents, opt store.Options) string {
	ai := "no-agents"
	if agents != nil {
		ai = agents.PromptVersion + "/" + modelOr(opt, agents.FastModel) + langSuffix(opt)
	}
	accession := ""
	if b.Financials.TTM != nil {
		accession = b.Financials.TTM.Accession
	} else if n := len(b.Financials.Annual); n > 0 {
		accession = b.Financials.Annual[n-1].Accession
	}
	priceDate := ""
	if n := len(b.Prices); n > 0 {
		priceDate = b.Prices[n-1].Date
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s", BodyVersion, accession, priceDate, cfg.Version, ai)
}

func warningFor(warnings []string, section string) string {
	for _, w := range warnings {
		if len(w) > len(section) && w[:len(section)] == section {
			return w
		}
	}
	return ""
}
