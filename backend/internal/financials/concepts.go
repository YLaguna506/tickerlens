package financials

import "github.com/YLaguna506/tickerlens/backend/internal/edgar"

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
		"RevenuesNetOfInterestExpense", // banks and broker-dealers (JPMorgan, Goldman Sachs)
	}},
	{"grossProfit", flow, "USD", []string{"GrossProfit"}},
	{"operatingIncome", flow, "USD", []string{"OperatingIncomeLoss"}},
	{"netIncome", flow, "USD", []string{
		"NetIncomeLoss", // attributable to the parent company: preferred
		"ProfitLoss",    // includes non-controlling interests
		"NetIncomeLossAvailableToCommonStockholdersBasic",
	}},
	// The per-class concepts are filled in by ingest from filing documents when
	// companyfacts has no EPS (companies with several share classes).
	// Airbnb tags only the continuing-operations EPS.
	{"epsDiluted", flow, "USD/shares", []string{
		"EarningsPerShareDiluted",
		"IncomeLossFromContinuingOperationsPerDilutedShare",
		edgar.ClassEPSConcept,
	}},
	{"epsContinuing", flow, "USD/shares", []string{"IncomeLossFromContinuingOperationsPerDilutedShare"}},
	{"sharesDiluted", average, "shares", []string{"WeightedAverageNumberOfDilutedSharesOutstanding", edgar.ClassSharesConcept}},
	{"operatingCashFlow", flow, "USD", []string{
		"NetCashProvidedByUsedInOperatingActivities",
		"NetCashProvidedByUsedInOperatingActivitiesContinuingOperations",
	}},
	{"capex", flow, "USD", []string{
		"PaymentsToAcquirePropertyPlantAndEquipment",
		"PaymentsToAcquireProductiveAssets",
		"PaymentsToAcquireOtherPropertyPlantAndEquipment", // Eli Lilly
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
		"InterestAndDebtExpense",   // ConocoPhillips
		"InterestExpenseOperating", // REITs since 2022 (Realty Income); banks' is not used
	}},
	{"dividendsPaid", flow, "USD", []string{"PaymentsOfDividends", "PaymentsOfDividendsCommonStock"}},
	{"buybacks", flow, "USD", []string{"PaymentsForRepurchaseOfCommonStock"}},
	{"debtRepaid", flow, "USD", []string{
		"RepaymentsOfLongTermDebt",
		"RepaymentsOfDebt",
		"RepaymentsOfConvertibleDebt",
	}},
	{"acquisitions", flow, "USD", []string{"PaymentsToAcquireBusinessesNetOfCashAcquired"}},
	{"debtIssued", flow, "USD", []string{
		"ProceedsFromIssuanceOfLongTermDebt",
		"ProceedsFromIssuanceOfDebt",
		"ProceedsFromConvertibleDebt",
	}},

	// Banks, insurers and REITs (metrics/sector.go). Other companies rarely
	// report them, so they are optional.
	{"totalAssets", instant, "USD", []string{"Assets"}},
	// Book value per share and return on equity count common shareholders
	// only: preferred stock and its dividends come out.
	{"preferredStock", instant, "USD", []string{
		"PreferredStockValue",
		"PreferredStockIncludingAdditionalPaidInCapital", // Bank of America
		"PreferredStockValueOutstanding",
		"PreferredStockLiquidationPreferenceValue", // JPMorgan
	}},
	{"netIncomeCommon", flow, "USD", []string{"NetIncomeLossAvailableToCommonStockholdersBasic"}},
	{"netInterestIncome", flow, "USD", []string{"InterestIncomeExpenseNet"}},
	{"noninterestIncome", flow, "USD", []string{"NoninterestIncome"}},
	{"noninterestExpense", flow, "USD", []string{"NoninterestExpense"}},
	{"deposits", instant, "USD", []string{"Deposits"}},
	{"loans", instant, "USD", []string{
		"FinancingReceivableExcludingAccruedInterestBeforeAllowanceForCreditLoss",
		"LoansAndLeasesReceivableNetOfDeferredIncome",
		"FinancingReceivableExcludingAccruedInterestAfterAllowanceForCreditLoss",
		"LoansAndLeasesReceivableNetReportedAmount",
	}},
	{"premiumsEarned", flow, "USD", []string{"PremiumsEarnedNet", "PremiumsEarnedNetPropertyAndCasualty"}},
	{"netInvestmentIncome", flow, "USD", []string{"NetInvestmentIncome"}},
	{"claimsIncurred", flow, "USD", []string{
		"PolicyholderBenefitsAndClaimsIncurredNet",
		"IncurredClaimsPropertyCasualtyAndLiability",
	}},
	// Funds from operations (FFO) add back the depreciation of buildings and
	// take out gains on selling them.
	{"depreciation", flow, "USD", []string{
		"DepreciationDepletionAndAmortization",
		"DepreciationAndAmortization",
		"DepreciationAmortizationAndAccretionNet", // American Tower
		"Depreciation",
	}},
	{"propertySaleGains", flow, "USD", []string{
		"GainLossOnSaleOfProperties",
		"GainsLossesOnSalesOfInvestmentRealEstate",
		"GainLossOnDispositionOfAssets1",
		"GainLossOnSaleOfPropertyPlantEquipment",
	}},
	{"realEstateImpairment", flow, "USD", []string{"ImpairmentOfRealEstate"}},
}

