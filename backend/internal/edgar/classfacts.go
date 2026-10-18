package edgar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

// Synthetic concept names for per-class values merged into CompanyFacts.
// The normalizer lists them as last-resort fallbacks, and the name shows up
// as the value's source.
const (
	ClassEPSConcept    = "EarningsPerShareDiluted [per share class, from filing XBRL]"
	ClassSharesConcept = "WeightedAverageNumberOfDilutedSharesOutstanding [per share class, from filing XBRL]"
)

const classAxis = "StatementClassOfStockAxis"

// Filing is one entry in a company's filing history.
type Filing struct {
	Accession       string `json:"accession"`
	Form            string `json:"form"`
	ReportDate      string `json:"reportDate"`
	FilingDate      string `json:"filingDate"`
	PrimaryDocument string `json:"primaryDocument"`
	InlineXBRL      bool   `json:"inlineXBRL"`
}

// Filings returns the company's recent 10-K and 10-Q filings, newest first.
func (c *Client) Filings(ctx context.Context, cik string) ([]Filing, error) {
	return c.recentFilings(ctx, cik, "10-K", "10-Q")
}

// submissions is the part of the company's submissions document TickerLens
// reads. The "recent" block is column-oriented: parallel arrays, one per field.
type submissions struct {
	SIC            string `json:"sic"`
	SICDescription string `json:"sicDescription"`
	Filings        struct {
		Recent struct {
			Accession       []string `json:"accessionNumber"`
			Form            []string `json:"form"`
			ReportDate      []string `json:"reportDate"`
			FilingDate      []string `json:"filingDate"`
			PrimaryDocument []string `json:"primaryDocument"`
			InlineXBRL      []int    `json:"isInlineXBRL"`
		} `json:"recent"`
	} `json:"filings"`
}

func (c *Client) submissions(ctx context.Context, cik string) (*submissions, error) {
	body, err := c.fetch.Get(ctx, fetch.Request{
		URL:      fmt.Sprintf("%s/submissions/CIK%s.json", c.opts.DataBaseURL, cik),
		CacheKey: "sec:submissions:" + cik,
		TTL:      24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("sec submissions %s: %w", cik, err)
	}
	var raw submissions
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("sec submissions %s: decode: %w", cik, err)
	}
	return &raw, nil
}

// recentFilings returns the company's recent filings of the given forms,
// newest first.
func (c *Client) recentFilings(ctx context.Context, cik string, forms ...string) ([]Filing, error) {
	raw, err := c.submissions(ctx, cik)
	if err != nil {
		return nil, err
	}
	r := raw.Filings.Recent
	var out []Filing
	for i := range r.Accession {
		if i >= len(r.Form) || i >= len(r.ReportDate) || i >= len(r.PrimaryDocument) {
			break
		}
		if !slices.Contains(forms, r.Form[i]) {
			continue
		}
		f := Filing{
			Accession:       r.Accession[i],
			Form:            r.Form[i],
			ReportDate:      r.ReportDate[i],
			PrimaryDocument: r.PrimaryDocument[i],
		}
		if i < len(r.FilingDate) {
			f.FilingDate = r.FilingDate[i]
		}
		if i < len(r.InlineXBRL) {
			f.InlineXBRL = r.InlineXBRL[i] == 1
		}
		out = append(out, f)
	}
	return out, nil
}

// InstanceFacts returns the per-share-class EPS and diluted share facts in one
// filing. Parsed results are cached for good: a filing never changes.
func (c *Client) InstanceFacts(ctx context.Context, cik string, f Filing) ([]DimFact, error) {
	// v2: basic EPS and shares are read too.
	cacheKey := "sec:classfacts:v2:" + f.Accession
	if c.opts.Cache != nil {
		if body, ok, err := c.opts.Cache.Get(ctx, cacheKey); err == nil && ok {
			var facts []DimFact
			if json.Unmarshal(body, &facts) == nil {
				return facts, nil
			}
		}
	}

	url, err := c.instanceURL(ctx, cik, f)
	if err != nil {
		return nil, err
	}
	// The raw document (several MB) is not cached; only the parsed facts are.
	body, err := c.fetch.Get(ctx, fetch.Request{URL: url})
	if err != nil {
		return nil, fmt.Errorf("sec xbrl instance %s: %w", f.Accession, err)
	}
	all, err := ParseInstance(bytes.NewReader(body), classConcepts)
	if err != nil {
		return nil, fmt.Errorf("sec xbrl instance %s: %w", f.Accession, err)
	}
	var facts []DimFact
	for _, d := range all {
		// Keep facts split only by share class (one dimension, the class axis).
		if len(d.Dims) == 1 && d.Dims[classAxis] != "" {
			facts = append(facts, d)
		}
	}

	if c.opts.Cache != nil {
		if b, err := json.Marshal(facts); err == nil {
			_ = c.opts.Cache.Set(ctx, cacheKey, b, 10*365*24*time.Hour)
		}
	}
	return facts, nil
}

