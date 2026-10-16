package main

import (
	"fmt"
	"io"
	"math"
	"strings"
	"text/tabwriter"

	"github.com/YLaguna506/tickerlens/backend/internal/ingest"
	"github.com/YLaguna506/tickerlens/backend/internal/metrics"
	"github.com/YLaguna506/tickerlens/backend/internal/scoring"
	"github.com/YLaguna506/tickerlens/backend/internal/verdict"
)

// printReport writes the Financial overview, phase and scores.
func printReport(w io.Writer, r *metrics.Report, b *ingest.Bundle) {
	fmt.Fprintf(w, "%s (%s)\n", r.Name, r.Ticker)
	fmt.Fprintf(w, "Price %s on %s · market cap %s\n", formatMetricValue(metrics.PerShare, r.Price), r.PriceDate,
		formatMetricValue(metrics.USD, r.MarketCap.Value))
	fmt.Fprintf(w, "Financials through %s (latest fiscal year FY%d) · scoring %s\n\n", r.FinancialsTo, r.LatestFiscalYr, r.ScoringVersion)

	ph := r.Phase
	conf := ""
	if ph.LowConfidence {
		conf = " (low confidence)"
	}
	fmt.Fprintf(w, "Phase %d · %s%s · valued with %s\n  %s\n", ph.Number, ph.Name, conf, ph.ValuationMetric, ph.Reason)
	for _, c := range ph.Checks {
		mark := "?"
		if c.Answer != nil {
			mark = map[bool]string{true: "✓", false: "✗"}[*c.Answer]
		}
		fmt.Fprintf(w, "  %s %s  %s\n", mark, c.Question, c.Detail)
	}
	fmt.Fprintln(w)

	printMetrics(w, "Profitability", r.Profitability)
	printMetrics(w, "Growth", r.Growth)
	printMetrics(w, "Financial health", r.Health)
	printMetrics(w, "Valuation", r.Valuation)
	printMetrics(w, "Shareholder returns", r.Returns)
	printMetrics(w, "Price", []metrics.Metric{r.PriceChange5y})

	fmt.Fprintln(w, "Scores")
	printScore(w, "Growth", r.GrowthScore)
	printScore(w, "Financial health", r.HealthScore)
	printScore(w, "Valuation", r.ValuationScore)
	fmt.Fprintln(w)

	if bd := r.Bands; bd != nil {
		fmt.Fprintf(w, "Valuation bands (%s, %s to %s, %d trading days)\n", bd.Metric, bd.HistoryFrom, bd.HistoryTo, bd.Days)
		fmt.Fprintf(w, "  cheap below %.1f× · expensive above %.1f× · today %.1f× → %s (higher than %.0f%% of the period)\n",
			bd.Cheap, bd.Expensive, bd.Current, bd.Zone, bd.Percentile*100)
		fmt.Fprintf(w, "  price bar on %s $%.2f (%s): cheap below $%.2f · expensive above $%.2f · today $%.2f\n\n",
			bd.PerShareBasis, bd.PerShare, bd.BasisPeriod, bd.CheapPrice, bd.ExpensivePrice, bd.Price)
	}

	e := r.Earnings
	if e.Quarters > 0 {
		avg := "n/a"
		if e.AvgSurprisePct != nil {
			avg = fmt.Sprintf("%+.1f%%", *e.AvgSurprisePct)
		}
		fmt.Fprintf(w, "Earnings: beat the EPS estimate in %d of %d quarters (average surprise %s)\n\n", e.Beats, e.Quarters, avg)
	}

	notes := append(append([]string{}, r.Notes...), b.Notes...)
	notes = append(notes, b.Warnings...)
	if len(notes) > 0 {
		fmt.Fprintln(w, "Notes")
		for _, n := range notes {
			fmt.Fprintln(w, "  -", n)
		}
	}
}

// printVerdict writes the verdict and how it was reached.
func printVerdict(w io.Writer, v verdict.Result) {
	fmt.Fprintln(w)
	if v.Label == "" {
		fmt.Fprintf(w, "Verdict: none (%s)\n", v.Rule)
		return
	}
	fmt.Fprintf(w, "Verdict: %s · %s\n", strings.ToUpper(v.Label), v.Meaning)
	fmt.Fprintf(w, "  %s\n", v.Rule)
	parts := []string{}
	for _, c := range v.Components {
		score := "n/a"
		if c.Score != nil {
			score = fmt.Sprintf("%.1f", *c.Score)
		}
		parts = append(parts, fmt.Sprintf("%s %s × %.0f%%", c.Name, score, c.Weight*100))
	}
	fmt.Fprintf(w, "  B = %s\n", strings.Join(parts, " + "))
	fmt.Fprintf(w, "  Confidence: %s (%d/100): %s\n", v.ConfidenceLevel, v.Confidence, strings.Join(v.ConfidenceReasons, "; "))
	fmt.Fprintln(w, "  (The CLI runs no AI agents. Request the report through the API for the full verdict.)")
}

func printMetrics(w io.Writer, title string, ms []metrics.Metric) {
	fmt.Fprintln(w, title)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, m := range ms {
		detail := m.Period
		if m.Note != "" {
			detail = m.Note
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", m.Label, formatMetricValue(m.Unit, m.Value), detail)
	}
	_ = tw.Flush()
	fmt.Fprintln(w)
}

func printScore(w io.Writer, name string, s scoring.Score) {
	value := "n/a"
	if s.Value != nil {
		value = fmt.Sprintf("%.1f/5", *s.Value)
	}
	fmt.Fprintf(w, "  %-17s %-6s %s\n", name, value, strings.Join(s.Reasons, "; "))
}

func formatMetricValue(u metrics.Unit, v *float64) string {
	if v == nil {
		return "-"
	}
	switch u {
	case metrics.Percent:
		return fmt.Sprintf("%.1f%%", *v*100)
	case metrics.Multiple:
		return fmt.Sprintf("%.1f×", *v)
	case metrics.PerShare:
		return fmt.Sprintf("$%.2f", *v)
	case metrics.USD:
		abs := math.Abs(*v)
		switch {
		case abs >= 1e12:
			return fmt.Sprintf("$%.2fT", *v/1e12)
		case abs >= 1e9:
			return fmt.Sprintf("$%.1fB", *v/1e9)
		default:
			return fmt.Sprintf("$%sM", withCommas(*v/1e6))
		}
	}
	return fmt.Sprint(*v)
}
