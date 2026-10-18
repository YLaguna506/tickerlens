package metrics

import (
	"fmt"
	"math"

	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/phase"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
)

// Sector is the analysis for banks, broker-dealers, insurers and REITs, whose
// accounts the operating-company metrics do not fit. Each kind is read on its
// own terms: banks, brokers and insurers on the return they earn on their
// equity and the price paid for that equity (P/B); REITs on funds from
// operations (FFO), which add back the depreciation of their buildings.
type Sector struct {
	Kind  string `json:"kind"`  // bank | broker | insurer | reit
	About string `json:"about"` // how this kind of company is read
	// Metrics are the kind's own measures (keys: roe, efficiencyRatio, ffo…).
	Metrics []Metric `json:"metrics"`
	// ValuationMetric is the multiple the price is judged by (P/B or P/FFO).
	ValuationMetric string `json:"valuationMetric"`
	// ScoreName names the score standing in for financial health.
	ScoreName string `json:"scoreName,omitempty"`
	// Trend is return on equity, or FFO per share, by fiscal year.
	Trend *Trend `json:"trend,omitempty"`
}

// Trend is one measure by fiscal year.
type Trend struct {
	Label  string  `json:"label"`
	Unit   Unit    `json:"unit"`
	Points []Point `json:"points"`
}

var sectorAbout = map[string]string{
	"bank": "Banks lend out their depositors' money, so interest is their revenue and deposits their main debt. " +
		"They are read on what they earn on their shareholders' equity (ROE), how cheaply they run (efficiency ratio) " +
		"and how much equity backs their assets, and valued on book value (P/B).",
	"broker": "Broker-dealers fund trading and client accounts with borrowed money, so debt ratios do not apply. " +
		"They are read on what they earn on their shareholders' equity (ROE) and how much equity backs their assets, " +
		"and valued on book value (P/B).",
	"insurer": "Insurers collect premiums now and pay claims later, investing the money in between. " +
		"They are read on what they earn on their shareholders' equity (ROE), how much of their premiums go to claims " +
		"and how their book value grows, and valued on book value (P/B).",
	"reit": "REITs own property and must pay out most of their income. Depreciation of their buildings makes earnings look low, " +
		"so they are read on funds from operations (FFO: net income + real estate depreciation and impairments − gains on property sales) " +
		"and valued on price to FFO (P/FFO).",
}

// sectorKind returns the kind for an industry code, or "".
func sectorKind(sic string) string { return limitedSIC[sic] }

// sector computes the kind's metrics and trend.
func (c *calc) sector(kind string, price *float64) *Sector {
	s := &Sector{Kind: kind, About: sectorAbout[kind], ValuationMetric: phase.PB}
	switch kind {
	case "reit":
		s.ValuationMetric, s.ScoreName = phase.PFFO, "Financial strength"
		s.Metrics = c.reitMetrics(price)
		s.Trend = c.ffoPerShareTrend()
		return s
	case "insurer":
		s.ScoreName = "Financial strength"
		s.Metrics = append(c.equityMetrics(), c.insurerMetrics()...)
	default:
		s.ScoreName = "Financial strength"
		s.Metrics = append(c.equityMetrics(), c.bankMetrics()...)
	}
	s.Trend = c.roeTrend()
	return s
}

// usable reports whether the analysis has its core number; without it the
// report falls back to the limited-support notice.
func (s *Sector) usable() bool {
	if s.Kind == "reit" {
		return find(s.Metrics, "ffo") != nil
	}
	return find(s.Metrics, "roe") != nil
}

// valuationCandidates are the multiples tried, in order, for the bands.
func (s *Sector) valuationCandidates() []string {
	if s.Kind == "reit" {
		return []string{phase.PFFO, phase.PS}
	}
	return []string{phase.PB, phase.PE}
}

// --- Banks, brokers and insurers ---------------------------------------------------