// instanceURL finds the XBRL instance document of a filing. Inline XBRL
// filings (2019 onwards) publish it as <primary document>_htm.xml; older ones
// are found through the filing's index.
func (c *Client) instanceURL(ctx context.Context, cik string, f Filing) (string, error) {
	base := fmt.Sprintf("%s/Archives/edgar/data/%s/%s/", c.opts.WWWBaseURL,
		strings.TrimLeft(cik, "0"), strings.ReplaceAll(f.Accession, "-", ""))
	if f.InlineXBRL && strings.HasSuffix(f.PrimaryDocument, ".htm") {
		return base + strings.TrimSuffix(f.PrimaryDocument, ".htm") + "_htm.xml", nil
	}

	body, err := c.fetch.Get(ctx, fetch.Request{
		URL:      base + "index.json",
		CacheKey: "sec:index:" + f.Accession,
		TTL:      10 * 365 * 24 * time.Hour,
	})
	if err != nil {
		return "", fmt.Errorf("sec filing index %s: %w", f.Accession, err)
	}
	var idx struct {
		Directory struct {
			Item []struct {
				Name string `json:"name"`
			} `json:"item"`
		} `json:"directory"`
	}
	if err := json.Unmarshal(body, &idx); err != nil {
		return "", fmt.Errorf("sec filing index %s: decode: %w", f.Accession, err)
	}
	for _, it := range idx.Directory.Item {
		n := it.Name
		if !strings.HasSuffix(n, ".xml") || n == "FilingSummary.xml" {
			continue
		}
		if strings.HasSuffix(n, "_cal.xml") || strings.HasSuffix(n, "_def.xml") ||
			strings.HasSuffix(n, "_lab.xml") || strings.HasSuffix(n, "_pre.xml") {
			continue
		}
		return base + n, nil
	}
	return "", fmt.Errorf("sec filing %s: no XBRL instance found", f.Accession)
}

// How far back to read filings for per-class values: quarterly EPS for
// 5-year valuation history (plus one year for the first TTM), and 10-Ks for
// 10-year growth (each 10-K holds three years).
const (
	classQuarterlyYears = 6
	classAnnualYears    = 10
)

// ClassFacts reads per-share-class EPS and diluted shares from the company's
// recent 10-K and 10-Q filings. Filings that cannot be read are skipped and
// reported in notes, so one bad document does not lose the rest.
func (c *Client) ClassFacts(ctx context.Context, cik, ticker string, now time.Time) (eps, shares []Fact, notes []string, err error) {
	filings, err := c.Filings(ctx, cik)
	if err != nil {
		return nil, nil, nil, err
	}
	quarterlyFrom := now.AddDate(-classQuarterlyYears, 0, 0).Format(time.DateOnly)
	annualFrom := now.AddDate(-classAnnualYears, 0, 0).Format(time.DateOnly)

	used := map[string]bool{}
	for _, f := range filings {
		if (f.Form == "10-Q" && f.ReportDate < quarterlyFrom) || (f.Form == "10-K" && f.ReportDate < annualFrom) {
			continue
		}
		facts, err := c.InstanceFacts(ctx, cik, f)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, nil, ctx.Err()
			}
			notes = append(notes, fmt.Sprintf("skipped %s %s: %v", f.Form, f.ReportDate, err))
			continue
		}
		e, s, how := SelectClass(f, facts, ClassOf(ticker))
		eps = append(eps, e...)
		shares = append(shares, s...)
		if how != "" {
			used[how] = true
		}
	}
	for how := range used {
		notes = append(notes, "per-class EPS and diluted shares read from filing XBRL: "+how)
	}
	sort.Strings(notes)
	return eps, shares, notes, nil
}

// The per-class concepts read from filings. Basic figures stand in when a
// filing has no diluted ones: Berkshire Hathaway has nothing dilutive and
// reports basic EPS only.
var classConcepts = []string{
	"EarningsPerShareDiluted", "WeightedAverageNumberOfDilutedSharesOutstanding",
	"EarningsPerShareBasic", "WeightedAverageNumberOfSharesOutstandingBasic",
}

// ClassOf returns the share class a ticker trades as, from its suffix
// ("BRK-B" → "B"), or "" for none.
func ClassOf(ticker string) string {
	t := strings.ToUpper(ticker)
	if len(t) > 2 && (t[len(t)-2] == '-' || t[len(t)-2] == '.') {
		return t[len(t)-1:]
	}
	return ""
}

// classLetter names a class member's class: "A", "B", "C", "common" for
// plain common stock, or "" for anything else. Members are matched by their
// ending, so company-specific ones count (Berkshire's EquivalentClassBMember).
func classLetter(member string) string {
	switch {
	case member == "CommonStockMember":
		return "common"
	case strings.HasSuffix(member, "ClassAMember"):
		return "A"
	case strings.HasSuffix(member, "ClassBMember"):
		return "B"
	case strings.HasSuffix(member, "ClassCMember"):
		return "C"
	}
	return ""
}

