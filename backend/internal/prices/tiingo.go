// Package prices fetches daily price history from Tiingo.
package prices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

// ErrUnknownTicker means Tiingo has no prices for the ticker.
var ErrUnknownTicker = errors.New("tiingo: unknown ticker")

// Bar is one trading day.
type Bar struct {
	Date        string  `json:"date"`     // YYYY-MM-DD
	Close       float64 `json:"close"`    // as traded
	AdjClose    float64 `json:"adjClose"` // adjusted for splits and dividends
	DivCash     float64 `json:"divCash"`
	SplitFactor float64 `json:"splitFactor"`
}

// Tiingo is a client for Tiingo's end-of-day API. The fetch.Client must send
// "Authorization: Token <key>".
type Tiingo struct {
	fetch   *fetch.Client
	baseURL string
}

// NewTiingo returns a Tiingo client. baseURL is empty except in tests.
func NewTiingo(f *fetch.Client, baseURL string) *Tiingo {
	if baseURL == "" {
		baseURL = "https://api.tiingo.com"
	}
	return &Tiingo{fetch: f, baseURL: baseURL}
}

// DailyPrices returns daily bars from start until the latest close, oldest first.
func (t *Tiingo) DailyPrices(ctx context.Context, ticker string, start time.Time) ([]Bar, error) {
	from := start.Format(time.DateOnly)
	body, err := t.fetch.Get(ctx, fetch.Request{
		URL:      fmt.Sprintf("%s/tiingo/daily/%s/prices?startDate=%s", t.baseURL, url.PathEscape(ticker), from),
		CacheKey: "tiingo:daily:" + ticker + ":" + from,
		// End-of-day data changes once per trading day.
		TTL: 12 * time.Hour,
	})
	var statusErr *fetch.StatusError
	if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTicker, ticker)
	}
	if err != nil {
		return nil, fmt.Errorf("tiingo prices %s: %w", ticker, err)
	}

	var bars []Bar
	if err := json.Unmarshal(body, &bars); err != nil {
		return nil, fmt.Errorf("tiingo prices %s: decode: %w", ticker, err)
	}
	for i := range bars {
		// Tiingo sends "2026-09-28T00:00:00.000Z"; keep the date part.
		if len(bars[i].Date) >= 10 {
			bars[i].Date = bars[i].Date[:10]
		}
	}
	return bars, nil
}
