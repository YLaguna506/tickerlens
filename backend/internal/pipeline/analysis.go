package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"

	"github.com/YLaguna506/tickerlens/backend/internal/agentclient"
	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/metrics"
)

// StageAnalyzing runs the AI agents.
const StageAnalyzing = "analyzing"

// The agent sections, run in parallel.
const (
	AgentOverview = "business_overview"
	AgentQuality  = "business_quality"
	AgentSegments = "segments"
)

// FilingSource finds and downloads 10-K documents (edgar.Client).
type FilingSource interface {
	LatestAnnualReport(ctx context.Context, cik string) (edgar.Filing, error)
	PrimaryDocument(ctx context.Context, cik string, f edgar.Filing) ([]byte, error)
	DocumentURL(cik string, f edgar.Filing) string
}

// AgentService is the Python agents service (agentclient.Client).
type AgentService interface {
	Sections(ctx context.Context, html []byte) (map[string]string, error)
	Analyze(ctx context.Context, task agentclient.Task) (agentclient.Result, error)
}

// Agents configures the analyzing stage. A nil *Agents skips it.
type Agents struct {
	Filings       FilingSource
	Service       AgentService
	Cache         fetch.Cache // optional: caches filing sections and agent results
	FastModel     string      // part of the cache key: a new model means new answers
	StrongModel   string      // model of the verdict narrative (cache key)
	PromptVersion string      // default prompt version for every agent
	// PromptVersions overrides the version per agent, so changing one prompt
	// does not invalidate the cached answers of the others.
	PromptVersions map[string]string
	MaxDocChars    int     // per document sent to an agent (a token budget)
	CostCapUSD     float64 // LLM spend per report; the narrative is skipped beyond it (0 = no cap)
	// StageTimeout bounds the agents stage; an agent still running when it
	// expires fails its own section and the report carries on (default 6 min).
	StageTimeout time.Duration
	// Concurrency limits how many agents run at once (0 = all three). Free
	// LLM tiers have per-minute quotas; 1 runs them one after another.
	Concurrency int
}

// promptFor returns the prompt version used for an agent.
func (a *Agents) promptFor(section string) string {
	if v, ok := a.PromptVersions[section]; ok {
		return v
	}
	return a.PromptVersion
}

// AgentSection is one agent's outcome in the report body.
type AgentSection struct {
	Status   string                 `json:"status"` // ready | failed
	Output   json.RawMessage        `json:"output,omitempty"`
	Evidence []agentclient.Evidence `json:"evidence,omitempty"`
	Errors   []string               `json:"errors,omitempty"`
	Usage    agentclient.Usage      `json:"usage"`
	Cached   bool                   `json:"cached"`
}

// FilingRef identifies the 10-K the agents read.
type FilingRef struct {
	Accession  string `json:"accession"`
	ReportDate string `json:"reportDate"`
	URL        string `json:"url"`
}

// QualityScore is the business quality score computed from the agent's
// four answers: 0/1/2 points each, score = 1 + 4 × points/8.
type QualityScore struct {
	Score   float64           `json:"score"`
	Points  int               `json:"points"`
	Answers map[string]string `json:"answers"`
}

// SegmentShare is one line of a validated revenue breakdown.
type SegmentShare struct {
	Name         string   `json:"name"`
	Revenue      float64  `json:"revenue"`      // USD
	Share        float64  `json:"share"`        // of the breakdown's total
	PriorRevenue *float64 `json:"priorRevenue"` // USD, previous fiscal year (when disclosed)
	YoY          *float64 `json:"yoy"`          // revenue ÷ prior − 1
}

// SegmentBreakdown is the segments agent's output after Go checks it.
type SegmentBreakdown struct {
	FiscalYear string         `json:"fiscalYear"`
	Segments   []SegmentShare `json:"segments,omitempty"`
	Geography  []SegmentShare `json:"geography,omitempty"`
}

