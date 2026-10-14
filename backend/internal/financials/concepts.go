package financials

// fieldKind says how a field's values behave over time.
type fieldKind int

const (
	// flow is reported over a period and can be summed (revenue, cash flows).
	// Quarters missing from filings are derived from year-to-date values.
	flow fieldKind = iota
	// average is a weighted average over a period (diluted share count). It
	// cannot be summed or subtracted, so only directly reported values are used.
	average
	// instant is a balance-sheet value at a single date.
	instant
)

// fieldSpec maps one canonical field to the XBRL concepts that may hold it.
// Concepts are tried in order for each period; the first with a value wins.
// Companies switch concepts over time (e.g. SalesRevenueNet → RevenueFrom...),
// so the choice is made per period, and recorded in FinancialPeriod.Sources.
type fieldSpec struct {
	name     string
	kind     fieldKind
	unit     string
	concepts []string
}

// fields are copied straight into FinancialPeriod (see FinancialPeriod.field).
var fields = []fieldSpec{
	{"revenue", flow, "USD", []string{
		"Revenues",
		"RevenueFromContractWithCustomerExcludingAssessedTax",
		"RevenueFromContractWithCustomerIncludingAssessedTax",
		"SalesRevenueNet",
	}},
	{"grossProfit", flow, "USD", []string{"GrossProfit"}},
	{"operatingIncome", flow, "USD", []string{"OperatingIncomeLoss"}},
	{"netIncome", flow, "USD", []string{
		"NetIncomeLoss", // attributable to the parent company: preferred
		"ProfitLoss",    // includes non-controlling interests
		"NetIncomeLossAvailableToCommonStockholdersBasic",
	}},
	{"epsDiluted", flow, "USD/shares", []string{"EarningsPerShareDiluted"}},
	{"sharesDiluted", average, "shares", []string{"WeightedAverageNumberOfDilutedSharesOutstanding"}},
	{"operatingCashFlow", flow, "USD", []string{
		"NetCashProvidedByUsedInOperatingActivities",
		"NetCashProvidedByUsedInOperatingActivitiesContinuingOperations",
	}},
	{"capex", flow, "USD", []string{
		"PaymentsToAcquirePropertyPlantAndEquipment",
		"PaymentsToAcquireProductiveAssets",
	}},
	{"cash", instant, "USD", []string{
		"CashAndCashEquivalentsAtCarryingValue",
		"CashCashEquivalentsRestrictedCashAndRestrictedCashEquivalents",
	}},
	{"shortTermInvestments", instant, "USD", []string{
		"ShortTermInvestments",
		"MarketableSecuritiesCurrent",
		"AvailableForSaleSecuritiesDebtSecuritiesCurrent",
	}},
	{"totalEquity", instant, "USD", []string{
		"StockholdersEquity",
		"StockholdersEquityIncludingPortionAttributableToNoncontrollingInterest",
	}},
	{"interestExpense", flow, "USD", []string{
		"InterestExpense",
		"InterestExpenseNonoperating",
		"InterestExpenseDebt",
	}},
	{"dividendsPaid", flow, "USD", []string{"PaymentsOfDividends", "PaymentsOfDividendsCommonStock"}},
	{"buybacks", flow, "USD", []string{"PaymentsForRepurchaseOfCommonStock"}},
	{"debtRepaid", flow, "USD", []string{
		"RepaymentsOfLongTermDebt",
		"RepaymentsOfDebt",
		"RepaymentsOfConvertibleDebt",
	}},
	{"debtIssued", flow, "USD", []string{
		"ProceedsFromIssuanceOfLongTermDebt",
		"ProceedsFromIssuanceOfDebt",
		"ProceedsFromConvertibleDebt",
	}},
}

// helpers are inputs to derived fields (gross profit fallback, total debt).
// They are not exposed on FinancialPeriod.
var helpers = []fieldSpec{
	{"costOfRevenue", flow, "USD", []string{"CostOfRevenue", "CostOfGoodsAndServicesSold"}},
	{"debtTotal", instant, "USD", []string{"LongTermDebt"}},
	{"debtCurrent", instant, "USD", []string{"LongTermDebtCurrent"}},
	{"debtNoncurrent", instant, "USD", []string{"LongTermDebtNoncurrent"}},
	{"commercialPaper", instant, "USD", []string{"CommercialPaper"}},
	{"shortTermBorrowings", instant, "USD", []string{"ShortTermBorrowings"}},
	{"convertibleNoncurrent", instant, "USD", []string{"ConvertibleDebtNoncurrent"}},
	{"convertibleCurrent", instant, "USD", []string{"ConvertibleNotesPayableCurrent"}},
}

// missingHints explains common reasons a field has no data at all.
var missingHints = map[string]string{
	"epsDiluted":    "companies with several share classes (e.g. Visa) report EPS per class, which the SEC companyfacts feed omits",
	"sharesDiluted": "companies with several share classes report share counts per class, which the SEC companyfacts feed omits",
	"grossProfit":   "some companies (e.g. payment networks, banks) do not report cost of revenue",
}

// Concepts lists every us-gaap concept the normalizer reads. The fixture
// generator uses it to trim recorded companyfacts files.
func Concepts() []string {
	var out []string
	for _, specs := range [][]fieldSpec{fields, helpers} {
		for _, s := range specs {
			out = append(out, s.concepts...)
		}
	}
	return out
}
