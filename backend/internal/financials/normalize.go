package financials

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
)

const day = 24 * time.Hour

// Period lengths in days. Ranges allow for 52/53-week fiscal years, whose
// quarters can run 12, 12, 12 and 16 (or 17) weeks (PepsiCo).
func isAnnual(days int) bool  { return days >= 350 && days <= 380 }
func isQuarter(days int) bool { return days >= 80 && days <= 125 }

// value is one resolved number plus where it came from.
type value struct {
	val    float64
	start  time.Time // zero for instant values
	end    time.Time
	accn   string
	filed  string
	source string
}

// series holds one concept's values, deduplicated and keyed by end date.
type series struct {
	annual    map[string]value
	quarterly map[string]value
	instant   map[string]value
}

// Normalize converts a company's XBRL facts into fiscal years, fiscal
// quarters and TTM. It never guesses: a value that cannot be found or derived
// from reported numbers is left nil.
//
// Values keep the share terms of the filing they came from (see
// FinancialPeriod.Filed). splits (nil if unknown) are used only where two
// per-share values from different filings are combined: deriving a quarter
// from year-to-date totals and summing TTM. Each value is first put in the
// other's share terms, so a split between the filings does not mix them.
func Normalize(co edgar.Company, facts *edgar.CompanyFacts, splits []Split) Financials {
	gaap := facts.Facts["us-gaap"]
	r := newResolver(gaap, splits)
	cal := newCalendar(r, gaap)

	var out Financials
	for _, fy := range cal.years {
		out.Annual = append(out.Annual, r.period(co, FiscalYear, fy.label, 0, fy.start, fy.end))
	}

	skipped := 0
	for _, q := range r.quarterSpans() {
		label, n, ok := cal.locate(q.end)
		if !ok {
			skipped++
			continue
		}
		out.Quarterly = append(out.Quarterly, r.period(co, FiscalQuarter, label, n, q.start, q.end))
	}
	if skipped > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%d quarters skipped: could not place them in a fiscal year", skipped))
	}

	if f := shareScale(out); f != 1 {
		for _, list := range [][]FinancialPeriod{out.Annual, out.Quarterly} {
			for i := range list {
				if v := list[i].SharesDiluted; v != nil && *v < 1e6 {
					list[i].SharesDiluted = ptr(*v * f)
					list[i].Sources["sharesDiluted"] += fmt.Sprintf(" (filed in %s; scaled)", map[float64]string{1e3: "thousands", 1e6: "millions"}[f])
				}
			}
		}
	}

	ttm, warning := trailing(co, out.Quarterly, out.Annual, r.terms)
	if ttm == nil && len(out.Annual) > 0 {
		// The latest fiscal year is also a trailing twelve months: better than
		// leaving every TTM-based metric empty.
		fy := out.Annual[len(out.Annual)-1]
		fy.PeriodType = TTM
		ttm = &fy
		warning += fmt.Sprintf("; using FY%d instead", fy.FiscalYear)
	}
	out.TTM = ttm
	if warning != "" {
		out.Warnings = append(out.Warnings, warning)
	}
	out.Warnings = append(out.Warnings, missingFieldWarnings(out)...)
	return out
}

// shareScale detects share counts filed without their scale (McDonald's tags
// 718 for 718 million): net income ÷ EPS gives the real count. It returns the
// factor to multiply such counts by, or 1.
func shareScale(f Financials) float64 {
	votes := map[float64]int{}
	for _, list := range [][]FinancialPeriod{f.Annual, f.Quarterly} {
		for _, p := range list {
			ni, eps, sh := p.NetIncome, p.EPSDiluted, p.SharesDiluted
			if ni == nil || eps == nil || sh == nil || *sh <= 0 || *sh >= 1e6 || math.Abs(*eps) < 0.05 {
				continue
			}
			ratio := *ni / *eps / *sh
			for _, factor := range []float64{1e3, 1e6} {
				if ratio > factor*0.8 && ratio < factor*1.25 {
					votes[factor]++
				}
			}
		}
	}
	best, n := 1.0, 0
	for factor, v := range votes {
		if v > n {
			best, n = factor, v
		}
	}
	return best
}

// --- Reading concepts ---------------------------------------------------------

