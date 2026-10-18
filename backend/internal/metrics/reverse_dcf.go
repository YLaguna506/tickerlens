package metrics

import (
	"fmt"
	"math"

	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

// ReverseDCF works a discounted cash flow model backwards: instead of
// estimating a value from a growth forecast, it finds the growth the market
// price already assumes, and compares it with the company's actual growth.
type ReverseDCF struct {
	// ImpliedGrowth is the yearly free cash flow growth over Years that makes
	// the cash flows, discounted at DiscountRate, worth the market cap (nil
	// when the model does not apply; see Note).
	ImpliedGrowth  *float64 `json:"impliedGrowth"`
	Years          int      `json:"years"`
	DiscountRate   float64  `json:"discountRate"`
	TerminalGrowth float64  `json:"terminalGrowth"`
	MarketCap      *float64 `json:"marketCap"`
	BaseFCF        *float64 `json:"baseFcf"`
	BasePeriod     string   `json:"basePeriod,omitempty"`
	// History is the company's own record to compare with.
	History []Input `json:"history"`
	// Reference is the growth the implied rate is judged against: the 5-year
	// revenue growth (steadier than FCF), else FCF, else EPS growth.
	Reference     *float64 `json:"reference"`
	ReferenceName string   `json:"referenceName,omitempty"`
	// JudgedAgainst is Reference, capped at the configured ceiling (few
	// companies keep growing faster for 10 years).
	JudgedAgainst *float64 `json:"judgedAgainst"`
	Capped        bool     `json:"capped,omitempty"`
	// Expectations: "low", "in line", "high" or "very high" against
	// JudgedAgainst.
	Expectations string `json:"expectations,omitempty"`
	// VAdjustment is the change it made to the valuation score V.
	VAdjustment float64 `json:"vAdjustment"`
	// solved: the implied growth is inside the search range.
	solved bool
	Note   string `json:"note,omitempty"`
}

// Bisection limits for the implied growth.
const (
	minImplied = -0.5
	maxImplied = 1.0
)

// dcfValue is what Years of cash flows starting at fcf and growing at g,
// plus a terminal value growing at gt, are worth today at discount rate r.
func dcfValue(fcf, g, r, gt float64, years int) float64 {
	var pv float64
	cf := fcf
	for t := 1; t <= years; t++ {
		cf *= 1 + g
		pv += cf / math.Pow(1+r, float64(t))
	}
	terminal := cf * (1 + gt) / (r - gt)
	return pv + terminal/math.Pow(1+r, float64(years))
}

// impliedGrowth solves dcfValue(g) = target by bisection (the value rises
// with g). ok is false when the answer is outside the search range; the
// bound it hit is returned.
func impliedGrowth(fcf, target, r, gt float64, years int) (float64, bool) {
	lo, hi := minImplied, maxImplied
	if dcfValue(fcf, lo, r, gt, years) > target {
		return lo, false
	}
	if dcfValue(fcf, hi, r, gt, years) < target {
		return hi, false
	}
	for range 100 {
		mid := (lo + hi) / 2
		if dcfValue(fcf, mid, r, gt, years) < target {
			lo = mid
		} else {
			hi = mid
		}
	}
	return math.Round((lo+hi)/2*10000) / 10000, true
}

func (c *calc) reverseDCF(mcap *float64, growth []Metric, ls *LimitedSupport, cfg scoring.ReverseDCFRules) *ReverseDCF {
	if cfg.Years <= 0 || cfg.DiscountRate <= cfg.TerminalGrowth {
		return nil
	}
	d := &ReverseDCF{Years: cfg.Years, DiscountRate: cfg.DiscountRate, TerminalGrowth: cfg.TerminalGrowth, MarketCap: mcap}
	for _, h := range []struct{ key, name string }{
		{"revenueCagr5y", "revenue growth, 5 years"},
		{"fcfCagr5y", "free cash flow growth, 5 years"},
		{"epsCagr5y", "EPS growth, 5 years"},
		{"revenueCagr3y", "revenue growth, 3 years"},
	} {
		if v := find(growth, h.key); v != nil {
			d.History = append(d.History, Input{Name: h.name, Value: v})
			if d.Reference == nil && h.key != "revenueCagr3y" {
				d.Reference, d.ReferenceName = v, h.name
			}
		}
	}

	switch {
	case ls != nil:
		d.Note = fmt.Sprintf("not meaningful for %ss: free cash flow means something different for them", ls.Kind)
		return d
	case mcap == nil || *mcap <= 0:
		d.Note = "no market cap"
		return d
	}
	fcf, period := c.ttmValue("freeCashFlow"), c.period
	if fcf == nil || *fcf <= 0 {
		// A bad year: use the last three fiscal years' average if positive.
		if avg, label, ok := averageFCF(c.f.Annual, 3); ok {
			fcf, period = &avg, label
		} else {
			d.Note = "free cash flow was negative or not reported, so there is nothing to grow"
			return d
		}
	}
	d.BaseFCF, d.BasePeriod = fcf, period

	g, ok := impliedGrowth(*fcf, *mcap, cfg.DiscountRate, cfg.TerminalGrowth, cfg.Years)
	d.ImpliedGrowth, d.solved = &g, ok
	if !ok {
		if g <= minImplied {
			d.Note = fmt.Sprintf("the price is below what today's cash flow is worth even if it shrank %.0f%% a year", -minImplied*100)
		} else {
			d.Note = fmt.Sprintf("the price needs over %.0f%% yearly growth", maxImplied*100)
		}
	}
	if d.Reference != nil {
		ref := *d.Reference
		if cfg.ReferenceCap > 0 && ref > cfg.ReferenceCap {
			ref, d.Capped = cfg.ReferenceCap, true
		}
		d.JudgedAgainst = &ref
		gap := g - ref
		switch {
		case gap <= -cfg.InLineGap:
			d.Expectations = "low"
		case gap <= cfg.InLineGap:
			d.Expectations = "in line"
		case gap <= cfg.HighGap:
			d.Expectations = "high"
		default:
			d.Expectations = "very high"
		}
	}
	return d
}

// averageFCF is the mean free cash flow of the last n fiscal years, when all
// of them report it and the mean is positive.
func averageFCF(annual []financials.FinancialPeriod, n int) (float64, string, bool) {
	if len(annual) < n {
		return 0, "", false
	}
	last := annual[len(annual)-n:]
	var sum float64
	for _, y := range last {
		if y.FreeCashFlow == nil {
			return 0, "", false
		}
		sum += *y.FreeCashFlow
	}
	avg := sum / float64(n)
	if avg <= 0 {
		return 0, "", false
	}
	return avg, fmt.Sprintf("average of FY%d–FY%d", last[0].FiscalYear, last[n-1].FiscalYear), true
}

// adjustValuation moves V by the reverse DCF's expectations level, within
// 1–5, and says so in V's reasons. Nothing changes without a V or when the
// implied growth was out of range.
func adjustValuation(v *scoring.Score, d *ReverseDCF, cfg scoring.ReverseDCFRules) {
	if v.Value == nil || d == nil || !d.solved || d.Expectations == "" || d.ImpliedGrowth == nil || d.JudgedAgainst == nil {
		return
	}
	pts := cfg.VPoints(d.Expectations)
	if pts == 0 {
		return
	}
	before := *v.Value
	after := math.Min(math.Max(before+pts, 1), 5)
	if after == before {
		return
	}
	d.VAdjustment = after - before
	v.Value = &after
	v.Reasons = append(v.Reasons, fmt.Sprintf("reverse DCF: the price implies %.1f%% yearly growth against %.1f%%, expectations %s → %+g (%g → %g)",
		*d.ImpliedGrowth*100, *d.JudgedAgainst*100, d.Expectations, d.VAdjustment, before, after))
}
