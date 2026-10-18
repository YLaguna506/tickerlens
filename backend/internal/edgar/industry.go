package edgar

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

// Profile is what the SEC records about a company beyond its filings.
type Profile struct {
	SIC            string `json:"sic"`            // Standard Industrial Classification, e.g. "2080"
	SICDescription string `json:"sicDescription"` // e.g. "Beverages"
}

// Profile returns the company's industry code.
func (c *Client) Profile(ctx context.Context, cik string) (Profile, error) {
	raw, err := c.submissions(ctx, cik)
	if err != nil {
		return Profile{}, err
	}
	return Profile{SIC: raw.SIC, SICDescription: raw.SICDescription}, nil
}

// LatestProxy returns the company's most recent proxy statement (DEF 14A),
// which reports executive ownership and biographies.
func (c *Client) LatestProxy(ctx context.Context, cik string) (Filing, error) {
	filings, err := c.recentFilings(ctx, cik, "DEF 14A")
	if err != nil {
		return Filing{}, err
	}
	if len(filings) == 0 {
		return Filing{}, fmt.Errorf("sec: no proxy statement (DEF 14A) found for CIK %s", cik)
	}
	return filings[0], nil
}

var cikRE = regexp.MustCompile(`<cik>(\d+)</cik>`)

// maxSICPages caps the company list read for one industry code (100 a page;
// prepackaged software has ~1,800 companies).
const maxSICPages = 25

// CompaniesBySIC lists the CIKs of every company EDGAR files under an
// industry code, active or not. Pages are cached for a week.
func (c *Client) CompaniesBySIC(ctx context.Context, sic string) ([]string, error) {
	var out []string
	for page := range maxSICPages {
		start := page * 100
		body, err := c.fetch.Get(ctx, fetch.Request{
			URL: fmt.Sprintf("%s/cgi-bin/browse-edgar?action=getcompany&SIC=%s&type=10-K&dateb=&owner=include&count=100&start=%d&output=atom",
				c.opts.WWWBaseURL, sic, start),
			CacheKey: fmt.Sprintf("sec:sic:%s:%d", sic, start),
			TTL:      7 * 24 * time.Hour,
		})
		if err != nil {
			return nil, fmt.Errorf("sec companies with SIC %s: %w", sic, err)
		}
		found := cikRE.FindAllSubmatch(body, -1)
		for _, m := range found {
			out = append(out, string(m[1]))
		}
		if len(found) < 100 {
			break
		}
	}
	return out, nil
}

// FrameValue is one company's value in a frame.
type FrameValue struct {
	Name string  `json:"entityName"`
	Val  float64 `json:"val"`
	End  string  `json:"end"`
}

// Frame returns every filer's value of a us-gaap USD concept for a calendar
// year (the SEC aligns each company's fiscal year to the nearest one), keyed
// by zero-padded CIK.
func (c *Client) Frame(ctx context.Context, concept string, year int) (map[string]FrameValue, error) {
	body, err := c.fetch.Get(ctx, fetch.Request{
		URL:      fmt.Sprintf("%s/api/xbrl/frames/us-gaap/%s/USD/CY%d.json", c.opts.DataBaseURL, concept, year),
		CacheKey: fmt.Sprintf("sec:frame:%s:%d", concept, year),
		TTL:      7 * 24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("sec frame %s CY%d: %w", concept, year, err)
	}
	var raw struct {
		Data []struct {
			CIK int `json:"cik"`
			FrameValue
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("sec frame %s CY%d: decode: %w", concept, year, err)
	}
	out := make(map[string]FrameValue, len(raw.Data))
	for _, d := range raw.Data {
		out[PadCIK(d.CIK)] = d.FrameValue
	}
	return out, nil
}