// equityMetrics are shared by banks, brokers and insurers.
func (c *calc) equityMetrics() []Metric {
	ni, niName := c.ttmValue("netIncomeCommon"), "net income to common shareholders"
	if ni == nil {
		ni, niName = c.ttmValue("netIncome"), "net income"
	}
	equity, assets := c.ttmValue("totalEquity"), c.ttmValue("totalAssets")
	var common *float64
	if c.ttm != nil {
		common = commonEquity(*c.ttm)
	}
	avgCommon, eqHow := c.averageBalance(commonEquity)
	avgAssets, asHow := c.averageBalance(func(p financials.FinancialPeriod) *float64 { return p.TotalAssets })
	balance := "balance sheet at " + c.ttmEnd()

	roe := ratio(Metric{Key: "roe", Label: "Return on equity", Unit: Percent, Period: c.period,
		Formula: niName + " ÷ average common equity (" + eqHow + ")"}, niName, ni, "average common equity", avgCommon)
	roa := ratio(Metric{Key: "roa", Label: "Return on assets", Unit: Percent, Period: c.period,
		Formula: "net income ÷ average total assets (" + asHow + ")"}, "net income", c.ttmValue("netIncome"), "average total assets", avgAssets)
	eqAssets := ratio(Metric{Key: "equityToAssets", Label: "Equity ÷ assets", Unit: Percent, Period: balance,
		Formula: "shareholders' equity ÷ total assets"}, "equity", equity, "total assets", assets)
	bvps := ratio(Metric{Key: "bookValuePerShare", Label: "Book value per share", Unit: PerShare, Period: balance,
		Formula: "common equity (shareholders' equity − preferred stock) ÷ diluted shares"}, "common equity", common, "diluted shares", c.shares)
	return []Metric{roe, roa, eqAssets, bvps, c.bookValueCAGR(5)}
}

func (c *calc) bankMetrics() []Metric {
	nii, fees, costs := c.ttmValue("netInterestIncome"), c.ttmValue("noninterestIncome"), c.ttmValue("noninterestExpense")
	var revenue *float64
	if nii != nil && fees != nil {
		revenue = ptr(*nii + *fees)
	}
	avgAssets, asHow := c.averageBalance(func(p financials.FinancialPeriod) *float64 { return p.TotalAssets })
	balance := "balance sheet at " + c.ttmEnd()

	margin := ratio(Metric{Key: "niiToAssets", Label: "Net interest income ÷ assets", Unit: Percent, Period: c.period,
		Formula: "net interest income ÷ average total assets (" + asHow + "); close to the net interest margin, which divides by interest-earning assets only"},
		"net interest income", nii, "average total assets", avgAssets)
	eff := ratio(Metric{Key: "efficiencyRatio", Label: "Efficiency ratio", Unit: Percent, Period: c.period,
		Formula: "non-interest expense ÷ (net interest income + non-interest income); lower is better"},
		"non-interest expense", costs, "net interest income + non-interest income", revenue)
	feeShare := ratio(Metric{Key: "feeIncomeShare", Label: "Fee income share", Unit: Percent, Period: c.period,
		Formula: "non-interest income ÷ (net interest income + non-interest income)"},
		"non-interest income", fees, "net interest income + non-interest income", revenue)
	ltd := ratio(Metric{Key: "loansToDeposits", Label: "Loans ÷ deposits", Unit: Percent, Period: balance,
		Formula: "loans ÷ deposits"}, "loans", c.ttmValue("loans"), "deposits", c.ttmValue("deposits"))
	return []Metric{margin, eff, feeShare, ltd}
}

func (c *calc) insurerMetrics() []Metric {
	premiums := c.ttmValue("premiumsEarned")
	loss := ratio(Metric{Key: "lossRatio", Label: "Claims ÷ premiums", Unit: Percent, Period: c.period,
		Formula: "claims and policyholder benefits ÷ premiums earned (the loss ratio); lower is better"},
		"claims and benefits", c.ttmValue("claimsIncurred"), "premiums earned", premiums)
	inv := ratio(Metric{Key: "investmentIncomeShare", Label: "Investment income ÷ revenue", Unit: Percent, Period: c.period,
		Formula: "net investment income ÷ revenue"}, "net investment income", c.ttmValue("netInvestmentIncome"), "revenue", c.ttmValue("revenue"))
	return []Metric{loss, inv, c.annualCAGR("premiumsEarned", "premiumGrowth1y", "Premium growth 1y", 1),
		c.annualCAGR("premiumsEarned", "premiumGrowth5y", "Premium CAGR 5y", 5)}
}