// resolver holds every concept series, grouped by canonical field.
type resolver struct {
	specs  map[string]fieldSpec
	series map[string][]series // field name → one series per concept, in priority order
	terms  shareTerms
	// noDebtAfter: periods ending after this date have no debt, so their
	// total debt is zero rather than unknown ("" when not debt-free).
	noDebtAfter string
}

func newResolver(gaap map[string]edgar.Concept, splits []Split) *resolver {
	r := &resolver{specs: map[string]fieldSpec{}, series: map[string][]series{}, terms: splits}
	for _, specs := range [][]fieldSpec{fields, helpers} {
		for _, spec := range specs {
			r.specs[spec.name] = spec
			var terms shareTerms
			if spec.unit == perShareUnit {
				terms = r.terms
			}
			for _, concept := range spec.concepts {
				c, ok := gaap[concept]
				if !ok {
					continue
				}
				r.series[spec.name] = append(r.series[spec.name], buildSeries(concept, c.Units[spec.unit], spec.kind, terms, nonNegative[spec.name]))
			}
		}
	}
	r.noDebtAfter = noDebtAfter(gaap)
	return r
}

// buildSeries deduplicates a concept's facts and, for flows, derives quarters
// that filings only report as year-to-date totals. terms is set for per-share
// concepts, so YTD values filed on either side of a stock split are put in the
// same share terms before one is subtracted from the other.
//
// nonNeg marks fields that cannot be negative. When such a quarter comes out
// negative, the year was restated after its quarters were filed (Dollar
// Tree's FY2023 without Family Dollar); it is derived from the year as
// originally reported instead.
func buildSeries(concept string, facts []edgar.Fact, kind fieldKind, terms shareTerms, nonNeg bool) series {
	s := series{annual: map[string]value{}, quarterly: map[string]value{}, instant: map[string]value{}}

	// The same period is reported by several filings (original + later
	// comparatives). Keep the most recently filed value, which includes any
	// restatement.
	type dates struct{ start, end string }
	latest := map[dates]edgar.Fact{}
	original := map[dates]edgar.Fact{}
	for _, f := range facts {
		if (kind == instant) != (f.Start == "") {
			continue // wrong shape for this field
		}
		if isProxy(f.Form) {
			continue
		}
		k := dates{f.Start, f.End}
		if cur, ok := latest[k]; !ok || newer(f, cur) {
			latest[k] = f
		}
		if cur, ok := original[k]; !ok || newer(cur, f) {
			original[k] = f
		}
	}

	byStart := map[string][]value{}
	for k, f := range latest {
		end, ok := parseDate(k.end)
		if !ok {
			continue
		}
		v := value{val: f.Val, end: end, accn: f.Accn, filed: f.Filed, source: concept}
		if kind == instant {
			s.instant[k.end] = v
			continue
		}
		start, ok := parseDate(k.start)
		if !ok {
			continue
		}
		v.start = start
		switch d := days(start, end); {
		case isAnnual(d):
			keepNewer(s.annual, k.end, v)
		case isQuarter(d):
			keepNewer(s.quarterly, k.end, v)
		}
		byStart[k.start] = append(byStart[k.start], v)
	}

	if kind != flow {
		return s // averages and balances cannot be subtracted
	}

	// 10-Qs report cash flows (and 10-Ks report full years) as year-to-date
	// totals that share a start date. Consecutive YTD values differ by exactly
	// one quarter: Q2 = H1 − Q1, Q3 = 9M − H1, Q4 = FY − 9M.
	for _, chain := range byStart {
		sort.Slice(chain, func(i, j int) bool { return chain[i].end.Before(chain[j].end) })
		for i := 1; i < len(chain); i++ {
			prev, cur := chain[i-1], chain[i]
			qStart := prev.end.Add(day)
			key := formatDate(cur.end)
			if !isQuarter(days(qStart, cur.end)) {
				continue
			}
			if _, reported := s.quarterly[key]; reported {
				continue
			}
			// The quarter is in the share terms of cur's filing.
			source := fmt.Sprintf("%s (derived: YTD to %s minus YTD to %s", concept, key, formatDate(prev.end))
			prevVal := prev.val
			if f := terms.factor(prev.filed, cur.filed); f != 1 {
				prevVal /= f
				source += ", restated for a " + splitRatio(f) + " split"
			}
			val := cur.val - prevVal
			if nonNeg && val < 0 {
				orig := original[dates{formatDate(cur.start), key}]
				if alt := orig.Val - prevVal; orig.Val != cur.val && alt >= 0 {
					val = alt
					source += ", using the year as filed on " + orig.Filed + ", before it was restated"
				}
			}
			s.quarterly[key] = value{
				val:    round4(val),
				start:  qStart,
				end:    cur.end,
				accn:   cur.accn,
				filed:  cur.filed,
				source: source + ")",
			}
		}
	}
	return s
}

