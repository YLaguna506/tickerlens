package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/financials"
	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
)

func TestWithCommas(t *testing.T) {
	tests := map[float64]string{
		0:          "0",
		999:        "999",
		1000:       "1,000",
		391035:     "391,035",
		-1289.2:    "-1,289",
		1234567.6:  "1,234,568",
		-1000000.0: "-1,000,000",
	}
	for in, want := range tests {
		if got := withCommas(in); got != want {
			t.Errorf("withCommas(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestPrintBundle(t *testing.T) {
	rev, eps := 391_035e6, 6.08
	b := &ingest.Bundle{Financials: financials.Financials{
		Annual: []financials.FinancialPeriod{{
			PeriodType: financials.FiscalYear, FiscalYear: 2024, EndDate: "2024-09-28",
			Revenue: &rev, EPSDiluted: &eps,
			Sources: map[string]string{"revenue": "RevenueFromContractWithCustomerExcludingAssessedTax"},
		}},
	}}
	b.Company.Name, b.Company.Ticker = "Apple Inc.", "AAPL"

	var out bytes.Buffer
	printBundle(&out, b, 5, 4)

	for _, want := range []string{"Apple Inc. (AAPL)", "FY2024", "391,035", "6.08", "RevenueFromContractWithCustomerExcludingAssessedTax"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}
