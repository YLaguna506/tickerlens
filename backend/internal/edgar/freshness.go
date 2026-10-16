package edgar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

// FactsByConcept is concept → unit → facts, the shape of CompanyFacts.
type FactsByConcept map[string]map[string][]Fact

// FreshFacts reads the company's latest 10-K or 10-Q directly when the
// companyfacts feed has not caught up with it yet (it can lag a filing by
// weeks). knownThrough is the latest period end companyfacts has. ok is false
// when the feed is already current, in which case nothing is downloaded.
//
// unitFor maps a concept to the unit it is stored under ("USD",
// "USD/shares", "shares"); instance documents name units by local ids.
func (c *Client) FreshFacts(ctx context.Context, cik, knownThrough string, concepts []string,
	unitFor func(concept string) string) (Filing, FactsByConcept, bool, error) {
	filings, err := c.Filings(ctx, cik)
	if err != nil {
		return Filing{}, nil, false, err
	}
	var latest Filing
	for _, f := range filings { // newest first
		if f.Form == "10-Q" || f.Form == "10-K" {
			latest = f
			break
		}
	}
	if latest.Accession == "" || latest.ReportDate <= knownThrough {
		return latest, nil, false, nil
	}

	facts, info, err := c.filingFacts(ctx, cik, latest, concepts)
	if err != nil {
		return latest, nil, false, err
	}
	out := FactsByConcept{}
	for _, d := range facts {
		if len(d.Dims) > 0 {
			continue // only plain company-wide values, like companyfacts
		}
		unit := unitFor(d.Concept)
		if out[d.Concept] == nil {
			out[d.Concept] = map[string][]Fact{}
		}
		out[d.Concept][unit] = append(out[d.Concept][unit], Fact{
			Start: d.Start, End: d.End, Val: d.Val,
			Accn: latest.Accession, FY: info.FiscalYear, FP: info.FiscalPeriod,
			Form: latest.Form, Filed: latest.FilingDate,
		})
	}
	return latest, out, true, nil
}

type parsedFiling struct {
	Facts []DimFact    `json:"facts"`
	Info  DocumentInfo `json:"info"`
}

// filingFacts parses a filing's instance document, caching the result for
// good (a filing never changes).
func (c *Client) filingFacts(ctx context.Context, cik string, f Filing, concepts []string) ([]DimFact, DocumentInfo, error) {
	key := "sec:filingfacts:v1:" + f.Accession
	if c.opts.Cache != nil {
		if b, ok, err := c.opts.Cache.Get(ctx, key); err == nil && ok {
			var p parsedFiling
			if json.Unmarshal(b, &p) == nil {
				return p.Facts, p.Info, nil
			}
		}
	}
	url, err := c.instanceURL(ctx, cik, f)
	if err != nil {
		return nil, DocumentInfo{}, err
	}
	body, err := c.fetch.Get(ctx, fetch.Request{URL: url})
	if err != nil {
		return nil, DocumentInfo{}, fmt.Errorf("sec xbrl instance %s: %w", f.Accession, err)
	}
	facts, info, err := ParseFiling(bytes.NewReader(body), concepts)
	if err != nil {
		return nil, info, fmt.Errorf("sec xbrl instance %s: %w", f.Accession, err)
	}
	if c.opts.Cache != nil {
		if b, err := json.Marshal(parsedFiling{Facts: facts, Info: info}); err == nil {
			_ = c.opts.Cache.Set(ctx, key, b, 10*365*24*time.Hour)
		}
	}
	return facts, info, nil
}

// MergeFacts adds facts to a CompanyFacts document. The normalizer keeps the
// most recently filed value per period, so overlapping values are fine.
func MergeFacts(cf *CompanyFacts, add FactsByConcept) {
	if cf.Facts == nil {
		cf.Facts = map[string]map[string]Concept{}
	}
	gaap := cf.Facts["us-gaap"]
	if gaap == nil {
		gaap = map[string]Concept{}
		cf.Facts["us-gaap"] = gaap
	}
	for concept, units := range add {
		c := gaap[concept]
		if c.Units == nil {
			c.Units = map[string][]Fact{}
		}
		for unit, facts := range units {
			c.Units[unit] = append(c.Units[unit], facts...)
		}
		gaap[concept] = c
	}
}