// get returns the first concept's value for a field and period. For revenue,
// whose concepts are synonyms, a later concept's value wins when it was filed
// over a year after the first one's: the company moved to that concept and
// kept re-reporting the period under it (General Mills' "Revenues" holds one
// stray FY2024 figure).
func (r *resolver) get(field string, pt PeriodType, end string) (value, bool) {
	kind := r.specs[field].kind
	var best value
	found := false
	for _, s := range r.series[field] {
		var m map[string]value
		switch {
		case kind == instant:
			m = s.instant
		case pt == FiscalYear:
			m = s.annual
		default:
			m = s.quarterly
		}
		v, ok := m[end]
		switch {
		case !ok:
		case !found:
			best, found = v, true
			if field != "revenue" {
				return best, true
			}
		case filedApart(best.filed, v.filed) > 300:
			best = v
		}
	}
	return best, found
}

// isProxy reports proxy statements (DEF 14A and the like). Their pay-versus-
// performance tables tag net income, often in millions without the scale
// (Medtronic's 4,662 for $4,662 million), and are filed after the 10-K, so
// they would otherwise win as the latest value.
func isProxy(form string) bool {
	return strings.Contains(form, "14A") || strings.Contains(form, "14C")
}

// filedApart is the number of days from filing date a to b.
func filedApart(a, b string) int {
	ta, ok1 := parseDate(a)
	tb, ok2 := parseDate(b)
	if !ok1 || !ok2 {
		return 0
	}
	return int(tb.Sub(ta) / day)
}

// noDebtAfter returns the date after which the company has had no debt: the
// last time any debt balance, debt cash flow or interest expense was above
// zero, when that is over a year before its latest filing (Palantir repaid
// its loans in 2021, Zoom its notes in 2019). "" means the debt is recent or
// ongoing; then a period without tagged debt has unknown debt, not zero,
// because the company may tag it under its own concepts (Caterpillar, Deere).
func noDebtAfter(gaap map[string]edgar.Concept) string {
	var lastDebt, lastFiled string
	for name, c := range gaap {
		lower := strings.ToLower(name)
		debtLike := strings.Contains(lower, "debt") || strings.Contains(lower, "borrowing") ||
			strings.Contains(lower, "notespayable") || strings.HasPrefix(lower, "interestexpense")
		owned := strings.Contains(lower, "securities") || strings.Contains(lower, "investment") ||
			strings.Contains(lower, "receivable") || strings.Contains(lower, "availableforsale") ||
			strings.Contains(lower, "heldtomaturity") || strings.Contains(lower, "capacity")
		for _, facts := range c.Units {
			for _, f := range facts {
				lastFiled = max(lastFiled, f.End)
				if debtLike && !owned && f.Val != 0 {
					lastDebt = max(lastDebt, f.End)
				}
			}
		}
	}
	last, ok1 := parseDate(lastFiled)
	debt, ok2 := parseDate(lastDebt)
	switch {
	case !ok1:
		return ""
	case !ok2:
		return "0000-00-00" // never had any
	case last.Sub(debt) > 365*day:
		return lastDebt
	default:
		return ""
	}
}

type span struct{ start, end time.Time }

// quarterSpans lists every quarter any period-based field has a value for.
func (r *resolver) quarterSpans() []span {
	seen := map[string]span{}
	for name, list := range r.series {
		if r.specs[name].kind == instant {
			continue
		}
		for _, s := range list {
			for end, v := range s.quarterly {
				if _, ok := seen[end]; !ok {
					seen[end] = span{v.start, v.end}
				}
			}
		}
	}
	return sortedSpans(seen)
}

// annualSpans lists every fiscal-year period any field has a value for.
func (r *resolver) annualSpans() []span {
	seen := map[string]span{}
	for name, list := range r.series {
		if r.specs[name].kind == instant {
			continue
		}
		for _, s := range list {
			for end, v := range s.annual {
				if _, ok := seen[end]; !ok {
					seen[end] = span{v.start, v.end}
				}
			}
		}
	}
	return sortedSpans(seen)
}

