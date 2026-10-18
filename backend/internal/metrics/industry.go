package metrics

// LimitedSupport flags companies whose accounts the metrics were not built
// for. TickerLens reads operating companies: revenue, margins, debt and free
// cash flow. Banks, broker-dealers, insurers and REITs report on other terms.
type LimitedSupport struct {
	Kind   string `json:"kind"`   // bank | broker | insurer | reit
	Reason string `json:"reason"` // what does not fit, shown on the report
	// NoHealthScore: debt ratios do not apply, so the health score is left out.
	NoHealthScore bool `json:"noHealthScore"`
}

// Standard Industrial Classification codes, by kind.
var limitedSIC = map[string]string{
	"6021": "bank", "6022": "bank", "6029": "bank", "6035": "bank", "6036": "bank",
	"6211": "broker",
	"6311": "insurer", "6331": "insurer", "6351": "insurer", "6361": "insurer", "6399": "insurer",
	"6798": "reit",
}

var limitedReasons = map[string]string{
	"bank": "Banks lend out their depositors' money: deposits are their main debt and interest is their revenue, " +
		"so debt ratios, margins and free cash flow mean something different here. The financial health score is left out; " +
		"book value (P/B) and return on equity say more.",
	"broker": "Broker-dealers fund their trading and client accounts with borrowed money, so debt ratios and free cash flow " +
		"mean something different here. The financial health score is left out.",
	"insurer": "Insurers invest the premiums they hold for future claims (the float) and report reserves for those claims " +
		"as liabilities, so debt ratios and margins read differently. The financial health score is left out.",
	"reit": "REITs must pay out most of their income and are judged on funds from operations (FFO), which TickerLens does " +
		"not compute; depreciation of their properties makes P/E look high.",
}

// limitedSupport returns the flag for an industry code, or nil.
func limitedSupport(sic string) *LimitedSupport {
	kind, ok := limitedSIC[sic]
	if !ok {
		return nil
	}
	return &LimitedSupport{Kind: kind, Reason: limitedReasons[kind], NoHealthScore: kind != "reit"}
}
