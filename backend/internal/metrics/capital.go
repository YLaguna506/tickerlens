package metrics

import (
	"fmt"

	"github.com/YLaguna506/tickerlens/backend/internal/financials"
)

// capitalYears is how many fiscal years the capital allocation record covers.
const capitalYears = 5

// CashUse is one way the company spent (or raised) cash over the period.
type CashUse struct {
	Key    string  `json:"key"` // capex, acquisitions, dividends, buybacks, debtPaydown
	Label  string  `json:"label"`
	Amount float64 `json:"amount"` // USD over the period; negative debt paydown is net borrowing
	// Share of operating cash flow over the period.
	Share *float64 `json:"share"`
}

// CapitalAllocation is how management spent the cash the business made.
type CapitalAllocation struct {
	From              string    `json:"from"` // e.g. FY2021
	To                string    `json:"to"`   // e.g. FY2025
	Years             int       `json:"years"`
	OperatingCashFlow float64   `json:"operatingCashFlow"`
	Uses              []CashUse `json:"uses"`
	// ShareCountChange is the change in diluted shares over the period, in
	// today's share terms (negative: buybacks shrank the count).
	ShareCountChange *float64 `json:"shareCountChange"`
	Notes            []string `json:"notes,omitempty"`
}

// capitalAllocation sums the last five fiscal years' cash flows. A use not
// reported in a year counts as zero, with a note.
func capitalAllocation(f financials.Financials, sp splits) *CapitalAllocation {
	years := f.Annual
	if len(years) > capitalYears {
		years = years[len(years)-capitalYears:]
	}
	var ocf float64
	for _, y := range years {
		if y.OperatingCashFlow == nil {
			return nil // without it, shares of cash flow mean nothing
		}
		ocf += *y.OperatingCashFlow
	}
	if len(years) < 3 || ocf <= 0 {
		return nil
	}
	ca := &CapitalAllocation{
		From: fmt.Sprintf("FY%d", years[0].FiscalYear), To: fmt.Sprintf("FY%d", years[len(years)-1].FiscalYear),
		Years: len(years), OperatingCashFlow: ocf,
	}
	sum := func(field string) (float64, int) {
		var total float64
		n := 0
		for _, y := range years {
			if v := y.Get(field); v != nil {
				total += *v
				n++
			}
		}
		return total, n
	}
	add := func(key, label string, amount float64, reported int) {
		if reported == 0 {
			return
		}
		share := amount / ocf
		ca.Uses = append(ca.Uses, CashUse{Key: key, Label: label, Amount: amount, Share: &share})
		if reported < len(years) {
			ca.Notes = append(ca.Notes, fmt.Sprintf("%s reported for %d of %d years", label, reported, len(years)))
		}
	}
	for _, u := range []struct{ key, label, field string }{
		{"capex", "Capital expenditure", "capex"},
		{"acquisitions", "Acquisitions", "acquisitions"},
		{"dividends", "Dividends", "dividendsPaid"},
		{"buybacks", "Buybacks", "buybacks"},
	} {
		amount, n := sum(u.field)
		add(u.key, u.label, amount, n)
	}
	repaid, n1 := sum("debtRepaid")
	issued, n2 := sum("debtIssued")
	if n1 > 0 || n2 > 0 {
		label := "Net debt repaid"
		if repaid < issued {
			label = "Net new borrowing"
		}
		add("debtPaydown", label, repaid-issued, max(n1, n2))
	}

	first, last := years[0], years[len(years)-1]
	if a, b := sp.shareCount(first, "sharesDiluted"), sp.shareCount(last, "sharesDiluted"); a != nil && b != nil && *a > 0 {
		change := *b / *a - 1
		ca.ShareCountChange = &change
	}
	return ca
}