// period builds one FinancialPeriod, including derived fields.
func (r *resolver) period(co edgar.Company, pt PeriodType, fy, fq int, start, end time.Time) FinancialPeriod {
	p := FinancialPeriod{
		Ticker:        co.Ticker,
		CIK:           co.CIK,
		PeriodType:    pt,
		FiscalYear:    fy,
		FiscalQuarter: fq,
		StartDate:     formatDate(start),
		EndDate:       formatDate(end),
		Sources:       map[string]string{},
		Filed:         map[string]string{},
	}
	endKey := formatDate(end)

	for _, spec := range fields {
		if v, ok := r.get(spec.name, pt, endKey); ok {
			*p.field(spec.name) = ptr(v.val)
			p.Sources[spec.name] = v.source
			p.Filed[spec.name] = v.filed
		}
	}
	for _, name := range []string{"revenue", "netIncome"} {
		if v, ok := r.get(name, pt, endKey); ok {
			p.Accession = v.accn
			break
		}
	}

	// Gross profit: fall back to revenue − cost of revenue when not tagged.
	if p.GrossProfit == nil && p.Revenue != nil {
		if cost, ok := r.get("costOfRevenue", pt, endKey); ok {
			p.GrossProfit = ptr(*p.Revenue - cost.val)
			p.Sources["grossProfit"] = "derived: revenue − " + cost.source
		}
	}

	// Free cash flow = operating cash flow − capital expenditure.
	if p.OperatingCashFlow != nil && p.Capex != nil {
		p.FreeCashFlow = ptr(*p.OperatingCashFlow - *p.Capex)
		p.Sources["freeCashFlow"] = "derived: operatingCashFlow − capex"
	}

	if v, src, ok := r.totalDebt(endKey); ok {
		p.TotalDebt = ptr(v)
		p.Sources["totalDebt"] = src
	} else if r.noDebtAfter != "" && endKey > r.noDebtAfter {
		p.TotalDebt = ptr(0.0)
		p.Sources["totalDebt"] = "no debt: no debt, borrowings or interest expense reported"
		if r.noDebtAfter != "0000-00-00" {
			p.Sources["totalDebt"] += " since " + r.noDebtAfter
		}
	}
	return p
}

// totalDebt adds up the debt balances reported at a date.
//
//  1. All current debt (DebtCurrent) + non-current long-term debt: the whole
//     balance sheet, nothing to double count.
//  2. Otherwise long-term debt (LongTermDebt, which includes the current
//     portion, or non-current + current portion) plus the short-term debt.
//     Short-term borrowings often already include commercial paper and the
//     current portion of long-term debt (HPE, PepsiCo). When they are at
//     least as large as those two together, they are taken to include them,
//     so neither is counted twice.
//
// Convertible notes are added only when no long-term debt is tagged, because
// filers that tag both usually include converts in long-term debt already.
func (r *resolver) totalDebt(end string) (float64, string, bool) {
	get := func(name string) (value, bool) { return r.get(name, FiscalYear, end) } // instants ignore the period type

	non, hasNon := get("debtNoncurrent")
	if !hasNon {
		non, hasNon = get("debtNoncurrentLeases")
	}
	if cur, ok := get("debtCurrentAll"); ok && hasNon {
		return cur.val + non.val, "derived: " + cur.source + " + " + non.source, true
	}

	var total float64
	var parts []string
	use := func(v value, ok bool) {
		if ok {
			total += v.val
			parts = append(parts, v.source)
		}
	}

	ltdCur, hasCur := get("debtCurrent")
	ltdTotal, hasTotal := get("debtTotal")
	if hasTotal {
		use(ltdTotal, true)
	} else {
		use(non, hasNon)
		use(ltdCur, hasCur)
	}
	haveLongTerm := hasTotal || hasNon || hasCur

	cp, hasCP := get("commercialPaper")
	stb, hasSTB := get("shortTermBorrowings")
	included := 0.0
	if hasCP {
		included += cp.val
	}
	if hasCur {
		included += ltdCur.val
	}
	switch {
	case hasSTB && (hasCP || hasCur) && stb.val >= included:
		// Short-term borrowings include commercial paper and the current
		// portion, which the long-term part may already count.
		total += stb.val
		parts = append(parts, stb.source+" (includes commercial paper and current maturities)")
		if hasCur {
			total -= ltdCur.val
		}
	case hasSTB && hasCP && stb.val >= cp.val:
		// They include commercial paper only (Coca-Cola's "loans and notes
		// payable"); the current portion is in the long-term part.
		total += stb.val
		parts = append(parts, stb.source+" (includes commercial paper)")
	default:
		use(cp, hasCP)
		use(stb, hasSTB)
	}

	if !haveLongTerm {
		use(get("convertibleNoncurrent"))
		use(get("convertibleCurrent"))
		// Notes payable plus secured debt (mortgages), only with the notes:
		// secured debt alone is usually a slice (Deere's securitizations).
		if np, ok := get("notesPayable"); ok {
			use(np, true)
			use(get("securedDebt"))
		}
	}
	if len(parts) == 0 {
		return 0, "", false
	}
	return total, "derived: " + strings.Join(parts, " + "), true
}

