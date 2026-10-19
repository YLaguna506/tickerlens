// Package prices fetches daily price history from Tiingo.
package prices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

// IsSplit reports whether the bar's split factor is a stock split: a ratio of
// small whole numbers (2-for-1, 3-for-2, 1-for-10). Tiingo also records
// spin-offs as split factors (Honeywell's 1.061 in 2025), which change no
// share counts.
func (b Bar) IsSplit() bool {
	f := b.SplitFactor
	if f <= 0 || f == 1 {
		return false
	}
	for q := 1.0; q <= 10; q++ {
		for _, x := range []float64{f, 1 / f} {
			if p := math.Round(x * q); p >= 1 && p != q && math.Abs(p/q-x) < 0.002*x {
				return true
			}
		}
	}
	return false
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
	// End-of-day data changes once per trading day.
	return t.get(ctx, ticker, from, "tiingo:daily:"+ticker+":"+from, 12*time.Hour)
}

// History returns daily bars from start, cached for ttl. The backtest uses
// it with a fixed start and a long ttl: it needs years of history, not
// today's close, and the free tier allows 500 symbols a month.
func (t *Tiingo) History(ctx context.Context, ticker string, start time.Time, ttl time.Duration) ([]Bar, error) {
	from := start.Format(time.DateOnly)
	return t.get(ctx, ticker, from, "tiingo:history:"+ticker+":"+from, ttl)
}

func (t *Tiingo) get(ctx context.Context, ticker, from, cacheKey string, ttl time.Duration) ([]Bar, error) {
	body, err := t.fetch.Get(ctx, fetch.Request{
		URL:      fmt.Sprintf("%s/tiingo/daily/%s/prices?startDate=%s", t.baseURL, url.PathEscape(ticker), from),
		CacheKey: cacheKey,
		TTL:      ttl,
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
