package edgar

import (
	"context"
	"fmt"
	"strings"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

// LatestAnnualReport returns the company's most recent 10-K.
func (c *Client) LatestAnnualReport(ctx context.Context, cik string) (Filing, error) {
	filings, err := c.Filings(ctx, cik)
	if err != nil {
		return Filing{}, err
	}
	for _, f := range filings { // newest first
		if f.Form == "10-K" {
			return f, nil
		}
	}
	return Filing{}, fmt.Errorf("sec: no 10-K found for CIK %s", cik)
}

// DocumentURL is the address of a filing's primary document (the 10-K itself).
func (c *Client) DocumentURL(cik string, f Filing) string {
	return fmt.Sprintf("%s/Archives/edgar/data/%s/%s/%s", c.opts.WWWBaseURL,
		strings.TrimLeft(cik, "0"), strings.ReplaceAll(f.Accession, "-", ""), f.PrimaryDocument)
}

// PrimaryDocument downloads a filing's primary document (HTML, often several
// MB). It is not cached; callers cache what they extract from it.
func (c *Client) PrimaryDocument(ctx context.Context, cik string, f Filing) ([]byte, error) {
	body, err := c.fetch.Get(ctx, fetch.Request{URL: c.DocumentURL(cik, f)})
	if err != nil {
		return nil, fmt.Errorf("sec document %s: %w", f.Accession, err)
	}
	return body, nil
}