// --- Fiscal calendar -----------------------------------------------------------

type fiscalYear struct {
	label      int
	start, end time.Time
}

type calendar struct {
	years []fiscalYear // oldest first
}

// newCalendar finds the company's fiscal years from its annual periods and
// labels each one with the fiscal year its own 10-K used.
func newCalendar(r *resolver, gaap map[string]edgar.Concept) calendar {
	labels := filingYears(gaap)
	var cal calendar
	for _, s := range r.annualSpans() {
		label, ok := labels[formatDate(s.end)]
		if !ok {
			label = s.end.Year()
		}
		cal.years = append(cal.years, fiscalYear{label: label, start: s.start, end: s.end})
	}
	// Guard against bad "fy" tags: labels must strictly increase.
	for i := 1; i < len(cal.years); i++ {
		if cal.years[i].label <= cal.years[i-1].label {
			for j := range cal.years {
				cal.years[j].label = cal.years[j].end.Year()
			}
			break
		}
	}
	return cal
}

// filingYears maps a fiscal-year end date to the fiscal year number used by
// the 10-K covering it. A 10-K's own period is its latest end date; facts in
// it with earlier dates are comparatives, whose "fy" is the filing's, not theirs.
func filingYears(gaap map[string]edgar.Concept) map[string]int {
	type filing struct {
		fy     int
		maxEnd string
		filed  string
	}
	filings := map[string]filing{}
	for _, concept := range Concepts() {
		c, ok := gaap[concept]
		if !ok {
			continue
		}
		for _, facts := range c.Units {
			for _, f := range facts {
				if !strings.HasPrefix(f.Form, "10-K") || f.FP != "FY" || f.FY == 0 {
					continue
				}
				cur := filings[f.Accn]
				if f.End > cur.maxEnd {
					cur.maxEnd = f.End
				}
				cur.fy, cur.filed = f.FY, f.Filed
				filings[f.Accn] = cur
			}
		}
	}

	labels := map[string]int{}
	firstFiled := map[string]string{}
	for _, f := range filings {
		// If several 10-Ks (e.g. an amendment) cover the same year, the
		// original filing decides the label.
		if prev, ok := firstFiled[f.maxEnd]; !ok || f.filed < prev {
			labels[f.maxEnd] = f.fy
			firstFiled[f.maxEnd] = f.filed
		}
	}
	return labels
}

// locate returns the fiscal year and quarter (1-4) a quarter-end date falls
// in. Years after the last 10-K (the current fiscal year) are extrapolated.
func (c calendar) locate(end time.Time) (fy, quarter int, ok bool) {
	if len(c.years) == 0 {
		return 0, 0, false
	}
	year, ok := c.containing(end)
	if !ok {
		return 0, 0, false
	}
	q := int(math.Round(float64(days(year.start, end)) / (365.25 / 4)))
	return year.label, min(max(q, 1), 4), true
}

func (c calendar) containing(t time.Time) (fiscalYear, bool) {
	contains := func(y fiscalYear) bool {
		return t.After(y.start) && !t.After(y.end.Add(3*day))
	}
	for _, y := range c.years {
		if contains(y) {
			return y, true
		}
	}
	next := c.years[len(c.years)-1]
	for range 3 {
		next = fiscalYear{label: next.label + 1, start: next.end.Add(day), end: next.end.AddDate(1, 0, 0)}
		if contains(next) {
			return next, true
		}
	}
	prev := c.years[0]
	for range 3 {
		prev = fiscalYear{label: prev.label - 1, start: prev.start.AddDate(-1, 0, 0), end: prev.start.Add(-day)}
		if contains(prev) {
			return prev, true
		}
	}
	return fiscalYear{}, false
}