// helpers are inputs to derived fields (gross profit fallback, total debt).
// They are not exposed on FinancialPeriod.
var helpers = []fieldSpec{
	{"costOfRevenue", flow, "USD", []string{"CostOfRevenue", "CostOfGoodsAndServicesSold"}},
	{"debtTotal", instant, "USD", []string{
		"LongTermDebt",
		"LongTermDebtAndCapitalLeaseObligationsIncludingCurrentMaturities", // Chevron
	}},
	{"debtCurrentAll", instant, "USD", []string{"DebtCurrent"}}, // current LTD + short-term borrowings + CP
	{"debtCurrent", instant, "USD", []string{
		"LongTermDebtCurrent",
		"LongTermDebtAndCapitalLeaseObligationsCurrent", // Coca-Cola
	}},
	{"debtNoncurrent", instant, "USD", []string{"LongTermDebtNoncurrent"}},
	// Non-current long-term debt including finance leases (PepsiCo's 10-Qs).
	{"debtNoncurrentLeases", instant, "USD", []string{"LongTermDebtAndCapitalLeaseObligations"}},
	{"commercialPaper", instant, "USD", []string{"CommercialPaper"}},
	{"shortTermBorrowings", instant, "USD", []string{
		"ShortTermBorrowings",
		"NotesAndLoansPayable", // Coca-Cola's "loans and notes payable", which include commercial paper
	}},
	{"convertibleNoncurrent", instant, "USD", []string{"ConvertibleDebtNoncurrent"}},
	// Unsecured notes and mortgages, for companies that tag no long-term
	// debt otherwise (Realty Income).
	{"notesPayable", instant, "USD", []string{"NotesPayable"}},
	{"securedDebt", instant, "USD", []string{"SecuredDebt"}},
	{"convertibleCurrent", instant, "USD", []string{"ConvertibleNotesPayableCurrent"}},
}

// nonNegative fields cannot be below zero. A derived quarter that is, was
// computed from a year restated after its quarters were filed.
var nonNegative = map[string]bool{"revenue": true, "capex": true, "dividendsPaid": true, "buybacks": true}

// optional fields are often absent for good reason and get no warning.
var optional = map[string]bool{"epsContinuing": true, "acquisitions": true,
	"totalAssets": true, "preferredStock": true, "netIncomeCommon": true, "netInterestIncome": true, "noninterestIncome": true, "noninterestExpense": true,
	"deposits": true, "loans": true, "premiumsEarned": true, "netInvestmentIncome": true, "claimsIncurred": true,
	"depreciation": true, "propertySaleGains": true, "realEstateImpairment": true}

// missingHints explains common reasons a field has no data at all.
var missingHints = map[string]string{
	"epsDiluted":    "companies with several share classes (e.g. Visa) report EPS per class, which the SEC companyfacts feed omits",
	"sharesDiluted": "companies with several share classes report share counts per class, which the SEC companyfacts feed omits",
	"grossProfit":   "some companies (e.g. payment networks, banks) do not report cost of revenue",
}

// UnitOf returns the unit a concept's values are stored under ("USD",
// "USD/shares" or "shares").
func UnitOf(concept string) string {
	for _, specs := range [][]fieldSpec{fields, helpers} {
		for _, s := range specs {
			for _, c := range s.concepts {
				if c == concept {
					return s.unit
				}
			}
		}
	}
	return "USD"
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
