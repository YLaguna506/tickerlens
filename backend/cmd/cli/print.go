package main

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
)

var fieldLabels = map[string]string{
	"revenue":              "Revenue",
	"grossProfit":          "Gross profit",
	"operatingIncome":      "Operating income",
	"netIncome":            "Net income",
	"epsDiluted":           "EPS (diluted)",
	"sharesDiluted":        "Diluted shares",
	"operatingCashFlow":    "Operating cash flow",
	"capex":                "Capex",
	"freeCashFlow":         "Free cash flow",
	"cash":                 "Cash",
	"shortTermInvestments": "Short-term investments",
	"totalDebt":            "Total debt",
	"totalEquity":          "Total equity",
	"interestExpense":      "Interest expense",
	"dividendsPaid":        "Dividends paid",
	"buybacks":             "Buybacks",
	"debtRepaid":           "Debt repaid",
	"debtIssued":           "Debt issued",
}

// printBundle writes a human-readable summary.
func printBundle(w io.Writer, b *ingest.Bundle, years, quarters int) {
	fmt.Fprintf(w, "%s (%s) · CIK %s\n\n", b.Company.Name, b.Company.Ticker, b.Company.CIK)
	printFinancials(w, b.Financials, years, quarters)
	printPrices(w, b)
	printEarnings(w, b)

	warnings := append(append([]string{}, b.Financials.Warnings...), b.Warnings...)
	if len(warnings) > 0 {
		fmt.Fprintln(w, "Warnings")
		for _, msg := range warnings {
			fmt.Fprintln(w, "  -", msg)
		}
	}
}

func printFinancials(w io.Writer, f financials.Financials, years, quarters int) {
	cols := append(lastN(f.Annual, years), lastN(f.Quarterly, quarters)...)
	if f.TTM != nil {
		cols = append(cols, *f.TTM)
	}
	if len(cols) == 0 {
		fmt.Fprintln(w, "No financial periods found.")
		return
	}

	fmt.Fprintln(w, "Financials (USD millions; shares in millions; EPS in USD)")
	// tabwriter aligns tab-separated columns; AlignRight right-aligns numbers.
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	header, ends := []string{""}, []string{"period end"}
	for _, p := range cols {
		header = append(header, periodLabel(p))
		ends = append(ends, p.EndDate)
	}
	fmt.Fprintln(tw, strings.Join(header, "\t")+"\t")
	fmt.Fprintln(tw, strings.Join(ends, "\t")+"\t")
	for _, name := range financials.FieldNames() {
		row := []string{fieldLabels[name]}
		for i := range cols {
			row = append(row, formatValue(name, cols[i].Get(name)))
		}
		fmt.Fprintln(tw, strings.Join(row, "\t")+"\t")
	}
	_ = tw.Flush()

	if len(f.Annual) > 0 {
		latest := f.Annual[len(f.Annual)-1]
		fmt.Fprintf(w, "\nSources for %s:\n", periodLabel(latest))
		for _, name := range financials.FieldNames() {
			if src, ok := latest.Sources[name]; ok {
				fmt.Fprintf(w, "  %-22s %s\n", fieldLabels[name], src)
			}
		}
	}
	fmt.Fprintln(w)
}

func printPrices(w io.Writer, b *ingest.Bundle) {
	if len(b.Prices) == 0 {
		return
	}
	first, last := b.Prices[0], b.Prices[len(b.Prices)-1]
	change := (last.AdjClose/first.AdjClose - 1) * 100
	fmt.Fprintf(w, "Prices: %d trading days, %s to %s. Last close $%.2f (%s). Change %+.1f%% (adjusted).\n\n",
		len(b.Prices), first.Date, last.Date, last.Close, last.Date, change)
}

func printEarnings(w io.Writer, b *ingest.Bundle) {
	if len(b.Earnings) == 0 {
		return
	}
	recent := b.Earnings[max(0, len(b.Earnings)-8):]
	fmt.Fprintln(w, "Earnings (EPS reported vs analyst estimate, last 8 quarters)")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "quarter end\treported\testimate\tsurprise\t")
	beats := 0
	for _, e := range recent {
		if e.ReportedEPS != nil && e.EstimatedEPS != nil && *e.ReportedEPS > *e.EstimatedEPS {
			beats++
		}
		surprise := "-"
		if e.SurprisePercent != nil {
			surprise = fmt.Sprintf("%+.1f%%", *e.SurprisePercent)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t\n", e.FiscalDateEnding, formatEPS(e.ReportedEPS), formatEPS(e.EstimatedEPS), surprise)
	}
	_ = tw.Flush()
	fmt.Fprintf(w, "Beat the estimate in %d of %d quarters.\n\n", beats, len(recent))
}

func periodLabel(p financials.FinancialPeriod) string {
	switch p.PeriodType {
	case financials.FiscalYear:
		return fmt.Sprintf("FY%d", p.FiscalYear)
	case financials.FiscalQuarter:
		return fmt.Sprintf("Q%d FY%02d", p.FiscalQuarter, p.FiscalYear%100)
	default:
		return "TTM"
	}
}

func formatValue(field string, v *float64) string {
	if v == nil {
		return "-"
	}
	if field == "epsDiluted" {
		return formatEPS(v)
	}
	return withCommas(*v / 1e6)
}

func formatEPS(v *float64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatFloat(*v, 'f', 2, 64)
}

// withCommas formats a number rounded to an integer with thousands separators.
func withCommas(x float64) string {
	n := int64(math.Round(x))
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return sign + b.String()
}

// lastN returns the last n elements (all of them if there are fewer).
func lastN[T any](s []T, n int) []T {
	return s[max(0, len(s)-n):]
}