// --- TTM ------------------------------------------------------------------------

// trailing builds the TTM period from the latest four consecutive quarters.
// Per-share values are summed in the share terms of the latest filing among
// the four quarters.
func trailing(co edgar.Company, quarters, annual []FinancialPeriod, terms shareTerms) (*FinancialPeriod, string) {
	last := -1
	for i := len(quarters) - 1; i >= 0; i-- {
		if quarters[i].Revenue != nil || quarters[i].NetIncome != nil {
			last = i
			break
		}
	}
	if last < 3 {
		return nil, "TTM unavailable: fewer than 4 quarters reported"
	}
	q := quarters[last-3 : last+1]
	for i := 1; i < len(q); i++ {
		prevEnd, _ := parseDate(q[i-1].EndDate)
		start, _ := parseDate(q[i].StartDate)
		if gap := start.Sub(prevEnd.Add(day)); gap > 7*day || gap < -7*day {
			return nil, fmt.Sprintf("TTM unavailable: quarters ending %s and %s are not consecutive", q[i-1].EndDate, q[i].EndDate)
		}
	}
	latest := q[3]

	t := FinancialPeriod{
		Ticker:        co.Ticker,
		CIK:           co.CIK,
		PeriodType:    TTM,
		FiscalYear:    latest.FiscalYear,
		FiscalQuarter: latest.FiscalQuarter,
		StartDate:     q[0].StartDate,
		EndDate:       latest.EndDate,
		Accession:     latest.Accession,
		Sources:       map[string]string{},
		Filed:         map[string]string{},
	}

	for _, spec := range fields {
		switch spec.kind {
		case flow:
			filed, ok := "", true
			for i := range q {
				if q[i].Get(spec.name) == nil {
					ok = false
					break
				}
				filed = max(filed, q[i].Filed[spec.name])
			}
			if !ok {
				// Interest expense not reported every quarter (Eli Lilly
				// in 2026): the latest fiscal year, if it is recent, is the
				// closest twelve months for interest coverage. Other fields
				// stay empty; metrics falls back to fiscal years itself.
				if spec.name != "interestExpense" {
					continue
				}
				if fy := latestYear(annual); fy != nil && fy.EndDate >= q[0].StartDate && fy.Get(spec.name) != nil {
					*t.field(spec.name) = ptr(*fy.Get(spec.name))
					t.Sources[spec.name] = fmt.Sprintf("FY%d (not reported for every quarter)", fy.FiscalYear)
					t.Filed[spec.name] = fy.Filed[spec.name]
				}
				continue
			}
			sum, restated := 0.0, false
			for i := range q {
				v := *q[i].Get(spec.name)
				if spec.unit == perShareUnit && filed != "" {
					if f := terms.factor(filedOrEnd(q[i], spec.name), filed); f != 1 {
						v /= f
						restated = true
					}
				}
				sum += v
			}
			*t.field(spec.name) = ptr(round4(sum))
			t.Sources[spec.name] = "sum of last 4 quarters"
			if restated {
				t.Sources[spec.name] += " (restated for stock splits)"
			}
			if filed != "" {
				t.Filed[spec.name] = filed
			}
		case average:
			// Use the latest quarter's weighted average; Q4 is rarely reported
			// on its own, so fall back to the fiscal year ending the same day.
			if v := latest.Get(spec.name); v != nil {
				*t.field(spec.name) = ptr(*v)
				t.Sources[spec.name] = latest.Sources[spec.name]
				t.Filed[spec.name] = latest.Filed[spec.name]
			} else if fy := findByEnd(annual, latest.EndDate); fy != nil && fy.Get(spec.name) != nil {
				*t.field(spec.name) = ptr(*fy.Get(spec.name))
				t.Sources[spec.name] = fy.Sources[spec.name] + " (fiscal year)"
				t.Filed[spec.name] = fy.Filed[spec.name]
			}
		case instant:
			if v := latest.Get(spec.name); v != nil {
				*t.field(spec.name) = ptr(*v)
				t.Sources[spec.name] = latest.Sources[spec.name]
				t.Filed[spec.name] = latest.Filed[spec.name]
			}
		}
	}
	if latest.TotalDebt != nil {
		t.TotalDebt = ptr(*latest.TotalDebt)
		t.Sources["totalDebt"] = latest.Sources["totalDebt"]
	}
	if t.OperatingCashFlow != nil && t.Capex != nil {
		t.FreeCashFlow = ptr(*t.OperatingCashFlow - *t.Capex)
		t.Sources["freeCashFlow"] = "derived: operatingCashFlow − capex"
	}
	return &t, ""
}

