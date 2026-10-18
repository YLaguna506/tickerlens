// Package metrics computes every number in the Financial overview, the phase
// inputs and the computed scores. All of it is plain arithmetic on reported
// data; each metric carries its inputs and formula so the UI can show how it
// was calculated.
package metrics

import (
	"fmt"
	"math"
	"sort"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/estimates"
	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/phase"
	"github.com/YLaguna506/tickerlens/backend/internal/prices"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

// Unit says how to display a value.
type Unit string

const (
	Percent  Unit = "percent"  // fraction: 0.12 means 12%
	Multiple Unit = "multiple" // 25 means 25×
	USD      Unit = "usd"
	PerShare Unit = "usd/share"
)

// Input is one number a metric was computed from.
type Input struct {
	Name   string   `json:"name"`
	Value  *float64 `json:"value"`
	Period string   `json:"period,omitempty"`
}

// Metric is one computed value with everything needed to explain it.
type Metric struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Value   *float64 `json:"value"` // nil when it cannot be computed; see Note
	Unit    Unit     `json:"unit"`
	Period  string   `json:"period,omitempty"`
	Formula string   `json:"formula,omitempty"`
	Inputs  []Input  `json:"inputs,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// Report is the full computed overview for one company.
type Report struct {
	Ticker         string `json:"ticker"`
	Name           string `json:"name"`
	ScoringVersion string `json:"scoringVersion"`

	PriceDate      string   `json:"priceDate"`
	Price          *float64 `json:"price"`
	FinancialsTo   string   `json:"financialsThrough"` // TTM end date
	LatestFiscalYr int      `json:"latestFiscalYear"`

	MarketCap     Metric   `json:"marketCap"`
	Profitability []Metric `json:"profitability"`
	Growth        []Metric `json:"growth"`
	Health        []Metric `json:"health"`
	Valuation     []Metric `json:"valuation"`
	Returns       []Metric `json:"returns"`
	PriceChange5y Metric   `json:"priceChange5y"`
	TotalReturn5y Metric   `json:"totalReturn5y"`

	Earnings EarningsRecord `json:"earnings"`
	// Industry is the SEC industry code; LimitedSupport is set for banks,
	// broker-dealers, insurers and REITs, which these metrics do not fit.
	Industry       *edgar.Profile  `json:"industry,omitempty"`
	LimitedSupport *LimitedSupport `json:"limitedSupport,omitempty"`
	// Capital is how the cash from operations was spent over five years
	// (nil without enough cash flow history).
	Capital *CapitalAllocation `json:"capitalAllocation,omitempty"`
	Phase   phase.Result       `json:"phase"`

	GrowthScore    scoring.Score `json:"growthScore"`
	HealthScore    scoring.Score `json:"healthScore"`
	ValuationScore scoring.Score `json:"valuationScore"`
	Bands          *Bands        `json:"valuationBands"`

	Charts Charts   `json:"charts"`
	Notes  []string `json:"notes,omitempty"`
}

// EarningsRecord summarises EPS actual vs analyst estimate.
type EarningsRecord struct {
	Quarters       int      `json:"quarters"`
	Beats          int      `json:"beats"`
	AvgSurprisePct *float64 `json:"avgSurprisePercent"`
	LatestQuarter  string   `json:"latestQuarter,omitempty"`
}

// Point is one value in a chart series.
type Point struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// Charts holds the series the report draws.
type Charts struct {
	Price        []Point `json:"price"` // daily close, in today's share terms
	RevenueFY    []Point `json:"revenueFY"`
	NetIncomeFY  []Point `json:"netIncomeFY"`
	ValuationTTM []Point `json:"valuation1y"` // the phase's valuation metric, last year
}

// Compute builds the Report for a bundle.
func Compute(b *ingest.Bundle, cfg scoring.Config) Report {
	f := b.Financials
	sp := splitsFrom(b.Prices)
	if b.Splits != nil {
		sp = splitsOf(b.Splits)
	}
	rolling := rollingTTM(f.Quarterly, f.Annual, sp)

	r := Report{Ticker: b.Company.Ticker, Name: b.Company.Name, ScoringVersion: cfg.Version, Industry: b.Industry}
	if b.Industry != nil {
		r.LimitedSupport = limitedSupport(b.Industry.SIC)
	}
	c := newCalc(f, rolling, sp)
	if len(b.Prices) > 0 {
		last := b.Prices[len(b.Prices)-1]
		r.PriceDate, r.Price = last.Date, ptr(last.Close)
	}
	if f.TTM != nil {
		r.FinancialsTo = f.TTM.EndDate
	}
	if n := len(f.Annual); n > 0 {
		r.LatestFiscalYr = f.Annual[n-1].FiscalYear
	}

	r.MarketCap = c.marketCap(r.Price, r.PriceDate)
	mcap := r.MarketCap.Value

	r.Profitability = c.profitability()
	r.Growth = c.growth()
	r.Health = c.health()
	r.Valuation = c.valuation(r.Price, mcap)
	if d := recentSpinOff(b, rolling); d != "" {
		for i := range r.Valuation {
			r.Valuation[i].Note = "not comparable with history: " + d
		}
	}
	r.Returns = c.returns(mcap)
	r.Capital = capitalAllocation(b.Financials, sp)
	r.PriceChange5y = priceChange(b, sp, false)
	r.TotalReturn5y = priceChange(b, sp, true)
	r.Earnings = earningsRecord(b.Earnings)

	r.Phase = phase.Classify(c.phaseInputs(r))
	r.GrowthScore = cfg.GrowthScore(map[string]*float64{
		"revenue CAGR 3y": find(r.Growth, "revenueCagr3y"),
		"revenue CAGR 5y": find(r.Growth, "revenueCagr5y"),
		"EPS CAGR 3y":     find(r.Growth, "epsCagr3y"),
		"EPS CAGR 5y":     find(r.Growth, "epsCagr5y"),
	})
	r.HealthScore = cfg.HealthScore(c.healthInputs())
	if ls := r.LimitedSupport; ls != nil && ls.NoHealthScore {
		r.HealthScore = scoring.Score{Max: 5, Reasons: []string{"not scored: debt ratios do not apply to " + ls.Kind + "s"}}
	}

	r.Bands, r.ValuationScore, r.Charts.ValuationTTM, r.Notes = valuationBands(b, cfg, r.Phase, rolling, sp, r.Price)

	for _, bar := range lastYears(b.Prices, chartYears) {
		r.Charts.Price = append(r.Charts.Price, Point{Date: bar.Date, Value: round(sp.price(bar), 4)})
	}
	for _, p := range f.Annual {
		if p.Revenue != nil {
			r.Charts.RevenueFY = append(r.Charts.RevenueFY, Point{Date: p.EndDate, Value: *p.Revenue})
		}
		if p.NetIncome != nil {
			r.Charts.NetIncomeFY = append(r.Charts.NetIncomeFY, Point{Date: p.EndDate, Value: *p.NetIncome})
		}
	}
	return r
}

// --- Shared state -------------------------------------------------------------

type calc struct {
	f         financials.Financials
	ttm       *financials.FinancialPeriod
	ttmEPS    *float64 // today's share terms
	epsPeriod string
	// valuationEPS is the EPS of the same window that P/E divides by
	// (continuing operations when they differ from the total).
	valuationEPS *float64
	continuing   bool
	shares       *float64 // today's share terms
	rolling      []ttm
	sp           splits
	period       string // "TTM to 2026-06-27"
}

func newCalc(f financials.Financials, rolling []ttm, sp splits) *calc {
	c := &calc{f: f, ttm: f.TTM, rolling: rolling, sp: sp}
	if f.TTM != nil {
		c.period = "TTM to " + f.TTM.EndDate
	}
	// EPS and share counts can be more recent than the other figures (e.g.
	// read from a 10-Q that companyfacts has not picked up yet): use the
	// latest window that has them.
	for i := len(rolling) - 1; i >= 0; i-- {
		if c.ttmEPS == nil && rolling[i].eps != nil {
			c.ttmEPS, c.epsPeriod = rolling[i].eps, "TTM to "+rolling[i].end
			c.valuationEPS, c.continuing = rolling[i].valuationEPS, rolling[i].continuing
		}
		if c.shares == nil && rolling[i].shares != nil {
			c.shares = rolling[i].shares
		}
	}
	return c
}

// interestExpense is the TTM interest expense or, for filers that tag it only
// in the 10-K (HPE), the latest fiscal year's: close enough for a coverage
// ratio. It returns the value, its period and a note when it fell back.
func (c *calc) interestExpense() (*float64, string, string) {
	if v := c.ttmValue("interestExpense"); v != nil {
		return v, c.period, ""
	}
	if fy, v := c.recentAnnual("interestExpense"); v != nil {
		return v, fmt.Sprintf("FY%d", fy.FiscalYear), fmt.Sprintf("interest expense from FY%d (not reported quarterly)", fy.FiscalYear)
	}
	return nil, c.period, ""
}

// recentAnnual returns a field from the latest fiscal year that ended no more
// than a year before the TTM end, or nil.
func (c *calc) recentAnnual(field string) (financials.FinancialPeriod, *float64) {
	if c.ttm == nil {
		return financials.FinancialPeriod{}, nil
	}
	cutoff := shiftDate(c.ttm.EndDate, -1)
	for i := len(c.f.Annual) - 1; i >= 0; i-- {
		fy := c.f.Annual[i]
		if fy.EndDate < cutoff {
			break
		}
		if v := fy.Get(field); v != nil {
			return fy, v
		}
	}
	return financials.FinancialPeriod{}, nil
}

// ttmValue returns a TTM field (balance-sheet fields are as of the TTM end).
func (c *calc) ttmValue(field string) *float64 {
	if c.ttm == nil {
		return nil
	}
	return c.ttm.Get(field)
}

func (c *calc) marketCap(price *float64, priceDate string) Metric {
	m := Metric{Key: "marketCap", Label: "Market cap", Unit: USD, Formula: "price × diluted shares",
		Period: "price on " + priceDate,
		Inputs: []Input{{Name: "price", Value: price, Period: priceDate}, {Name: "diluted shares", Value: c.shares, Period: c.period}}}
	switch {
	case price == nil:
		m.Note = "no price data"
	case c.shares == nil:
		m.Note = "diluted share count not reported"
	default:
		m.Value = ptr(*price * *c.shares)
	}
	return m
}

// --- Profitability --------------------------------------------------------------

func (c *calc) profitability() []Metric {
	rev := c.ttmValue("revenue")
	margin := func(key, label, field, name string) Metric {
		return ratio(Metric{Key: key, Label: label, Unit: Percent, Period: c.period,
			Formula: name + " ÷ revenue"}, name, c.ttmValue(field), "revenue", rev)
	}
	return []Metric{
		margin("grossMargin", "Gross margin", "grossProfit", "gross profit"),
		margin("operatingMargin", "Operating margin", "operatingIncome", "operating income"),
		margin("netMargin", "Net margin", "netIncome", "net income"),
		margin("fcfMargin", "FCF margin", "freeCashFlow", "free cash flow"),
	}
}

// --- Growth ----------------------------------------------------------------------

func (c *calc) growth() []Metric {
	var out []Metric
	for _, n := range []int{1, 3, 5, 10} {
		out = append(out, c.cagr("revenue", "Revenue", n, false))
	}
	for _, n := range []int{1, 3, 5, 10} {
		out = append(out, c.cagr("epsDiluted", "EPS", n, true))
	}
	for _, n := range []int{3, 5, 10} {
		out = append(out, c.cagr("freeCashFlow", "FCF", n, false))
	}
	return out
}

// cagr is the compound annual growth rate between two endpoints n years
// apart. It prefers trailing-twelve-month endpoints (the latest TTM vs the
// TTM n years earlier), which include the newest quarters, and falls back to
// fiscal years. It is undefined when either endpoint is ≤ 0.
func (c *calc) cagr(field, label string, years int, perShare bool) Metric {
	key := map[string]string{"revenue": "revenue", "epsDiluted": "eps", "freeCashFlow": "fcf"}[field]
	m := Metric{Key: fmt.Sprintf("%sCagr%dy", key, years), Label: fmt.Sprintf("%s CAGR %dy", label, years),
		Unit: Percent, Formula: fmt.Sprintf("(end ÷ start)^(1/%d) − 1", years)}
	if out, ok := c.ttmCAGR(m, field, years); ok {
		return out
	}

	value := func(p financials.FinancialPeriod) *float64 {
		if perShare {
			return c.sp.perShare(p, field)
		}
		return p.Get(field)
	}
	var end *financials.FinancialPeriod
	for i := len(c.f.Annual) - 1; i >= 0; i-- {
		if value(c.f.Annual[i]) != nil {
			end = &c.f.Annual[i]
			break
		}
	}
	if end == nil {
		m.Note = "not reported"
		return m
	}
	var start *financials.FinancialPeriod
	for i := range c.f.Annual {
		if c.f.Annual[i].FiscalYear == end.FiscalYear-years {
			start = &c.f.Annual[i]
		}
	}
	m.Period = fmt.Sprintf("FY%d → FY%d", end.FiscalYear-years, end.FiscalYear)
	endV := value(*end)
	var startV *float64
	if start != nil {
		startV = value(*start)
	}
	m.Inputs = []Input{
		{Name: "start", Value: startV, Period: fmt.Sprintf("FY%d", end.FiscalYear-years)},
		{Name: "end", Value: endV, Period: fmt.Sprintf("FY%d", end.FiscalYear)},
	}
	switch {
	case startV == nil:
		m.Note = fmt.Sprintf("no data for FY%d", end.FiscalYear-years)
	case *startV <= 0:
		m.Note = "starting value is zero or negative, so a growth rate is not meaningful"
	case *endV <= 0:
		m.Note = "ending value is zero or negative, so a growth rate is not meaningful"
	default:
		m.Value = ptr(math.Pow(*endV / *startV, 1/float64(years)) - 1)
	}
	return m
}

// ttmCAGR computes the growth rate from TTM windows, if both endpoints exist.
func (c *calc) ttmCAGR(m Metric, field string, years int) (Metric, bool) {
	get := func(t ttm) *float64 {
		switch field {
		case "revenue":
			return t.revenue
		case "epsDiluted":
			return t.eps
		default:
			return t.fcf
		}
	}
	end, ok := latestWith(c.rolling, func(t ttm) bool { return get(t) != nil })
	if !ok {
		return m, false
	}
	var start *ttm
	for i := range c.rolling {
		if get(c.rolling[i]) != nil && yearsApart(c.rolling[i].end, end.end, years) {
			start = &c.rolling[i]
		}
	}
	if start == nil {
		return m, false
	}
	sv, ev := *get(*start), *get(end)
	m.Period = fmt.Sprintf("TTM to %s → TTM to %s", start.end, end.end)
	m.Inputs = []Input{
		{Name: "start", Value: ptr(sv), Period: "TTM to " + start.end},
		{Name: "end", Value: ptr(ev), Period: "TTM to " + end.end},
	}
	switch {
	case sv <= 0:
		m.Note = "starting value is zero or negative, so a growth rate is not meaningful"
	case ev <= 0:
		m.Note = "ending value is zero or negative, so a growth rate is not meaningful"
	default:
		m.Value = ptr(math.Pow(ev/sv, 1/float64(years)) - 1)
	}
	return m, true
}

// --- Financial health ----------------------------------------------------------

func (c *calc) health() []Metric {
	cash, sti := c.ttmValue("cash"), c.ttmValue("shortTermInvestments")
	debt, equity := c.ttmValue("totalDebt"), c.ttmValue("totalEquity")
	balance := "balance sheet at " + c.ttmEnd()

	cashInv := Metric{Key: "cashAndInvestments", Label: "Cash & short-term investments", Unit: USD, Period: balance,
		Formula: "cash + short-term investments",
		Inputs:  []Input{{Name: "cash", Value: cash}, {Name: "short-term investments", Value: sti}}}
	switch {
	case cash == nil:
		cashInv.Note = "cash not reported"
	case sti == nil:
		cashInv.Value = ptr(*cash)
		cashInv.Note = "no short-term investments reported; cash only"
	default:
		cashInv.Value = ptr(*cash + *sti)
	}

	totalDebt := Metric{Key: "totalDebt", Label: "Total debt", Unit: USD, Period: balance, Value: debt,
		Inputs: []Input{{Name: "total debt", Value: debt}}}
	if c.ttm != nil {
		totalDebt.Formula = c.ttm.Sources["totalDebt"]
	}
	if debt == nil {
		totalDebt.Note = "no debt reported"
	}

	de := Metric{Key: "debtToEquity", Label: "Debt / equity", Unit: Multiple, Period: balance, Formula: "total debt ÷ shareholders' equity",
		Inputs: []Input{{Name: "total debt", Value: debt}, {Name: "equity", Value: equity}}}
	switch {
	case debt == nil || equity == nil:
		de.Note = "debt or equity not reported"
	case *equity <= 0:
		de.Note = "shareholders' equity is zero or negative"
	default:
		de.Value = ptr(*debt / *equity)
	}

	ebit := c.ttmValue("operatingIncome")
	interest, interestPeriod, interestNote := c.interestExpense()
	cov := Metric{Key: "interestCoverage", Label: "EBIT / interest", Unit: Multiple, Period: c.period,
		Formula: "operating income ÷ interest expense", Note: interestNote,
		Inputs: []Input{{Name: "operating income", Value: ebit, Period: c.period}, {Name: "interest expense", Value: interest, Period: interestPeriod}}}
	switch {
	case interest == nil:
		cov.Note = "interest expense not reported separately"
	case *interest <= 0:
		cov.Note = "no interest expense"
	case ebit == nil:
		cov.Note = "operating income not reported"
	default:
		cov.Value = ptr(*ebit / *interest)
	}

	fcf := c.ttmValue("freeCashFlow")
	fcfDebt := Metric{Key: "fcfToDebt", Label: "FCF / total debt", Unit: Percent, Period: c.period,
		Formula: "free cash flow ÷ total debt",
		Inputs:  []Input{{Name: "free cash flow", Value: fcf}, {Name: "total debt", Value: debt}}}
	switch {
	case debt == nil || *debt == 0:
		fcfDebt.Note = "no debt"
	case fcf == nil:
		fcfDebt.Note = "free cash flow not reported"
	default:
		fcfDebt.Value = ptr(*fcf / *debt)
	}
	return []Metric{cashInv, totalDebt, de, cov, fcfDebt}
}

func (c *calc) healthInputs() scoring.HealthInputs {
	interest, _, _ := c.interestExpense()
	in := scoring.HealthInputs{
		EBIT:            c.ttmValue("operatingIncome"),
		InterestExpense: interest,
		TotalDebt:       c.ttmValue("totalDebt"),
		Equity:          c.ttmValue("totalEquity"),
		FCF:             c.ttmValue("freeCashFlow"),
	}
	for _, p := range c.f.Annual {
		in.FCFByYear = append(in.FCFByYear, p.FreeCashFlow)
	}
	return in
}

// --- Valuation (current) -------------------------------------------------------

func (c *calc) valuation(price, mcap *float64) []Metric {
	rev, fcf, equity := c.ttmValue("revenue"), c.ttmValue("freeCashFlow"), c.ttmValue("totalEquity")
	ps := multiple(Metric{Key: "ps", Label: "P/S", Formula: "market cap ÷ revenue", Period: c.period},
		"market cap", mcap, "revenue", rev)
	pe := multiple(Metric{Key: "pe", Label: "P/E", Formula: "price ÷ diluted EPS", Period: c.epsPeriod},
		"price", price, "diluted EPS", c.ttmEPS)
	if c.continuing {
		pe = multiple(Metric{Key: "pe", Label: "P/E", Formula: "price ÷ diluted EPS from continuing operations", Period: c.epsPeriod},
			"price", price, "diluted EPS from continuing operations", c.valuationEPS)
		if c.ttmEPS != nil {
			pe.Note = fmt.Sprintf("leaves out discontinued operations; with them, diluted EPS is $%.2f", *c.ttmEPS)
		}
	}
	pb := multiple(Metric{Key: "pb", Label: "P/B", Formula: "market cap ÷ shareholders' equity", Period: "balance sheet at " + c.ttmEnd()},
		"market cap", mcap, "equity", equity)
	pfcf := multiple(Metric{Key: "pfcf", Label: "P/FCF", Formula: "market cap ÷ free cash flow", Period: c.period},
		"market cap", mcap, "free cash flow", fcf)
	return []Metric{ps, pe, pb, pfcf}
}

// --- Shareholder returns ----------------------------------------------------------

func (c *calc) returns(mcap *float64) []Metric {
	div, buy := c.ttmValue("dividendsPaid"), c.ttmValue("buybacks")
	yield := func(key, label, name string, v *float64, period string) Metric {
		return ratio(Metric{Key: key, Label: label, Unit: Percent, Period: period, Formula: name + " ÷ market cap"},
			name, v, "market cap", mcap)
	}
	divY := yield("dividendYield", "Dividend yield", "dividends paid", div, c.period)
	buyY := yield("buybackYield", "Buyback yield", "buybacks", buy, c.period)

	// Net debt paydown = repaid − issued. Use TTM when both are reported for
	// all four quarters, else the latest fiscal year.
	paydown, period := c.netDebtPaydown()
	debtY := yield("debtPaydownYield", "Debt paydown yield", "net debt repaid", paydown, period)
	if paydown != nil && *paydown < 0 {
		debtY.Note = "negative: the company borrowed more than it repaid"
	}

	total := Metric{Key: "shareholderYield", Label: "Total shareholder yield", Unit: Percent, Period: c.period,
		Formula: "dividend yield + buyback yield + debt paydown yield"}
	var sum float64
	n := 0
	for _, m := range []Metric{divY, buyY, debtY} {
		total.Inputs = append(total.Inputs, Input{Name: m.Label, Value: m.Value, Period: m.Period})
		if m.Value != nil {
			sum += *m.Value
			n++
		}
	}
	switch {
	case mcap == nil:
		total.Note = "market cap unknown"
	case n == 0:
		total.Note = "no dividends, buybacks or debt paydown reported"
	default:
		total.Value = ptr(sum)
		if n < 3 {
			total.Note = "sum of the components that are reported"
		}
	}
	return []Metric{divY, buyY, debtY, total}
}

func (c *calc) netDebtPaydown() (*float64, string) {
	if rep, iss := c.ttmValue("debtRepaid"), c.ttmValue("debtIssued"); rep != nil && iss != nil {
		return ptr(*rep - *iss), c.period
	}
	for i := len(c.f.Annual) - 1; i >= 0; i-- {
		p := c.f.Annual[i]
		if p.DebtRepaid != nil || p.DebtIssued != nil {
			return ptr(deref(p.DebtRepaid) - deref(p.DebtIssued)), fmt.Sprintf("FY%d", p.FiscalYear)
		}
	}
	return nil, c.period
}

// --- Phase inputs ----------------------------------------------------------------

func (c *calc) phaseInputs(r Report) phase.Inputs {
	in := phase.Inputs{
		RevenueTTM:       c.ttmValue("revenue"),
		NetIncomeTTM:     c.ttmValue("netIncome"),
		NetMarginTTM:     find(r.Profitability, "netMargin"),
		RevenueCAGR3y:    find(r.Growth, "revenueCagr3y"),
		ShareholderYield: nil,
	}
	// Shareholder yield for the phase rule counts dividends + buybacks only.
	if div, buy := find(r.Returns, "dividendYield"), find(r.Returns, "buybackYield"); div != nil || buy != nil {
		in.ShareholderYield = ptr(deref(div) + deref(buy))
	}
	if fcf := c.ttmValue("freeCashFlow"); fcf != nil && *fcf > 0 {
		paid := deref(c.ttmValue("dividendsPaid")) + deref(c.ttmValue("buybacks"))
		in.PayoutOfFCF = ptr(paid / *fcf)
	}

	// Year-over-year: the TTM window ending four quarters earlier.
	if cur, ok := latestWith(c.rolling, func(t ttm) bool { return t.revenue != nil }); ok {
		for _, t := range c.rolling {
			if yearApart(t.end, cur.end) {
				if t.revenue != nil && cur.revenue != nil && *t.revenue > 0 {
					in.RevenueYoY = ptr(*cur.revenue / *t.revenue - 1)
				}
				in.NetIncomeTTMYearAgo = t.netIncome
			}
		}
	}

	// Operating margin, latest fiscal year vs three years earlier.
	if n := len(c.f.Annual); n > 0 {
		last := c.f.Annual[n-1]
		in.OperatingMargin = div(last.OperatingIncome, last.Revenue)
		for _, p := range c.f.Annual {
			if p.FiscalYear == last.FiscalYear-3 {
				in.OperatingMargin3y = div(p.OperatingIncome, p.Revenue)
			}
		}
	}
	return in
}

// --- Price and earnings -------------------------------------------------------------

// priceChange is the 5-year change in share price (split-adjusted, without
// dividends), or the total return when total is true (dividends reinvested).
func priceChange(b *ingest.Bundle, sp splits, total bool) Metric {
	m := Metric{Key: "priceChange5y", Label: "5-year price change", Unit: Percent,
		Formula: "split-adjusted close today ÷ split-adjusted close 5 years ago − 1"}
	price := sp.price
	if total {
		m.Key, m.Label = "totalReturn5y", "5-year total return"
		m.Formula = "adjusted close today ÷ adjusted close 5 years ago − 1 (dividends reinvested)"
		price = func(bar prices.Bar) float64 { return bar.AdjClose }
	}
	bars := lastYears(b.Prices, chartYears)
	if len(bars) < 2 {
		m.Note = "not enough price history"
		return m
	}
	first, last := bars[0], bars[len(bars)-1]
	start, end := price(first), price(last)
	m.Period = first.Date + " → " + last.Date
	m.Inputs = []Input{{Name: "start", Value: ptr(start), Period: first.Date}, {Name: "end", Value: ptr(end), Period: last.Date}}
	if start > 0 {
		m.Value = ptr(end/start - 1)
	}
	return m
}

func earningsRecord(quarters []estimates.QuarterlyEarnings) EarningsRecord {
	recent := quarters[max(0, len(quarters)-8):]
	var r EarningsRecord
	var surprise float64
	n := 0
	for _, q := range recent {
		if q.ReportedEPS == nil || q.EstimatedEPS == nil {
			continue
		}
		r.Quarters++
		if *q.ReportedEPS > *q.EstimatedEPS {
			r.Beats++
		}
		if q.SurprisePercent != nil {
			surprise += *q.SurprisePercent
			n++
		}
		r.LatestQuarter = q.FiscalDateEnding
	}
	if n > 0 {
		r.AvgSurprisePct = ptr(surprise / float64(n))
	}
	return r
}

// --- Helpers ---------------------------------------------------------------------

// chartYears is the span of the price chart and the price-change metric.
const chartYears = 5

// lastYears returns the bars within the last n years of the series.
func lastYears(bars []prices.Bar, n int) []prices.Bar {
	if len(bars) == 0 {
		return nil
	}
	from := shiftDate(bars[len(bars)-1].Date, -n)
	i := sort.Search(len(bars), func(i int) bool { return bars[i].Date >= from })
	return bars[i:]
}

func (c *calc) ttmEnd() string {
	if c.ttm == nil {
		return "n/a"
	}
	return c.ttm.EndDate
}

// ratio fills a Metric with num ÷ den (den must be > 0).
func ratio(m Metric, numName string, num *float64, denName string, den *float64) Metric {
	m.Inputs = []Input{{Name: numName, Value: num}, {Name: denName, Value: den}}
	switch {
	case num == nil:
		m.Note = numName + " not reported"
	case den == nil:
		m.Note = denName + " not available"
	case *den <= 0:
		m.Note = denName + " is zero or negative"
	default:
		m.Value = ptr(*num / *den)
	}
	return m
}

// multiple is a valuation ratio: undefined when the denominator is ≤ 0
// (e.g. P/E of a loss-making company).
func multiple(m Metric, numName string, num *float64, denName string, den *float64) Metric {
	m.Unit = Multiple
	m = ratio(m, numName, num, denName, den)
	if m.Value == nil && den != nil && *den <= 0 {
		m.Note = denName + " is negative or zero, so the multiple is not meaningful"
	}
	return m
}

func find(ms []Metric, key string) *float64 {
	for _, m := range ms {
		if m.Key == key {
			return m.Value
		}
	}
	return nil
}

func div(a, b *float64) *float64 {
	if a == nil || b == nil || *b == 0 {
		return nil
	}
	return ptr(*a / *b)
}

func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func ptr(v float64) *float64 { return &v }

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

func round4(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return ptr(round(*v, 4))
}
