// Package financials turns raw SEC XBRL facts into canonical financial
// periods: fiscal years, fiscal quarters and trailing twelve months (TTM).
//
// Everything here is a pure function of its input, so it is tested with
// recorded fixtures and hand-checked numbers.
package financials

// PeriodType distinguishes annual, quarterly and TTM periods.
type PeriodType string

const (
	FiscalYear    PeriodType = "FY"
	FiscalQuarter PeriodType = "Q"
	TTM           PeriodType = "TTM"
)

// FinancialPeriod is one period of normalized financials. Every value is a
// pointer: nil means "not reported", which is different from zero.
//
// C# note: *float64 plays the role of double? (Nullable<double>).
type FinancialPeriod struct {
	Ticker        string     `json:"ticker"`
	CIK           string     `json:"cik"`
	PeriodType    PeriodType `json:"periodType"`
	FiscalYear    int        `json:"fiscalYear"`
	FiscalQuarter int        `json:"fiscalQuarter,omitempty"` // 1-4 for quarters; omitted for FY
	StartDate     string     `json:"startDate"`
	EndDate       string     `json:"endDate"`
	// Accession is the filing the revenue (or net income) figure came from.
	Accession string `json:"accession,omitempty"`

	Revenue              *float64 `json:"revenue"`
	GrossProfit          *float64 `json:"grossProfit"`
	OperatingIncome      *float64 `json:"operatingIncome"`
	NetIncome            *float64 `json:"netIncome"`
	EPSDiluted           *float64 `json:"epsDiluted"`
	SharesDiluted        *float64 `json:"sharesDiluted"`
	OperatingCashFlow    *float64 `json:"operatingCashFlow"`
	Capex                *float64 `json:"capex"`
	FreeCashFlow         *float64 `json:"freeCashFlow"`
	Cash                 *float64 `json:"cash"`
	ShortTermInvestments *float64 `json:"shortTermInvestments"`
	TotalDebt            *float64 `json:"totalDebt"`
	TotalEquity          *float64 `json:"totalEquity"`
	InterestExpense      *float64 `json:"interestExpense"`
	DividendsPaid        *float64 `json:"dividendsPaid"`
	Buybacks             *float64 `json:"buybacks"`
	DebtRepaid           *float64 `json:"debtRepaid"`
	DebtIssued           *float64 `json:"debtIssued"`

	// Sources records where each value came from: an XBRL concept name, or a
	// description of how it was derived.
	Sources map[string]string `json:"sources"`
}

// Financials is the full normalized history for one company.
type Financials struct {
	Annual    []FinancialPeriod `json:"annual"`    // oldest first
	Quarterly []FinancialPeriod `json:"quarterly"` // oldest first
	TTM       *FinancialPeriod  `json:"ttm"`       // nil if the last 4 quarters are incomplete
	Warnings  []string          `json:"warnings,omitempty"`
}

// field returns a pointer to the struct field for a canonical field name.
// A switch is the plain, reflection-free way to do this in Go.
func (p *FinancialPeriod) field(name string) **float64 {
	switch name {
	case "revenue":
		return &p.Revenue
	case "grossProfit":
		return &p.GrossProfit
	case "operatingIncome":
		return &p.OperatingIncome
	case "netIncome":
		return &p.NetIncome
	case "epsDiluted":
		return &p.EPSDiluted
	case "sharesDiluted":
		return &p.SharesDiluted
	case "operatingCashFlow":
		return &p.OperatingCashFlow
	case "capex":
		return &p.Capex
	case "freeCashFlow":
		return &p.FreeCashFlow
	case "cash":
		return &p.Cash
	case "shortTermInvestments":
		return &p.ShortTermInvestments
	case "totalDebt":
		return &p.TotalDebt
	case "totalEquity":
		return &p.TotalEquity
	case "interestExpense":
		return &p.InterestExpense
	case "dividendsPaid":
		return &p.DividendsPaid
	case "buybacks":
		return &p.Buybacks
	case "debtRepaid":
		return &p.DebtRepaid
	case "debtIssued":
		return &p.DebtIssued
	default:
		panic("financials: unknown field " + name) // programming error, caught by tests
	}
}

// Get returns a field's value by canonical name (nil if not reported).
func (p *FinancialPeriod) Get(name string) *float64 {
	return *p.field(name)
}

// FieldNames lists the canonical value fields in display order.
func FieldNames() []string {
	return []string{
		"revenue", "grossProfit", "operatingIncome", "netIncome", "epsDiluted", "sharesDiluted",
		"operatingCashFlow", "capex", "freeCashFlow",
		"cash", "shortTermInvestments", "totalDebt", "totalEquity",
		"interestExpense", "dividendsPaid", "buybacks", "debtRepaid", "debtIssued",
	}
}
