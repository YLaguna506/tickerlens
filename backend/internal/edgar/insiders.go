package edgar

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

// InsiderTrade is one transaction in a company's stock reported on a Form 4
// by a director, officer or 10% owner.
type InsiderTrade struct {
	Accession string `json:"accession"`
	Filed     string `json:"filed"`
	Name      string `json:"name"`
	Role      string `json:"role"` // e.g. "Chief Executive Officer", "Director", "10% owner"
	Date      string `json:"date"`
	// Code is the SEC transaction code: P open-market purchase, S open-market
	// sale, A grant, M option exercise, F tax withholding, G gift, ...
	Code       string   `json:"code"`
	Shares     float64  `json:"shares"`
	Price      float64  `json:"price"`
	Acquired   bool     `json:"acquired"`
	OwnedAfter *float64 `json:"ownedAfter"`
	// Planned: made under a Rule 10b5-1 trading plan set up in advance
	// (reported on filings since 2023).
	Planned bool `json:"planned"`
}

// form4 is the part of a Form 4 XML document TickerLens reads.
type form4 struct {
	Owners []struct {
		Name string `xml:"reportingOwnerId>rptOwnerName"`
		Rel  struct {
			Director   string `xml:"isDirector"`
			Officer    string `xml:"isOfficer"`
			TenPercent string `xml:"isTenPercentOwner"`
			Title      string `xml:"officerTitle"`
		} `xml:"reportingOwnerRelationship"`
	} `xml:"reportingOwner"`
	Plan   string `xml:"aff10b5One"`
	Trades []struct {
		Date   string `xml:"transactionDate>value"`
		Code   string `xml:"transactionCoding>transactionCode"`
		Shares string `xml:"transactionAmounts>transactionShares>value"`
		Price  string `xml:"transactionAmounts>transactionPricePerShare>value"`
		AD     string `xml:"transactionAmounts>transactionAcquiredDisposedCode>value"`
		After  string `xml:"postTransactionAmounts>sharesOwnedFollowingTransaction>value"`
	} `xml:"nonDerivativeTable>nonDerivativeTransaction"`
}

// ParseForm4 reads the common-stock transactions in a Form 4 document.
func ParseForm4(doc []byte, f Filing) ([]InsiderTrade, error) {
	var x form4
	if err := xml.Unmarshal(doc, &x); err != nil {
		return nil, err
	}
	if len(x.Owners) == 0 {
		return nil, nil
	}
	o := x.Owners[0]
	var roles []string
	if t := strings.TrimSpace(o.Rel.Title); t != "" && flag(o.Rel.Officer) {
		roles = append(roles, t)
	} else if flag(o.Rel.Officer) {
		roles = append(roles, "Officer")
	}
	if flag(o.Rel.Director) {
		roles = append(roles, "Director")
	}
	if flag(o.Rel.TenPercent) {
		roles = append(roles, "10% owner")
	}
	var out []InsiderTrade
	for _, t := range x.Trades {
		shares, _ := strconv.ParseFloat(strings.TrimSpace(t.Shares), 64)
		price, _ := strconv.ParseFloat(strings.TrimSpace(t.Price), 64)
		tr := InsiderTrade{
			Accession: f.Accession, Filed: f.FilingDate,
			Name: strings.TrimSpace(o.Name), Role: strings.Join(roles, ", "),
			Date: strings.TrimSpace(t.Date), Code: strings.TrimSpace(t.Code),
			Shares: shares, Price: price, Acquired: strings.TrimSpace(t.AD) == "A",
			Planned: flag(x.Plan),
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(t.After), 64); err == nil {
			tr.OwnedAfter = &v
		}
		out = append(out, tr)
	}
	return out, nil
}

func flag(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "1" || s == "true"
}

// InsiderTrades reads the company's Form 4 filings since a date (newest
// first, at most max of them) and returns their common-stock transactions.
// Each parsed filing is cached for good.
func (c *Client) InsiderTrades(ctx context.Context, cik, since string, max int) ([]InsiderTrade, int, error) {
	filings, err := c.recentFilings(ctx, cik, "4")
	if err != nil {
		return nil, 0, err
	}
	var recent []Filing
	for _, f := range filings { // newest first
		if f.FilingDate < since || len(recent) >= max {
			break
		}
		recent = append(recent, f)
	}

	var (
		mu  sync.Mutex
		out []InsiderTrade
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4) // the SEC rate limiter paces the requests; this overlaps their latency
	for _, f := range recent {
		g.Go(func() error {
			trades, err := c.form4Trades(gctx, cik, f)
			if err != nil {
				return err
			}
			mu.Lock()
			out = append(out, trades...)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, 0, err
	}
	return out, len(recent), nil
}

func (c *Client) form4Trades(ctx context.Context, cik string, f Filing) ([]InsiderTrade, error) {
	key := "sec:form4:v1:" + f.Accession
	if c.opts.Cache != nil {
		if b, ok, err := c.opts.Cache.Get(ctx, key); err == nil && ok {
			var trades []InsiderTrade
			if json.Unmarshal(b, &trades) == nil {
				return trades, nil
			}
		}
	}
	// The listed document is the XSL-rendered view ("xslF345X06/form4.xml");
	// the raw XML has the same name one directory up.
	raw := f.PrimaryDocument[strings.LastIndex(f.PrimaryDocument, "/")+1:]
	body, err := c.fetch.Get(ctx, fetch.Request{URL: fmt.Sprintf("%s/Archives/edgar/data/%s/%s/%s",
		c.opts.WWWBaseURL, strings.TrimLeft(cik, "0"), strings.ReplaceAll(f.Accession, "-", ""), raw)})
	if err != nil {
		return nil, fmt.Errorf("sec form 4 %s: %w", f.Accession, err)
	}
	trades, err := ParseForm4(body, f)
	if err != nil {
		return nil, fmt.Errorf("sec form 4 %s: %w", f.Accession, err)
	}
	if c.opts.Cache != nil {
		if b, err := json.Marshal(trades); err == nil {
			_ = c.opts.Cache.Set(ctx, key, b, 10*365*24*time.Hour)
		}
	}
	return trades, nil
}
