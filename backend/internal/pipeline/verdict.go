package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/agentclient"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/metrics"
	"github.com/YLaguna506/tickerlens/backend/internal/verdict"
)

// StageScoring decides the verdict and writes its narrative.
const StageScoring = "scoring"

// AgentNarrative writes the verdict's tagline and rationale.
const AgentNarrative = "verdict_narrative"

// VerdictSection is the verdict in the report body: the computed decision
// plus the LLM-written narrative (optional; the rule text explains it too).
type VerdictSection struct {
	verdict.Result
	Tagline         string   `json:"tagline,omitempty"`
	Rationale       string   `json:"rationale,omitempty"`
	NarrativeStatus string   `json:"narrativeStatus"` // ready | failed | skipped
	NarrativeErrors []string `json:"narrativeErrors,omitempty"`
	// The narrative call's tokens and cost; nil when it came from the cache.
	NarrativeUsage *agentclient.Usage `json:"narrativeUsage,omitempty"`
}

// decide computes the verdict from the scores, then asks the narrative agent
// to explain it. A narrative failure never changes the verdict.
func (j *job) decide(ctx context.Context, bundle *ingest.Bundle, report *metrics.Report) *VerdictSection {
	in := verdict.Input{
		Growth:             report.GrowthScore.Value,
		FinancialHealth:    report.HealthScore.Value,
		Valuation:          report.ValuationScore.Value,
		ValuationFellBack:  report.Bands != nil && report.Bands.FellBackFrom != "",
		PhaseLowConfidence: report.Phase.LowConfidence,
		MissingData:        bundle.Failed,
	}
	if ls := report.LimitedSupport; ls != nil {
		in.LimitedSupport = ls.Kind
	}
	if m := j.body.Moat; m != nil {
		in.MoatWidth, in.MoatDirection = &m.Width, &m.Direction
	}
	if r := j.body.Risk; r != nil {
		in.BusinessRisk = &r.BusinessRisk
	}
	in.Insiders = insidersInput(report)
	if q := j.body.BusinessQuality; q != nil {
		in.BusinessQuality = &q.Score
	}
	for _, s := range []string{AgentOverview, AgentQuality, AgentMoat, AgentRisk} {
		if sec := j.body.Agents[s]; sec != nil && sec.Status != SectionStateReady {
			in.FailedAnalysis = append(in.FailedAnalysis, s)
		}
	}

	v := &VerdictSection{Result: verdict.Decide(in, j.cfg), NarrativeStatus: "skipped"}
	switch {
	case j.agents == nil:
		v.NarrativeErrors = []string{"AI agents not configured"}
	case v.Label == "":
		v.NarrativeErrors = []string{"no verdict to explain"}
	case j.agents.CostCapUSD > 0 && j.body.CostUSD >= j.agents.CostCapUSD:
		v.NarrativeErrors = []string{fmt.Sprintf("LLM cost cap of $%.2f reached", j.agents.CostCapUSD)}
	default:
		j.narrate(ctx, v, report)
	}
	return v
}

// narrate asks the strong model for a tagline and rationale. Its only inputs
// are the verdict, the scores and their justifications.
func (j *job) narrate(ctx context.Context, v *VerdictSection, report *metrics.Report) {
	a := j.agents
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	input := map[string]any{
		"verdict":         v.Label,
		"meaning":         v.Meaning,
		"rule":            v.Rule,
		"businessScore":   deref(v.Business),
		"valuationScore":  deref(v.Valuation),
		"confidence":      v.ConfidenceLevel,
		"phase":           report.Phase.Name,
		"valuationMetric": report.Phase.ValuationMetric,
		"growthScore":     deref(report.GrowthScore.Value),
		"growthReasons":   report.GrowthScore.Reasons,
		"healthScore":     deref(report.HealthScore.Value),
		"healthReasons":   report.HealthScore.Reasons,
		"valuationReason": report.ValuationScore.Reasons,
	}
	// State the valuation zone outright so the narrative does not infer it.
	if b := report.Bands; b != nil {
		input["valuationZone"] = b.Zone // cheap / fair / expensive vs its own history
		input["valuationPercentile"] = b.Percentile
	}
	if q := j.body.BusinessQuality; q != nil {
		input["businessQualityScore"] = q.Score
		if sec := j.body.Agents[AgentQuality]; sec != nil {
			var answers map[string]struct {
				Answer        string `json:"answer"`
				Justification string `json:"justification"`
			}
			if json.Unmarshal(sec.Output, &answers) == nil {
				input["businessQuality"] = answers
			}
		}
	}

	// Same inputs → same narrative: cache by a hash of the input.
	raw, _ := json.Marshal(input)
	sum := sha256.Sum256(raw)
	key := fmt.Sprintf("agent:narrative:%s:%s:%s", a.promptFor(AgentNarrative), modelOr(j.opt, a.StrongModel), hex.EncodeToString(sum[:12])) + langSuffix(j.opt)
	if a.Cache != nil {
		if b, ok, err := a.Cache.Get(ctx, key); err == nil && ok {
			var cached VerdictSection
			if json.Unmarshal(b, &cached) == nil {
				v.Tagline, v.Rationale, v.NarrativeStatus = cached.Tagline, cached.Rationale, SectionStateReady
				return
			}
		}
	}

	t0 := time.Now()
	res, err := a.Service.Analyze(ctx, j.choose(agentclient.Task{
		JobID:         j.id,
		Section:       AgentNarrative,
		Ticker:        j.body.Company.Ticker,
		CompanyName:   j.body.Company.Name,
		PromptVersion: a.promptFor(AgentNarrative),
		Metrics:       input,
	}))
	j.mu.Lock()
	j.body.TimingsMs["agent:"+AgentNarrative] = time.Since(t0).Milliseconds()
	j.mu.Unlock()
	if err != nil {
		v.NarrativeStatus, v.NarrativeErrors = SectionStateFailed, []string{err.Error()}
		return
	}
	j.body.CostUSD += res.Usage.CostUSD
	usage := res.Usage
	v.NarrativeUsage = &usage
	if !res.OK() {
		v.NarrativeStatus, v.NarrativeErrors = SectionStateFailed, res.Errors
		return
	}
	var out struct {
		Tagline   string `json:"tagline"`
		Rationale string `json:"rationale"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		v.NarrativeStatus, v.NarrativeErrors = SectionStateFailed, []string{err.Error()}
		return
	}
	v.Tagline, v.Rationale, v.NarrativeStatus = out.Tagline, out.Rationale, SectionStateReady
	if a.Cache != nil {
		if b, err := json.Marshal(VerdictSection{Tagline: out.Tagline, Rationale: out.Rationale}); err == nil {
			_ = a.Cache.Set(ctx, key, b, 365*24*time.Hour)
		}
	}
}

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// insidersInput is the verdict's view of the year's insider trading.
func insidersInput(r *metrics.Report) *verdict.Insiders {
	s := r.Insiders
	if s == nil {
		return nil
	}
	return &verdict.Insiders{BoughtUSD: s.BoughtUSD, SoldUSD: s.SoldUSD, PlannedSoldUSD: s.PlannedSoldUSD, MarketCap: r.MarketCap.Value}
}
