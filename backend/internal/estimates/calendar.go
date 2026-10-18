package estimates

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

// CalendarEntry is a company's next expected earnings report.
type CalendarEntry struct {
	Symbol           string   `json:"symbol"`
	ReportDate       string   `json:"reportDate"`
	FiscalDateEnding string   `json:"fiscalDateEnding"`
	EstimateEPS      *float64 `json:"estimateEps"`
	Time             string   `json:"time,omitempty"` // "pre-market" / "post-market"
}

// Calendar returns every company's next earnings report in the coming three
// months, keyed by ticker (BRK.B as BRK-B, like SEC tickers). It is one
// request for the whole market, cached for 12 hours, so the free tier's
// daily quota barely notices it.
func (c *Client) Calendar(ctx context.Context) (map[string]CalendarEntry, error) {
	q := url.Values{"function": {"EARNINGS_CALENDAR"}, "horizon": {"3month"}, "apikey": {c.apiKey}}
	body, err := c.fetch.Get(ctx, fetch.Request{
		URL:      c.baseURL + "/query?" + q.Encode(),
		CacheKey: "alphavantage:calendar:3month",
		TTL:      12 * time.Hour,
		Validate: validateCalendar,
	})
	if err != nil {
		return nil, fmt.Errorf("alphavantage earnings calendar: %w", err)
	}
	return ParseCalendar(body)
}

// validateCalendar rejects the JSON notes Alpha Vantage sends instead of the
// CSV when the quota is used up.
func validateCalendar(body []byte) error {
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("symbol,")) {
		return fmt.Errorf("%w: %s", ErrRateLimited, strings.TrimSpace(string(body[:min(len(body), 200)])))
	}
	return nil
}

// ParseCalendar reads the calendar CSV
// (symbol,name,reportDate,fiscalDateEnding,estimate,currency,timeOfTheDay).
func ParseCalendar(body []byte) (map[string]CalendarEntry, error) {
	r := csv.NewReader(bytes.NewReader(body))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("alphavantage earnings calendar: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	out := map[string]CalendarEntry{}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("alphavantage earnings calendar: %w", err)
		}
		sym := strings.ToUpper(strings.ReplaceAll(get(rec, "symbol"), ".", "-"))
		e := CalendarEntry{
			Symbol: sym, ReportDate: get(rec, "reportDate"), FiscalDateEnding: get(rec, "fiscalDateEnding"),
			EstimateEPS: parseNumber(get(rec, "estimate")), Time: get(rec, "timeOfTheDay"),
		}
		if sym == "" || e.ReportDate == "" {
			continue
		}
		// The earliest date wins when a company is listed twice.
		if prev, ok := out[sym]; !ok || e.ReportDate < prev.ReportDate {
			out[sym] = e
		}
	}
	return out, nil
}
