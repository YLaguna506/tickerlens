// Package edgar reads company data from SEC EDGAR: the ticker list and XBRL
// "company facts" (every number a company has tagged in its filings).
package edgar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

// ErrUnknownTicker means the ticker is not in the SEC's ticker list.
var ErrUnknownTicker = errors.New("unknown ticker")

// Company identifies an SEC filer.
type Company struct {
	Ticker string `json:"ticker"`
	CIK    string `json:"cik"` // zero-padded to 10 digits, as EDGAR URLs need
	Name   string `json:"name"`
}

// CompanyFacts is the raw companyfacts document.
type CompanyFacts struct {
	CIK        FactsCIK `json:"cik"`
	EntityName string   `json:"entityName"`
	// Facts maps taxonomy ("us-gaap", "dei") → concept name → data.
	Facts map[string]map[string]Concept `json:"facts"`
}

// FactsCIK is the CIK in a companyfacts document: a number, except for some
// new registrants, whose feed has it as a string, sometimes zero-padded
// ("2115436" for ExxonMobil Holdings, "0001858681" for Apollo in 2026).
type FactsCIK string

// UnmarshalJSON accepts a number or a string, padded or not.
func (c *FactsCIK) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	s := strings.TrimLeft(strings.Trim(string(b), `"`), "0")
	if s == "" {
		s = "0"
	}
	if _, err := strconv.ParseUint(s, 10, 64); err != nil {
		return fmt.Errorf("cik %s: %w", b, err)
	}
	*c = FactsCIK(s)
	return nil
}

func (c FactsCIK) String() string { return string(c) }

// Concept holds every reported value of one XBRL concept, grouped by unit.
type Concept struct {
	Label string            `json:"label"`
	Units map[string][]Fact `json:"units"` // "USD", "USD/shares", "shares"
}

// Fact is one reported value. A value appears once per filing that reports it,
// so the same period often shows up several times (original + comparatives).
type Fact struct {
	Start string  `json:"start,omitempty"` // empty for point-in-time (balance sheet) values
	End   string  `json:"end"`
	Val   float64 `json:"val"`
	Accn  string  `json:"accn"` // accession number of the filing
	FY    int     `json:"fy"`   // fiscal year of the *filing*, not necessarily of this value
	FP    string  `json:"fp"`   // fiscal period of the filing: FY, Q1, Q2, Q3
	Form  string  `json:"form"` // 10-K, 10-Q, 10-K/A, ...
	Filed string  `json:"filed"`
	Frame string  `json:"frame,omitempty"`
}

// Options configures a Client. Base URLs are overridable for tests.
type Options struct {
	WWWBaseURL  string // default https://www.sec.gov
	DataBaseURL string // default https://data.sec.gov
	// Cache, if set, stores parsed per-class facts from filing documents
	// (small) instead of the raw documents (several MB each).
	Cache fetch.Cache
}

// Client talks to SEC EDGAR. The fetch.Client passed in must send a
// User-Agent with contact details and be rate-limited to ≤ 10 req/s.
type Client struct {
	fetch *fetch.Client
	opts  Options
}

// NewClient returns an EDGAR client.
func NewClient(f *fetch.Client, opts Options) *Client {
	if opts.WWWBaseURL == "" {
		opts.WWWBaseURL = "https://www.sec.gov"
	}
	if opts.DataBaseURL == "" {
		opts.DataBaseURL = "https://data.sec.gov"
	}
	return &Client{fetch: f, opts: opts}
}

// tickerEntry matches one row of company_tickers.json.
type tickerEntry struct {
	CIK    int    `json:"cik_str"`
	Ticker string `json:"ticker"`
	Title  string `json:"title"`
}

// LookupTicker resolves a ticker such as "V" or "brk.b" to its SEC company.
func (c *Client) LookupTicker(ctx context.Context, ticker string) (Company, error) {
	companies, err := c.Tickers(ctx)
	if err != nil {
		return Company{}, err
	}
	want := NormalizeTicker(ticker)
	for _, co := range companies {
		if co.Ticker == want {
			return co, nil
		}
	}
	return Company{}, fmt.Errorf("%w: %s", ErrUnknownTicker, want)
}

// Tickers returns every company in the SEC ticker list.
func (c *Client) Tickers(ctx context.Context) ([]Company, error) {
	body, err := c.fetch.Get(ctx, fetch.Request{
		URL:      c.opts.WWWBaseURL + "/files/company_tickers.json",
		CacheKey: "sec:company_tickers",
		TTL:      24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("sec ticker list: %w", err)
	}
	// The file is a JSON object keyed "0", "1", ... rather than an array.
	var raw map[string]tickerEntry
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("sec ticker list: decode: %w", err)
	}
	// Keep the file's order (by market value), so a company's main listing
	// comes before its other share classes and foreign lines.
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.Atoi(keys[i])
		b, _ := strconv.Atoi(keys[j])
		return a < b
	})
	companies := make([]Company, 0, len(raw))
	for _, k := range keys {
		e := raw[k]
		companies = append(companies, Company{
			Ticker: strings.ToUpper(e.Ticker),
			CIK:    PadCIK(e.CIK),
			Name:   e.Title,
		})
	}
	return companies, nil
}

// CompanyFacts returns every XBRL fact the company has filed.
func (c *Client) CompanyFacts(ctx context.Context, cik string) (*CompanyFacts, error) {
	body, err := c.fetch.Get(ctx, fetch.Request{
		URL:      fmt.Sprintf("%s/api/xbrl/companyfacts/CIK%s.json", c.opts.DataBaseURL, cik),
		CacheKey: "sec:companyfacts:" + cik,
		// New filings arrive at most a few times a quarter; a day is fresh enough.
		TTL: 24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("sec companyfacts %s: %w", cik, err)
	}
	var facts CompanyFacts
	if err := json.Unmarshal(body, &facts); err != nil {
		return nil, fmt.Errorf("sec companyfacts %s: decode: %w", cik, err)
	}
	return &facts, nil
}

// NormalizeTicker upper-cases a ticker and uses "-" for share classes, as the
// SEC list does ("brk.b" → "BRK-B").
func NormalizeTicker(t string) string {
	return strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(t)), ".", "-")
}

// PadCIK formats a CIK as the 10-digit string EDGAR URLs use.
func PadCIK(cik int) string {
	return fmt.Sprintf("%010d", cik)
}
