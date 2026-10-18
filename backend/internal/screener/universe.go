package screener

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// sp500URL is a public dataset of the S&P 500's members, updated as the
// index changes (github.com/datasets/s-and-p-500-companies).
const sp500URL = "https://raw.githubusercontent.com/datasets/s-and-p-500-companies/main/data/constituents.csv"

// sp500Snapshot is the same list as of October 2026, used when the dataset
// cannot be fetched.
//
//go:embed sp500.csv
var sp500Snapshot []byte

// SP500 returns the S&P 500's members, fetched at most once a week, or the
// bundled snapshot when the dataset cannot be reached (client may be nil).
func SP500(ctx context.Context, client *fetch.Client) []store.ScreenMember {
	if client != nil {
		body, err := client.Get(ctx, fetch.Request{
			URL:      sp500URL,
			CacheKey: "universe:sp500",
			TTL:      7 * 24 * time.Hour,
			Validate: validateConstituents,
		})
		if err == nil {
			if members, err := ParseConstituents(body); err == nil && len(members) > 400 {
				return members
			}
		}
	}
	members, _ := ParseConstituents(sp500Snapshot)
	return members
}

func validateConstituents(body []byte) error {
	if !bytes.HasPrefix(body, []byte("Symbol,")) {
		return errors.New("not the constituents CSV")
	}
	return nil
}

// ParseConstituents reads the dataset's CSV (Symbol, Security, GICS Sector,
// …). Share classes use the SEC's form: BRK.B becomes BRK-B.
func ParseConstituents(body []byte) ([]store.ScreenMember, error) {
	r := csv.NewReader(bytes.NewReader(body))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("constituents: %w", err)
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
	var out []store.ScreenMember
	seen := map[string]bool{}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("constituents: %w", err)
		}
		t := edgar.NormalizeTicker(get(rec, "Symbol"))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, store.ScreenMember{Ticker: t, Name: get(rec, "Security"), Sector: get(rec, "GICS Sector")})
	}
	return out, nil
}

// MaxTickers bounds a custom list.
const MaxTickers = 600

var tickerSep = regexp.MustCompile(`[\s,;]+`)

// ParseTickers reads a typed list ("AAPL, msft brk.b"), without duplicates.
func ParseTickers(text string) []store.ScreenMember {
	var out []store.ScreenMember
	for _, raw := range tickerSep.Split(text, -1) {
		t := edgar.NormalizeTicker(raw)
		if t == "" || len(t) > 10 || slices.ContainsFunc(out, func(m store.ScreenMember) bool { return m.Ticker == t }) {
			continue
		}
		out = append(out, store.ScreenMember{Ticker: t})
		if len(out) == MaxTickers {
			break
		}
	}
	return out
}
