// Package estimates fetches quarterly EPS actuals vs analyst estimates from
// Alpha Vantage's EARNINGS endpoint.
package estimates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

// Provider is the name used for logs and the daily budget.
const Provider = "alphavantage"

// DailyLimit is Alpha Vantage's free-tier quota.
const DailyLimit = 25

var (
	// ErrNoData means Alpha Vantage has no earnings for the symbol.
	ErrNoData = errors.New("alphavantage: no earnings data")
	// ErrRateLimited means Alpha Vantage refused the call because of its quota.
	ErrRateLimited = errors.New("alphavantage: rate limit reached")
)

// QuarterlyEarnings is one reported quarter. Nil means "not available".
type QuarterlyEarnings struct {
	FiscalDateEnding string   `json:"fiscalDateEnding"`
	ReportedDate     string   `json:"reportedDate"`
	ReportedEPS      *float64 `json:"reportedEPS"`
	EstimatedEPS     *float64 `json:"estimatedEPS"`
	Surprise         *float64 `json:"surprise"`
	SurprisePercent  *float64 `json:"surprisePercent"`
	ReportTime       string   `json:"reportTime,omitempty"` // "pre-market" / "post-market"
}

// Client calls Alpha Vantage.
type Client struct {
	fetch   *fetch.Client
	apiKey  string
	baseURL string
}

// NewClient returns a client. baseURL is empty except in tests.
func NewClient(f *fetch.Client, apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://www.alphavantage.co"
	}
	return &Client{fetch: f, apiKey: apiKey, baseURL: baseURL}
}

// rawResponse mirrors the JSON. Alpha Vantage sends numbers as strings and
// reports errors inside a 200 response.
type rawResponse struct {
	Symbol    string `json:"symbol"`
	Quarterly []struct {
		FiscalDateEnding   string `json:"fiscalDateEnding"`
		ReportedDate       string `json:"reportedDate"`
		ReportedEPS        string `json:"reportedEPS"`
		EstimatedEPS       string `json:"estimatedEPS"`
		Surprise           string `json:"surprise"`
		SurprisePercentage string `json:"surprisePercentage"`
		ReportTime         string `json:"reportTime"`
	} `json:"quarterlyEarnings"`
	Information  string `json:"Information"`
	Note         string `json:"Note"`
	ErrorMessage string `json:"Error Message"`
}

// Earnings returns quarterly EPS history, oldest first.
func (c *Client) Earnings(ctx context.Context, ticker string) ([]QuarterlyEarnings, error) {
	q := url.Values{"function": {"EARNINGS"}, "symbol": {ticker}, "apikey": {c.apiKey}}
	body, err := c.fetch.Get(ctx, fetch.Request{
		URL: c.baseURL + "/query?" + q.Encode(),
		// The key is left out of the cache key on purpose: it is a secret.
		CacheKey: "alphavantage:earnings:" + ticker,
		// Earnings change once a quarter, and the free tier allows 25 calls a day.
		TTL:      7 * 24 * time.Hour,
		Validate: validate,
	})
	if err != nil {
		return nil, fmt.Errorf("alphavantage earnings %s: %w", ticker, err)
	}

	var raw rawResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("alphavantage earnings %s: decode: %w", ticker, err)
	}
	out := make([]QuarterlyEarnings, 0, len(raw.Quarterly))
	for _, r := range raw.Quarterly {
		out = append(out, QuarterlyEarnings{
			FiscalDateEnding: r.FiscalDateEnding,
			ReportedDate:     r.ReportedDate,
			ReportedEPS:      parseNumber(r.ReportedEPS),
			EstimatedEPS:     parseNumber(r.EstimatedEPS),
			Surprise:         parseNumber(r.Surprise),
			SurprisePercent:  parseNumber(r.SurprisePercentage),
			ReportTime:       r.ReportTime,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FiscalDateEnding < out[j].FiscalDateEnding })
	return out, nil
}

// validate rejects error responses before they are cached.
func validate(body []byte) error {
	var raw rawResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("alphavantage: decode: %w", err)
	}
	switch {
	case raw.Information != "" || raw.Note != "":
		return fmt.Errorf("%w: %s", ErrRateLimited, firstNonEmpty(raw.Information, raw.Note))
	case raw.ErrorMessage != "":
		return fmt.Errorf("%w: %s", ErrNoData, raw.ErrorMessage)
	case len(raw.Quarterly) == 0:
		return ErrNoData
	}
	return nil
}

// Budget counts requests against a daily quota. store.Usage implements it.
type Budget interface {
	Take(ctx context.Context, provider string, day time.Time, limit int) (bool, error)
}

// BudgetHook returns a fetch.Options.BeforeNetwork hook that stops requests
// once the daily quota is used. Cached responses do not count.
func BudgetHook(b Budget, limit int, now func() time.Time) func(context.Context) error {
	return func(ctx context.Context) error {
		ok, err := b.Take(ctx, Provider, now().UTC(), limit)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("alphavantage: %d requests used today: %w", limit, fetch.ErrBudgetExhausted)
		}
		return nil
	}
}

func parseNumber(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "None" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
