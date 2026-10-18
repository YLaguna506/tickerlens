package ingest

import (
	"math"
	"sort"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
)

// Splits lists the stock splits in daily prices, oldest first (spin-offs,
// which Tiingo also records as split factors, are left out).
func Splits(bars []prices.Bar) []financials.Split {
	var out []financials.Split
	for _, b := range bars {
		if b.IsSplit() {
			out = append(out, financials.Split{Date: b.Date, Factor: b.SplitFactor})
		}
	}
	return out
}

// SplitsFor lists the stock splits in prices plus those only the filings
// show. Tiingo can record a split made on the same day as a spin-off as one
// combined factor (Honeywell's 1-for-2 reverse split with its aerospace
// spin-off on 2026-06-29 appears as 0.9535), which is not a split ratio.
// The filings still show it: a later filing restates an earlier period's
// EPS and share count by inverse ratios.
func SplitsFor(bars []prices.Bar, facts *edgar.CompanyFacts) []financials.Split {
	out := Splits(bars)
	for _, s := range filingSplits(facts) {
		known := false
		for _, k := range out {
			if k.Date > s.after && k.Date <= s.by && math.Abs(k.Factor/s.factor-1) < 0.01 {
				known = true
			}
		}
		if known {
			continue
		}
		// The split took effect between the last filing in old share terms
		// and the first in new ones. A combined factor in prices in that
		// window gives the day; otherwise the first new filing stands in.
		date := s.by
		for _, b := range bars {
			if b.Date > s.after && b.Date <= s.by && b.SplitFactor > 0 && b.SplitFactor != 1 {
				date = b.Date
				break
			}
		}
		out = append(out, financials.Split{Date: date, Factor: s.factor})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// filingSplit is a split seen in restated filings: it took effect after the
// filing date `after` and on or before `by`.
type filingSplit struct {
	factor    float64
	after, by string
}

// filingSplits finds periods whose diluted EPS and diluted share count were
// restated by inverse split ratios (EPS × 2 and shares ÷ 2 is a 1-for-2
// reverse split, factor 0.5).
func filingSplits(cf *edgar.CompanyFacts) []filingSplit {
	gaap := cf.Facts["us-gaap"]
	type period struct{ start, end string }
	versions := func(concept, unit string) map[period][]edgar.Fact {
		m := map[period][]edgar.Fact{}
		for _, f := range gaap[concept].Units[unit] {
			if f.Start != "" && (f.Form == "10-K" || f.Form == "10-Q" || f.Form == "10-K/A" || f.Form == "10-Q/A") {
				p := period{f.Start, f.End}
				m[p] = append(m[p], f)
			}
		}
		for _, fs := range m {
			sort.Slice(fs, func(i, j int) bool { return fs[i].Filed < fs[j].Filed })
		}
		return m
	}
	eps := versions("EarningsPerShareDiluted", "USD/shares")
	shares := versions("WeightedAverageNumberOfDilutedSharesOutstanding", "shares")

	var out []filingSplit
	for p, es := range eps {
		for i := 1; i < len(es); i++ {
			old, cur := es[i-1], es[i]
			if math.Abs(old.Val) < 0.05 || old.Val*cur.Val <= 0 || old.Filed == cur.Filed {
				continue
			}
			factor := old.Val / cur.Val // shares multiply by this
			if !(prices.Bar{SplitFactor: factor}).IsSplit() {
				continue
			}
			oldShares, curShares, ok := sharesFiled(shares[p], old.Filed, cur.Filed)
			if !ok || math.Abs(curShares/oldShares/factor-1) > 0.02 {
				continue
			}
			out = mergeSplit(out, filingSplit{factor: factor, after: old.Filed, by: cur.Filed})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].by < out[j].by })
	return out
}

// sharesFiled returns a period's share count as last filed on or before
// oldFiled and as first filed on or after curFiled. 10-Ks restate EPS for
// earlier quarters without their share counts, so the dates can differ.
func sharesFiled(fs []edgar.Fact, oldFiled, curFiled string) (old, cur float64, ok bool) {
	var hasOld, hasCur bool
	for _, f := range fs { // oldest first
		if f.Filed <= oldFiled {
			old, hasOld = f.Val, true
		}
		if f.Filed >= curFiled && !hasCur {
			cur, hasCur = f.Val, true
		}
	}
	return old, cur, hasOld && hasCur && old > 0
}

// mergeSplit narrows the window of a split already seen (the same factor,
// overlapping dates) or adds a new one.
func mergeSplit(list []filingSplit, s filingSplit) []filingSplit {
	for i, k := range list {
		if math.Abs(k.factor/s.factor-1) < 0.01 && s.after < k.by && k.after < s.by {
			list[i].after, list[i].by = max(k.after, s.after), min(k.by, s.by)
			return list
		}
	}
	return append(list, s)
}