// commonEquity is shareholders' equity less preferred stock (nil without
// equity; preferred stock not reported counts as none).
func commonEquity(p financials.FinancialPeriod) *float64 {
	if p.TotalEquity == nil {
		return nil
	}
	return ptr(*p.TotalEquity - deref(p.PreferredStock))
}

// averageBalance averages a balance-sheet value at the TTM end and a year
// earlier, or returns the latest value when the year-earlier one is missing.
func (c *calc) averageBalance(get func(financials.FinancialPeriod) *float64) (*float64, string) {
	if c.ttm == nil {
		return nil, ""
	}
	end := get(*c.ttm)
	if end == nil {
		return nil, ""
	}
	for _, list := range [][]financials.FinancialPeriod{c.f.Quarterly, c.f.Annual} {
		for i := len(list) - 1; i >= 0; i-- {
			if !yearApart(list[i].EndDate, c.ttm.EndDate) {
				continue
			}
			if v := get(list[i]); v != nil {
				return ptr((*end + *v) / 2), "average of " + list[i].EndDate + " and " + c.ttm.EndDate
			}
		}
	}
	return end, "at " + c.ttm.EndDate
}

// bookValueCAGR is the growth of book value per share over n fiscal years.
func (c *calc) bookValueCAGR(years int) Metric {
	m := Metric{Key: fmt.Sprintf("bookValueCagr%dy", years), Label: fmt.Sprintf("Book value per share CAGR %dy", years),
		Unit: Percent, Formula: fmt.Sprintf("(end ÷ start)^(1/%d) − 1, book value per share at fiscal year ends", years)}
	bvps := func(p financials.FinancialPeriod) *float64 {
		return div(commonEquity(p), c.sp.shareCount(p, "sharesDiluted"))
	}
	return c.annualGrowth(m, bvps, years)
}

// annualCAGR is a field's growth over n fiscal years.
func (c *calc) annualCAGR(field, key, label string, years int) Metric {
	m := Metric{Key: key, Label: label, Unit: Percent, Formula: fmt.Sprintf("(end ÷ start)^(1/%d) − 1", years)}
	if years == 1 {
		m.Formula = "latest fiscal year ÷ the year before − 1"
	}
	return c.annualGrowth(m, func(p financials.FinancialPeriod) *float64 { return p.Get(field) }, years)
}

func (c *calc) annualGrowth(m Metric, value func(financials.FinancialPeriod) *float64, years int) Metric {
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
	var startV *float64
	for _, p := range c.f.Annual {
		if p.FiscalYear == end.FiscalYear-years {
			startV = value(p)
		}
	}
	endV := value(*end)
	m.Period = fmt.Sprintf("FY%d → FY%d", end.FiscalYear-years, end.FiscalYear)
	m.Inputs = []Input{
		{Name: "start", Value: round4(startV), Period: fmt.Sprintf("FY%d", end.FiscalYear-years)},
		{Name: "end", Value: round4(endV), Period: fmt.Sprintf("FY%d", end.FiscalYear)},
	}
	switch {
	case startV == nil:
		m.Note = fmt.Sprintf("no data for FY%d", end.FiscalYear-years)
	case *startV <= 0 || *endV <= 0:
		m.Note = "a value is zero or negative, so a growth rate is not meaningful"
	default:
		m.Value = ptr(math.Pow(*endV / *startV, 1/float64(years)) - 1)
	}
	return m
}

// roeTrend is return on equity by fiscal year, on the average of the year's
// opening and closing equity.
func (c *calc) roeTrend() *Trend {
	t := &Trend{Label: "Return on equity", Unit: Percent}
	for i := 1; i < len(c.f.Annual); i++ {
		p, prev := c.f.Annual[i], c.f.Annual[i-1]
		ni := p.NetIncomeCommon
		if ni == nil {
			ni = p.NetIncome
		}
		eq, prevEq := commonEquity(p), commonEquity(prev)
		if ni == nil || eq == nil || prevEq == nil || prev.FiscalYear != p.FiscalYear-1 {
			continue
		}
		if avg := (*eq + *prevEq) / 2; avg > 0 {
			t.Points = append(t.Points, Point{Date: p.EndDate, Value: round(*ni/avg, 4)})
		}
	}
	return trimTrend(t)
}