// SelectClass picks one EPS value (and matching share count) per period from
// per-class facts:
//   - every class has the same EPS (e.g. Alphabet): use it, and sum the share
//     counts of all classes;
//   - EPS differs by class: use the class the ticker trades as (prefer, e.g.
//     "B" for BRK-B), else Class A (Visa), else the plain common stock beside
//     a Class B (Hershey);
//   - otherwise: skip, rather than guess.
//
// Diluted figures are used where the filing has them, basic ones otherwise.
func SelectClass(filing Filing, facts []DimFact, prefer string) (eps, shares []Fact, note string) {
	type period struct{ start, end string }
	byKind := map[string]map[period]map[string]float64{}
	for _, d := range facts {
		p := period{d.Start, d.End}
		if byKind[d.Concept] == nil {
			byKind[d.Concept] = map[period]map[string]float64{}
		}
		if byKind[d.Concept][p] == nil {
			byKind[d.Concept][p] = map[string]float64{}
		}
		byKind[d.Concept][p][d.Dims[classAxis]] = d.Val
	}
	dilutedEPS, basicEPS := byKind["EarningsPerShareDiluted"], byKind["EarningsPerShareBasic"]
	dilutedShares, basicShares := byKind["WeightedAverageNumberOfDilutedSharesOutstanding"], byKind["WeightedAverageNumberOfSharesOutstandingBasic"]

	periods := map[period]bool{}
	for p := range dilutedEPS {
		periods[p] = true
	}
	for p := range basicEPS {
		periods[p] = true
	}
	ordered := make([]period, 0, len(periods))
	for p := range periods {
		ordered = append(ordered, p)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].end < ordered[j].end })

	mk := func(p period, v float64) Fact {
		return Fact{Start: p.start, End: p.end, Val: v, Accn: filing.Accession, Form: filing.Form, Filed: filing.FilingDate}
	}
	order := []string{prefer, "A", "common"}
	used := map[string]bool{}
	for _, p := range ordered {
		byClass, sharesBy, basic := dilutedEPS[p], dilutedShares[p], false
		if len(byClass) == 0 {
			byClass, sharesBy, basic = basicEPS[p], basicShares[p], true
		}
		if len(sharesBy) == 0 && basic {
			sharesBy = basicShares[p]
		}
		suffix := ""
		if basic {
			suffix = " (basic EPS: no dilutive securities reported)"
		}
		if v, ok := sameValue(byClass); ok {
			eps = append(eps, mk(p, v))
			if total, ok := sum(sharesBy); ok {
				shares = append(shares, mk(p, total))
			}
			used["all classes (same EPS)"+suffix] = true
			continue
		}
	pick:
		for _, want := range order {
			if want == "" {
				continue
			}
			for member, v := range byClass {
				if classLetter(member) != want {
					continue
				}
				eps = append(eps, mk(p, v))
				if s, ok := sharesBy[member]; ok {
					shares = append(shares, mk(p, s))
				}
				name := "Class " + want
				if want == "common" {
					name = "common stock"
				}
				used[name+suffix] = true
				break pick
			}
		}
	}
	var names []string
	for k := range used {
		names = append(names, k)
	}
	sort.Strings(names)
	return eps, shares, strings.Join(names, ", ")
}

// sameValue reports the common value when every class has the same one
// (ignoring classes reported as exactly zero, e.g. a class with no shares yet).
func sameValue(m map[string]float64) (float64, bool) {
	var first float64
	n := 0
	for _, v := range m {
		if v == 0 {
			continue
		}
		if n == 0 {
			first = v
		} else if math.Abs(v-first) > 0.005 {
			return 0, false
		}
		n++
	}
	return first, n > 1
}

func sum(m map[string]float64) (float64, bool) {
	if len(m) == 0 {
		return 0, false
	}
	var total float64
	for _, v := range m {
		total += v
	}
	return total, true
}

// MergeClassFacts adds per-class EPS and share facts to a CompanyFacts
// document under the synthetic ClassEPSConcept / ClassSharesConcept names.
func MergeClassFacts(cf *CompanyFacts, eps, shares []Fact) {
	if cf.Facts == nil {
		cf.Facts = map[string]map[string]Concept{}
	}
	if cf.Facts["us-gaap"] == nil {
		cf.Facts["us-gaap"] = map[string]Concept{}
	}
	if len(eps) > 0 {
		cf.Facts["us-gaap"][ClassEPSConcept] = Concept{Units: map[string][]Fact{"USD/shares": eps}}
	}
	if len(shares) > 0 {
		cf.Facts["us-gaap"][ClassSharesConcept] = Concept{Units: map[string][]Fact{"shares": shares}}
	}
}