// analyze runs the agents in parallel and records each section's outcome.
// Failures are per section: the rest of the report is unaffected.
func (j *job) analyze(ctx context.Context, bundle *ingest.Bundle, report *metrics.Report) []string {
	a := j.agents
	limit := a.StageTimeout
	if limit <= 0 {
		limit = 6 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	j.body.Agents = map[string]*AgentSection{}
	sections := []string{AgentOverview, AgentQuality, AgentSegments}
	failAll := func(msg string) []string {
		for _, s := range sections {
			j.body.Agents[s] = &AgentSection{Status: SectionStateFailed, Errors: []string{msg}}
			j.section(s, false, msg)
		}
		return sections
	}

	filing, items, err := j.filingSections(ctx, bundle.Company.CIK)
	if err != nil {
		return failAll("could not read the latest 10-K: " + err.Error())
	}
	j.body.Filing = &FilingRef{Accession: filing.Accession, ReportDate: filing.ReportDate, URL: a.Filings.DocumentURL(bundle.Company.CIK, filing)}

	doc := func(item, title string) agentclient.Document {
		text := items[item]
		if a.MaxDocChars > 0 && len(text) > a.MaxDocChars {
			text = text[:a.MaxDocChars]
		}
		return agentclient.Document{
			ID:     "10k-item" + strings.ToLower(item),
			Source: fmt.Sprintf("10-K for the period ended %s, Item %s (%s)", filing.ReportDate, item, title),
			URL:    j.body.Filing.URL,
			Text:   text,
		}
	}
	business, mdna := doc("1", "Business"), doc("7", "Management's Discussion and Analysis")
	// Segment tables can sit deep in a long MD&A (HPE's start ~49k
	// characters in): give that agent the stretch that talks most about
	// segments instead of the beginning.
	segmentsDoc := mdna
	if a.MaxDocChars > 0 {
		segmentsDoc.Text = segmentWindow(items["7"], a.MaxDocChars)
	}
	if business.Text == "" {
		return failAll("the 10-K's Item 1 (Business) could not be found")
	}
	docs := map[string][]agentclient.Document{
		AgentOverview: {business},
		AgentQuality:  {business, mdna},
		// Revenue tables are in MD&A; sending only Item 7 halves the reading.
		AgentSegments: {segmentsDoc},
	}
	facts := agentFacts(report)

	results := make([]*AgentSection, len(sections))
	var g errgroup.Group // no shared cancellation: one agent failing must not stop the others
	if a.Concurrency > 0 {
		g.SetLimit(a.Concurrency)
	}
	for i, s := range sections {
		g.Go(func() error {
			results[i] = j.runAgent(ctx, filing, j.choose(agentclient.Task{
				JobID:         j.id,
				Section:       s,
				Ticker:        bundle.Company.Ticker,
				CompanyName:   bundle.Company.Name,
				PromptVersion: a.promptFor(s),
				Metrics:       facts,
				Documents:     docs[s],
			}))
			return nil
		})
	}
	_ = g.Wait()

	var failed []string
	for i, s := range sections {
		sec := results[i]
		j.body.Agents[s] = sec
		j.body.CostUSD += sec.Usage.CostUSD
		if sec.Status == SectionStateReady {
			switch s {
			case AgentQuality:
				q, err := qualityScore(sec.Output)
				if err != nil {
					sec.Status, sec.Errors = SectionStateFailed, []string{err.Error()}
				} else {
					j.body.BusinessQuality = q
				}
			case AgentSegments:
				seg, err := checkSegments(sec.Output, bundle)
				if err != nil {
					sec.Status, sec.Errors = SectionStateFailed, []string{err.Error()}
				} else {
					j.body.Segments = seg
				}
			}
		}
		ok := sec.Status == SectionStateReady
		j.section(s, ok, strings.Join(sec.Errors, "; "))
		// Segments are best-effort: many companies disclose no breakdown, so
		// a missing one hides the section without marking the report partial.
		if !ok && s != AgentSegments {
			failed = append(failed, s)
		}
	}
	return failed
}

// runAgent calls one agent, using the cached answer for the same filing,
// section, prompt version and model when there is one.
func (j *job) runAgent(ctx context.Context, filing edgar.Filing, task agentclient.Task) *AgentSection {
	a := j.agents
	key := fmt.Sprintf("agent:%s:%s:%s:%s:%s", task.Ticker, filing.Accession, task.Section, task.PromptVersion, modelOr(j.opt, a.FastModel)) + langSuffix(j.opt)
	if task.Section == AgentSegments {
		// Which stretch of Item 7 the segments agent reads can change, so its
		// answer is also keyed by the text it was given.
		key += ":" + docsHash(task.Documents)
	}
	if a.Cache != nil {
		if b, ok, err := a.Cache.Get(ctx, key); err == nil && ok {
			var cached AgentSection
			if json.Unmarshal(b, &cached) == nil {
				cached.Cached = true
				return &cached
			}
		}
	}

	t0 := time.Now()
	res, err := a.Service.Analyze(ctx, task)
	j.mu.Lock()
	j.body.TimingsMs["agent:"+task.Section] = time.Since(t0).Milliseconds()
	j.mu.Unlock()
	if errors.Is(err, context.DeadlineExceeded) {
		return &AgentSection{Status: SectionStateFailed, Errors: []string{"the AI agent did not finish in time"}}
	}
	if err != nil {
		return &AgentSection{Status: SectionStateFailed, Errors: []string{err.Error()}}
	}
	sec := &AgentSection{Output: res.Output, Evidence: res.Evidence, Errors: res.Errors, Usage: res.Usage}
	if !res.OK() {
		sec.Status = SectionStateFailed
		return sec
	}
	sec.Status = SectionStateReady
	// An empty breakdown is not worth keeping: a later prompt or document
	// window may find the table.
	if a.Cache != nil && (task.Section != AgentSegments || !emptySegments(sec.Output)) {
		if b, err := json.Marshal(sec); err == nil {
			_ = a.Cache.Set(ctx, key, b, 365*24*time.Hour)
		}
	}
	return sec
}

// filingSections returns the latest 10-K split by Item, from the cache when
// possible (a filing never changes).
func (j *job) filingSections(ctx context.Context, cik string) (edgar.Filing, map[string]string, error) {
	a := j.agents
	filing, err := a.Filings.LatestAnnualReport(ctx, cik)
	if err != nil {
		return filing, nil, err
	}
	key := "sec:sections:v1:" + filing.Accession
	if a.Cache != nil {
		if b, ok, err := a.Cache.Get(ctx, key); err == nil && ok {
			var items map[string]string
			if json.Unmarshal(b, &items) == nil {
				return filing, items, nil
			}
		}
	}
	html, err := a.Filings.PrimaryDocument(ctx, cik, filing)
	if err != nil {
		return filing, nil, err
	}
	items, err := a.Service.Sections(ctx, html)
	if err != nil {
		return filing, nil, err
	}
	if a.Cache != nil {
		if b, err := json.Marshal(items); err == nil {
			_ = a.Cache.Set(ctx, key, b, 10*365*24*time.Hour)
		}
	}
	return filing, items, nil
}

// agentFacts is the compact set of computed numbers agents may cite. Keeping
// it small keeps the number guard strict (charts and history are left out).
func agentFacts(r *metrics.Report) map[string]any {
	facts := map[string]any{
		"latestFiscalYear":  fmt.Sprintf("FY%d", r.LatestFiscalYr),
		"financialsThrough": r.FinancialsTo,
		"phase":             r.Phase.Name,
		"phaseReason":       r.Phase.Reason,
	}
	for _, group := range [][]metrics.Metric{r.Profitability, r.Growth, r.Health, r.Valuation, r.Returns} {
		for _, m := range group {
			if m.Value != nil {
				facts[m.Key] = *m.Value
			}
		}
	}
	if r.MarketCap.Value != nil {
		facts["marketCap"] = *r.MarketCap.Value
	}
	return facts
}

var qualityPoints = map[string]map[string]int{
	"revenuePredictability": {"Unpredictable": 0, "Modest": 1, "Predictable": 2},
	"pricingPower":          {"No": 0, "Sometimes": 1, "Easily": 2},
	"recessionResistance":   {"Weak": 0, "Okay": 1, "Strong": 2},
	"competitivePosition":   {"Weak": 0, "Average": 1, "Dominant": 2},
}

func qualityScore(output json.RawMessage) (*QualityScore, error) {
	var answers map[string]struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(output, &answers); err != nil {
		return nil, fmt.Errorf("business quality output: %w", err)
	}
	q := &QualityScore{Answers: map[string]string{}}
	for question, scale := range qualityPoints {
		a, ok := answers[question]
		pts, valid := scale[a.Answer]
		if !ok || !valid {
			return nil, fmt.Errorf("business quality: missing or invalid answer for %s", question)
		}
		q.Points += pts
		q.Answers[question] = a.Answer
	}
	q.Score = 1 + 4*float64(q.Points)/8
	return q, nil
}

// segmentRow is one line of the segments agent's output.
type segmentRow struct {
	Name         string   `json:"name"`
	Revenue      float64  `json:"revenue"`
	PriorRevenue *float64 `json:"priorRevenue"`
}

var unitScale = map[string]float64{"USD": 1, "USD thousands": 1e3, "USD millions": 1e6, "USD billions": 1e9}

// checkSegments converts the breakdown to dollars and checks that it adds up
// to roughly the company's revenue. Segment figures are often before
// incentives or eliminations (Visa's exceed net revenue by ~30%), so the
// accepted range is wide; anything outside it is hidden rather than shown.
func checkSegments(output json.RawMessage, b *ingest.Bundle) (*SegmentBreakdown, error) {
	var raw struct {
		Unit       string       `json:"unit"`
		FiscalYear string       `json:"fiscalYear"`
		Segments   []segmentRow `json:"segments"`
		Geography  []segmentRow `json:"geography"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("segments output: %w", err)
	}
	scale, ok := unitScale[raw.Unit]
	if !ok {
		return nil, fmt.Errorf("segments: unknown unit %q", raw.Unit)
	}
	var revenue float64
	if n := len(b.Financials.Annual); n > 0 && b.Financials.Annual[n-1].Revenue != nil {
		revenue = *b.Financials.Annual[n-1].Revenue
	}

	build := func(name string, rows []SegmentShare) ([]SegmentShare, error) {
		if len(rows) < 2 {
			return nil, nil // a single row is not a breakdown
		}
		var sum float64
		for _, r := range rows {
			sum += r.Revenue
		}
		if revenue > 0 {
			if ratio := sum / revenue; ratio < 0.9 || ratio > 1.6 {
				return nil, fmt.Errorf("%s add up to %.0f%% of reported revenue; hidden", name, ratio*100)
			}
		}
		for i := range rows {
			rows[i].Share = rows[i].Revenue / sum
		}
		return rows, nil
	}
	toShares := func(rows []segmentRow) []SegmentShare {
		out := make([]SegmentShare, 0, len(rows))
		for _, r := range rows {
			s := SegmentShare{Name: r.Name, Revenue: r.Revenue * scale}
			if r.PriorRevenue != nil && *r.PriorRevenue > 0 {
				prior := *r.PriorRevenue * scale
				yoy := s.Revenue/prior - 1
				s.PriorRevenue, s.YoY = &prior, &yoy
			}
			out = append(out, s)
		}
		return out
	}

	seg := &SegmentBreakdown{FiscalYear: raw.FiscalYear}
	var err error
	if seg.Segments, err = build("segments", toShares(raw.Segments)); err != nil {
		return nil, err
	}
	if seg.Geography, err = build("regions", toShares(raw.Geography)); err != nil {
		return nil, err
	}
	if seg.Segments == nil && seg.Geography == nil {
		return nil, fmt.Errorf("no revenue breakdown disclosed in the documents")
	}
	return seg, nil
}

// segmentWindow returns text if it fits in size bytes; otherwise the window of
// that size mentioning "segment" most often, centered on those mentions.
func segmentWindow(text string, size int) string {
	if len(text) <= size {
		return text
	}
	lower := strings.ToLower(text)
	best, bestCount := 0, -1
	step := max(size/10, 1)
	for start := 0; ; start += step {
		if start+size > len(text) {
			start = len(text) - size
		}
		if n := strings.Count(lower[start:start+size], "segment"); n > bestCount {
			best, bestCount = start, n
		}
		if start+size >= len(text) {
			break
		}
	}
	// Center the window on the mentions it holds, so a table that starts
	// with "Segment results" is not cut short.
	if first := strings.Index(lower[best:best+size], "segment"); first >= 0 {
		last := strings.LastIndex(lower[best:best+size], "segment")
		best = min(max(best+(first+last)/2-size/2, 0), len(text)-size)
	}
	// Start and end on character boundaries.
	end := best + size
	for best > 0 && !utf8.RuneStart(text[best]) {
		best--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[best:end]
}

// docsHash fingerprints the text given to an agent.
func docsHash(docs []agentclient.Document) string {
	h := sha256.New()
	for _, d := range docs {
		h.Write([]byte(d.ID))
		h.Write([]byte{0})
		h.Write([]byte(d.Text))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// emptySegments reports whether a segments answer lists no breakdown at all.
func emptySegments(output json.RawMessage) bool {
	var out struct {
		Segments  []json.RawMessage `json:"segments"`
		Geography []json.RawMessage `json:"geography"`
	}
	return json.Unmarshal(output, &out) == nil && len(out.Segments) == 0 && len(out.Geography) == 0
}