// strengthInputs feed the financial strength score of banks, brokers and
// insurers.
func (c *calc) strengthInputs(s *Sector) scoring.StrengthInputs {
	in := scoring.StrengthInputs{
		Kind:            s.Kind,
		ROE:             find(s.Metrics, "roe"),
		EfficiencyRatio: find(s.Metrics, "efficiencyRatio"),
		EquityToAssets:  find(s.Metrics, "equityToAssets"),
		BookValueCAGR:   c.bookValueCAGR(3).Value,
		PremiumGrowth:   find(s.Metrics, "premiumGrowth1y"),
	}
	if s.Kind == "reit" {
		in.DebtToAssets = find(s.Metrics, "debtToAssets")
		in.InterestCoverage = find(s.Metrics, "ffoInterestCoverage")
		in.Payout = find(s.Metrics, "ffoPayout")
		for _, p := range c.f.Annual {
			in.NetIncomeByYear = append(in.NetIncomeByYear, quarterFFO(p))
		}
		return in
	}
	for _, p := range c.f.Annual {
		in.NetIncomeByYear = append(in.NetIncomeByYear, p.NetIncome)
	}
	return in
}

// --- REITs --------------------------------------------------------------------------

// quarterFFO is one period's funds from operations: net income to common
// shareholders plus
// depreciation and real estate impairments, minus gains on property sales
// (zero when not reported). It is approximate: the depreciation reported on
// the cash flow statement also covers non-property assets, and a REIT's own
// FFO adjusts for its share of joint ventures.
func quarterFFO(p financials.FinancialPeriod) *float64 {
	ni := ffoNetIncome(p)
	if ni == nil || p.Depreciation == nil {
		return nil
	}
	return ptr(*ni + *p.Depreciation - deref(p.PropertySaleGains) + deref(p.RealEstateImpairment))
}

// ffoNetIncome is net income to common shareholders, or net income.
func ffoNetIncome(p financials.FinancialPeriod) *float64 {
	if p.NetIncomeCommon != nil {
		return p.NetIncomeCommon
	}
	return p.NetIncome
}

func (c *calc) reitMetrics(price *float64) []Metric {
	t, ok := latestWith(c.rolling, func(t ttm) bool { return t.ffo != nil })
	period := "TTM to " + t.end
	ffo := Metric{Key: "ffo", Label: "Funds from operations (FFO)", Unit: USD, Period: period,
		Formula: "net income to common shareholders + depreciation + real estate impairments − gains on property sales (approximate)"}
	if !ok {
		ffo.Note = "net income or depreciation not reported for four straight quarters"
		ffo.Period = c.period
		return []Metric{ffo}
	}
	ffo.Value = round4(t.ffo)
	if q := c.quartersEnding(t.end); len(q) == 4 {
		sum := func(get func(financials.FinancialPeriod) *float64) *float64 {
			total := 0.0
			for _, p := range q {
				total += deref(get(p))
			}
			return ptr(total)
		}
		ffo.Inputs = []Input{
			{Name: "net income to common shareholders", Value: sum(ffoNetIncome)},
			{Name: "depreciation", Value: sum(func(p financials.FinancialPeriod) *float64 { return p.Depreciation })},
			{Name: "real estate impairments", Value: sum(func(p financials.FinancialPeriod) *float64 { return p.RealEstateImpairment })},
			{Name: "gains on property sales", Value: sum(func(p financials.FinancialPeriod) *float64 { return p.PropertySaleGains })},
		}
	}

	perShare := ratio(Metric{Key: "ffoPerShare", Label: "FFO per share", Unit: PerShare, Period: period,
		Formula: "FFO ÷ diluted shares"}, "FFO", t.ffo, "diluted shares", t.shares)
	pffo := multiple(Metric{Key: "pffo", Label: "P/FFO", Formula: "price ÷ FFO per share", Period: period},
		"price", price, "FFO per share", perShare.Value)
	payout := ratio(Metric{Key: "ffoPayout", Label: "Dividends ÷ FFO", Unit: Percent, Period: c.period,
		Formula: "dividends paid ÷ FFO; above 100% means paying out more than the properties earn"},
		"dividends paid", c.ttmValue("dividendsPaid"), "FFO", t.ffo)
	debtAssets := ratio(Metric{Key: "debtToAssets", Label: "Debt ÷ assets", Unit: Percent, Period: "balance sheet at " + c.ttmEnd(),
		Formula: "total debt ÷ total assets"}, "total debt", c.ttmValue("totalDebt"), "total assets", c.ttmValue("totalAssets"))
	interest, interestPeriod, interestNote := c.interestExpense()
	var beforeInterest *float64
	if interest != nil {
		beforeInterest = ptr(*t.ffo + *interest)
	}
	coverage := ratio(Metric{Key: "ffoInterestCoverage", Label: "Interest coverage (FFO)", Unit: Multiple, Period: period,
		Formula: "(FFO + interest expense) ÷ interest expense", Note: interestNote},
		"FFO + interest expense", beforeInterest, "interest expense", interest)
	if len(coverage.Inputs) == 2 {
		coverage.Inputs[1].Period = interestPeriod
	}
	return []Metric{ffo, perShare, pffo, payout, coverage, c.ffoPerShareCAGR(3), c.ffoPerShareCAGR(5), debtAssets}
}

