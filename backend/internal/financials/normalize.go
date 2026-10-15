package financials

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
)

const day = 24 * time.Hour

// Period lengths in days. Ranges allow for 52/53-week fiscal years.
func isAnnual(days int) bool  { return days >= 350 && days <= 380 }
func isQuarter(days int) bool { return days >= 80 && days <= 100 }

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
func Normalize(co edgar.Company, facts *edgar.CompanyFacts) Financials {
	gaap := facts.Facts["us-gaap"]
	r := newResolver(gaap)
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

	ttm, warning := trailing(co, out.Quarterly, out.Annual)
	out.TTM = ttm
	if warning != "" {
		out.Warnings = append(out.Warnings, warning)
	}
	out.Warnings = append(out.Warnings, missingFieldWarnings(out)...)
	return out
}

// --- Reading concepts ---------------------------------------------------------

// resolver holds every concept series, grouped by canonical field.
type resolver struct {
	specs  map[string]fieldSpec
	series map[string][]series // field name → one series per concept, in priority order
}

func newResolver(gaap map[string]edgar.Concept) *resolver {
	r := &resolver{specs: map[string]fieldSpec{}, series: map[string][]series{}}
	for _, specs := range [][]fieldSpec{fields, helpers} {
		for _, spec := range specs {
			r.specs[spec.name] = spec
			for _, concept := range spec.concepts {
				c, ok := gaap[concept]
				if !ok {
					continue
				}
				r.series[spec.name] = append(r.series[spec.name], buildSeries(concept, c.Units[spec.unit], spec.kind))
			}
		}
	}
	return r
}

// buildSeries deduplicates a concept's facts and, for flows, derives quarters
// that filings only report as year-to-date totals.
func buildSeries(concept string, facts []edgar.Fact, kind fieldKind) series {
	s := series{annual: map[string]value{}, quarterly: map[string]value{}, instant: map[string]value{}}

	// The same period is reported by several filings (original + later
	// comparatives). Keep the most recently filed value, which includes any
	// restatement.
	type dates struct{ start, end string }
	latest := map[dates]edgar.Fact{}
	for _, f := range facts {
		if (kind == instant) != (f.Start == "") {
			continue // wrong shape for this field
		}
		k := dates{f.Start, f.End}
		if cur, ok := latest[k]; !ok || newer(f, cur) {
			latest[k] = f
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
			s.quarterly[key] = value{
				val:   round4(cur.val - prev.val),
				start: qStart,
				end:   cur.end,
				accn:  cur.accn,
				filed: cur.filed,
				source: fmt.Sprintf("%s (derived: YTD to %s minus YTD to %s)",
					concept, key, formatDate(prev.end)),
			}
		}
	}
	return s
}

// get returns the first concept's value for a field and period.
func (r *resolver) get(field string, pt PeriodType, end string) (value, bool) {
	kind := r.specs[field].kind
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
		if v, ok := m[end]; ok {
			return v, true
		}
	}
	return value{}, false
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
	}
	return p
}

// totalDebt adds up the debt balances reported at a date:
// long-term debt (total, or current + non-current) plus commercial paper and
// short-term borrowings. Convertible notes are added only when no long-term
// debt is tagged, because filers that tag both usually include converts in
// long-term debt already.
func (r *resolver) totalDebt(end string) (float64, string, bool) {
	var total float64
	var parts []string
	add := func(name string) bool {
		v, ok := r.get(name, FiscalYear, end) // instants ignore the period type
		if ok {
			total += v.val
			parts = append(parts, v.source)
		}
		return ok
	}

	haveLongTerm := add("debtTotal")
	if !haveLongTerm {
		cur := add("debtCurrent")
		non := add("debtNoncurrent")
		haveLongTerm = cur || non
	}
	add("commercialPaper")
	add("shortTermBorrowings")
	if !haveLongTerm {
		add("convertibleNoncurrent")
		add("convertibleCurrent")
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
func trailing(co edgar.Company, quarters, annual []FinancialPeriod) (*FinancialPeriod, string) {
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
	}

	for _, spec := range fields {
		switch spec.kind {
		case flow:
			sum, ok := 0.0, true
			for i := range q {
				v := q[i].Get(spec.name)
				if v == nil {
					ok = false
					break
				}
				sum += *v
			}
			if ok {
				*t.field(spec.name) = ptr(round4(sum))
				t.Sources[spec.name] = "sum of last 4 quarters"
			}
		case average:
			// Use the latest quarter's weighted average; Q4 is rarely reported
			// on its own, so fall back to the fiscal year ending the same day.
			if v := latest.Get(spec.name); v != nil {
				*t.field(spec.name) = ptr(*v)
				t.Sources[spec.name] = latest.Sources[spec.name]
			} else if fy := findByEnd(annual, latest.EndDate); fy != nil && fy.Get(spec.name) != nil {
				*t.field(spec.name) = ptr(*fy.Get(spec.name))
				t.Sources[spec.name] = fy.Sources[spec.name] + " (fiscal year)"
			}
		case instant:
			if v := latest.Get(spec.name); v != nil {
				*t.field(spec.name) = ptr(*v)
				t.Sources[spec.name] = latest.Sources[spec.name]
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
