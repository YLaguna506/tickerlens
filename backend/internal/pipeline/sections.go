package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/peers"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

// The moat, risk and management agents.
const (
	AgentMoat       = "moat"
	AgentRisk       = "risk"
	AgentManagement = "management"
)

// ProxyFilings finds proxy statements (edgar.Client). Optional: without it
// the management section has no CEO facts.
type ProxyFilings interface {
	LatestProxy(ctx context.Context, cik string) (edgar.Filing, error)
}

// ProxyReader extracts a proxy statement's ownership table and CEO passages
// (agentclient.Client).
type ProxyReader interface {
	Proxy(ctx context.Context, html []byte) (map[string]string, error)
}

// MoatScore is the moat agent's judgment as numbers: width and direction
// each 1–5 (None … Wide, Narrowing … Widening).
type MoatScore struct {
	Width     float64               `json:"width"`
	WidthName string                `json:"widthName"`
	Direction float64               `json:"direction"`
	DirName   string                `json:"directionName"`
	Sources   map[string]MoatRating `json:"sources"`
}

// MoatRating is one moat source's rating and trend.
type MoatRating struct {
	Rating    string `json:"rating"`    // None | Weak | Moderate | Strong
	Direction string `json:"direction"` // Narrowing | Stable | Widening
}

var (
	moatWidth     = map[string]float64{"None": 1, "Thin": 2, "Narrow": 3, "Broad": 4, "Wide": 5}
	moatDirection = map[string]float64{"Narrowing": 1, "Slightly narrowing": 2, "Stable": 3, "Slightly widening": 4, "Widening": 5}
)

func moatScore(output json.RawMessage) (*MoatScore, error) {
	var raw struct {
		Width     string                `json:"width"`
		Direction string                `json:"direction"`
		Sources   map[string]MoatRating `json:"sources"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("moat output: %w", err)
	}
	w, ok1 := moatWidth[raw.Width]
	d, ok2 := moatDirection[raw.Direction]
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("moat: invalid width %q or direction %q", raw.Width, raw.Direction)
	}
	return &MoatScore{Width: w, WidthName: raw.Width, Direction: d, DirName: raw.Direction, Sources: raw.Sources}, nil
}

// RiskScore combines four risk questions, each 1–5 where 5 is the least
// risky: three judged by the risk agent from the 10-K, and financial health
// from the balance sheet (the health score).
type RiskScore struct {
	// Score is the average of the questions answered (5 = lowest risk).
	Score   float64            `json:"score"`
	Answers map[string]string  `json:"answers"`
	Points  map[string]float64 `json:"points"`
}

var riskPoints = map[string]map[string]float64{
	"revenueDiversification": {"Very concentrated": 1, "Concentrated": 2, "Moderate": 3, "Diversified": 4, "Very diversified": 5},
	"disruptionThreat":       {"Severe": 1, "High": 2, "Moderate": 3, "Low": 4, "Minimal": 5},
	"externalExposure":       {"Severe": 1, "High": 2, "Moderate": 3, "Low": 4, "Minimal": 5},
}

func riskScore(output json.RawMessage, health scoring.Score) (*RiskScore, error) {
	var answers map[string]struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(output, &answers); err != nil {
		return nil, fmt.Errorf("risk output: %w", err)
	}
	r := &RiskScore{Answers: map[string]string{}, Points: map[string]float64{}}
	var sum float64
	for question, scale := range riskPoints {
		a := answers[question].Answer
		pts, ok := scale[a]
		if !ok {
			return nil, fmt.Errorf("risk: missing or invalid answer for %s", question)
		}
		r.Answers[question], r.Points[question] = a, pts
		sum += pts
	}
	n := float64(len(riskPoints))
	if health.Value != nil {
		r.Points["financialHealth"] = *health.Value
		sum += *health.Value
		n++
	}
	r.Score = math.Round(sum/n*100) / 100
	return r, nil
}

// ManagementInfo is what the proxy statement says about the CEO, with the
// ownership valued at today's price.
type ManagementInfo struct {
	CEOName            string     `json:"ceoName"`
	CEOSince           *int       `json:"ceoSince"`
	TenureYears        *int       `json:"tenureYears"`
	SharesOwned        *float64   `json:"sharesOwned"`
	OwnedValueUSD      *float64   `json:"ownedValueUsd"` // shares × latest price
	PercentOfClass     *float64   `json:"percentOfClass"`
	LessThanOnePercent bool       `json:"lessThanOnePercent"`
	GroupPercent       *float64   `json:"directorsAndOfficersPercent"`
	Notes              string     `json:"notes,omitempty"`
	Proxy              *FilingRef `json:"proxy,omitempty"`
}

func managementInfo(output json.RawMessage, price *float64, now time.Time) (*ManagementInfo, error) {
	var raw struct {
		CEOName                     string   `json:"ceoName"`
		CEOSince                    *int     `json:"ceoSince"`
		SharesOwned                 *float64 `json:"sharesOwned"`
		PercentOfClass              *float64 `json:"percentOfClass"`
		LessThanOnePercent          bool     `json:"lessThanOnePercent"`
		DirectorsAndOfficersPercent *float64 `json:"directorsAndOfficersPercent"`
		Notes                       string   `json:"notes"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("management output: %w", err)
	}
	if raw.CEOName == "" {
		return nil, fmt.Errorf("management: no CEO named")
	}
	m := &ManagementInfo{
		CEOName: raw.CEOName, CEOSince: raw.CEOSince, SharesOwned: raw.SharesOwned,
		PercentOfClass: raw.PercentOfClass, LessThanOnePercent: raw.LessThanOnePercent,
		GroupPercent: raw.DirectorsAndOfficersPercent, Notes: raw.Notes,
	}
	if s := raw.CEOSince; s != nil {
		if *s < 1950 || *s > now.Year() {
			m.CEOSince = nil
		} else {
			tenure := now.Year() - *s
			m.TenureYears = &tenure
		}
	}
	if m.SharesOwned != nil && price != nil {
		v := *m.SharesOwned * *price
		m.OwnedValueUSD = &v
	}
	return m, nil
}

// peerFacts are the peer comparison numbers the moat agent may cite.
func peerFacts(g *peers.Group) map[string]any {
	if g == nil {
		return nil
	}
	f := map[string]any{
		"peerGroup":     fmt.Sprintf("%s (%s), calendar year %d", g.Industry, g.Basis, g.Year),
		"peerGroupSize": g.Of,
		"peers":         peerNames(g),
	}
	if g.Rank > 0 {
		f["revenueRank"] = g.Rank
	}
	put := func(key string, v *float64) {
		if v != nil {
			f[key] = math.Round(*v*10000) / 10000
		}
	}
	m := g.Medians
	put("peerMedianRevenue", m.Revenue)
	put("peerMedianRevenueGrowth", m.RevenueGrowth)
	put("peerMedianGrossMargin", m.GrossMargin)
	put("peerMedianOperatingMargin", m.OperatingMargin)
	put("peerMedianNetMargin", m.NetMargin)
	if c := g.Company; c != nil {
		put("companyRevenue", &c.Revenue)
		put("companyRevenueGrowth", c.RevenueGrowth)
		put("companyGrossMargin", c.GrossMargin)
		put("companyOperatingMargin", c.OperatingMargin)
		put("companyNetMargin", c.NetMargin)
	}
	return f
}

func peerNames(g *peers.Group) []string {
	names := make([]string, 0, len(g.Peers))
	for _, p := range g.Peers {
		names = append(names, p.Name)
	}
	return names
}