// ffoPerShareCAGR compares FFO per share in the latest TTM window with the
// window n years earlier.
func (c *calc) ffoPerShareCAGR(years int) Metric {
	m := Metric{Key: fmt.Sprintf("ffoPerShareCagr%dy", years), Label: fmt.Sprintf("FFO per share CAGR %dy", years),
		Unit: Percent, Formula: fmt.Sprintf("(end ÷ start)^(1/%d) − 1, FFO per share over trailing twelve months", years)}
	perShare := func(t ttm) *float64 { return div(t.ffo, t.shares) }
	end, ok := latestWith(c.rolling, func(t ttm) bool { return perShare(t) != nil })
	if !ok {
		m.Note = "FFO per share not available"
		return m
	}
	var start *ttm
	for i := range c.rolling {
		if perShare(c.rolling[i]) != nil && yearsApart(c.rolling[i].end, end.end, years) {
			start = &c.rolling[i]
		}
	}
	if start == nil {
		m.Note = fmt.Sprintf("no FFO for the twelve months to %s", shiftDate(end.end, -years))
		return m
	}
	sv, ev := *perShare(*start), *perShare(end)
	m.Period = fmt.Sprintf("TTM to %s → TTM to %s", start.end, end.end)
	m.Inputs = []Input{{Name: "start", Value: round4(&sv), Period: "TTM to " + start.end}, {Name: "end", Value: round4(&ev), Period: "TTM to " + end.end}}
	if sv <= 0 || ev <= 0 {
		m.Note = "a value is zero or negative, so a growth rate is not meaningful"
		return m
	}
	m.Value = ptr(math.Pow(ev/sv, 1/float64(years)) - 1)
	return m
}

// ffoPerShareTrend is FFO per share by fiscal year, in today's share terms.
func (c *calc) ffoPerShareTrend() *Trend {
	t := &Trend{Label: "FFO per share", Unit: PerShare}
	for _, p := range c.f.Annual {
		if v := div(quarterFFO(p), c.sp.shareCount(p, "sharesDiluted")); v != nil {
			t.Points = append(t.Points, Point{Date: p.EndDate, Value: round(*v, 4)})
		}
	}
	return trimTrend(t)
}

// quartersEnding returns the four quarters ending on end, or nil.
func (c *calc) quartersEnding(end string) []financials.FinancialPeriod {
	for i := 3; i < len(c.f.Quarterly); i++ {
		if c.f.Quarterly[i].EndDate == end {
			return c.f.Quarterly[i-3 : i+1]
		}
	}
	return nil
}

// trimTrend keeps the last ten years, or returns nil when there are fewer
// than two points.
func trimTrend(t *Trend) *Trend {
	if len(t.Points) < 2 {
		return nil
	}
	t.Points = t.Points[max(0, len(t.Points)-10):]
	return t
}