func latestYear(annual []FinancialPeriod) *FinancialPeriod {
	if len(annual) == 0 {
		return nil
	}
	return &annual[len(annual)-1]
}

func findByEnd(periods []FinancialPeriod, end string) *FinancialPeriod {
	for i := range periods {
		if periods[i].EndDate == end {
			return &periods[i]
		}
	}
	return nil
}

// missingFieldWarnings flags fields with no value in any period.
func missingFieldWarnings(f Financials) []string {
	var out []string
	for _, name := range FieldNames() {
		if optional[name] {
			continue
		}
		found := false
		for _, list := range [][]FinancialPeriod{f.Annual, f.Quarterly} {
			for i := range list {
				if list[i].Get(name) != nil {
					found = true
					break
				}
			}
		}
		if found {
			continue
		}
		msg := name + ": not found in SEC data"
		if hint, ok := missingHints[name]; ok {
			msg += " (" + hint + ")"
		}
		out = append(out, msg)
	}
	return out
}

// --- Stock splits --------------------------------------------------------------

// perShareUnit is the XBRL unit of per-share values (EPS).
const perShareUnit = "USD/shares"

// shareTerms converts per-share values between the share terms of two filing
// dates. Filings made after a split report restated (post-split) values, so a
// value filed on from is in the terms of to once divided by factor(from, to):
// the product of the splits after from, up to and including to (the inverse
// when from is later). Like metrics, a filing made on a split's date counts as
// already restated.
type shareTerms []Split

func (s shareTerms) factor(from, to string) float64 {
	f := 1.0
	for _, sp := range s {
		switch {
		case sp.Date > from && sp.Date <= to:
			f *= sp.Factor
		case sp.Date > to && sp.Date <= from:
			f /= sp.Factor
		}
	}
	return f
}

// splitRatio describes a split factor for sources, e.g. "10-for-1".
func splitRatio(f float64) string {
	if f < 1 {
		return "1-for-" + strconv.FormatFloat(1/f, 'g', 4, 64)
	}
	return strconv.FormatFloat(f, 'g', 4, 64) + "-for-1"
}

func filedOrEnd(p FinancialPeriod, field string) string {
	if d := p.Filed[field]; d != "" {
		return d
	}
	return p.EndDate
}

// --- Small helpers -----------------------------------------------------------

func newer(a, b edgar.Fact) bool {
	if a.Filed != b.Filed {
		return a.Filed > b.Filed // ISO dates compare correctly as strings
	}
	return a.Accn > b.Accn
}

func keepNewer(m map[string]value, key string, v value) {
	cur, ok := m[key]
	if !ok || v.filed > cur.filed || (v.filed == cur.filed && v.accn > cur.accn) {
		m[key] = v
	}
}

func sortedSpans(m map[string]span) []span {
	out := make([]span, 0, len(m))
	for _, s := range m {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].end.Before(out[j].end) })
	return out
}

func parseDate(s string) (time.Time, bool) {
	t, err := time.Parse(time.DateOnly, s)
	return t, err == nil
}

func formatDate(t time.Time) string { return t.Format(time.DateOnly) }

// days counts calendar days in [start, end], inclusive.
func days(start, end time.Time) int { return int(end.Sub(start)/day) + 1 }

// round4 removes floating-point noise from sums and differences
// (e.g. 7.46 − 5.62 = 1.8399999999999999). Dollar amounts are whole numbers,
// so only per-share values are affected.
func round4(x float64) float64 { return math.Round(x*1e4) / 1e4 }

// ptr returns a pointer to a copy of v.
//
// C# note: Go has generics too; [T any] is like <T> in C#.
func ptr[T any](v T) *T { return &v }
