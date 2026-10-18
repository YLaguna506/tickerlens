package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/peers"
)

func runPeers(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs, c := newFlags("peers", stderr)
	ticker, err := parse(fs, args)
	if err != nil {
		return err
	}
	f, err := newFetcher(ctx, c, stderr)
	if err != nil {
		return err
	}
	co, err := f.SEC.LookupTicker(ctx, ticker)
	if err != nil {
		return err
	}
	g, err := peers.Build(ctx, f.SEC, co.CIK, time.Now())
	if err != nil {
		return err
	}
	if *c.asJSON {
		return writeJSON(stdout, g)
	}
	fmt.Fprintf(stdout, "%s: %s (%s), CY%d, rank %d of %d by revenue\n\n", co.Ticker, g.Industry, g.Basis, g.Year, g.Rank, g.Of)
	row := func(name string, c peers.Company) {
		fmt.Fprintf(stdout, "%-36.36s %10s %8s %8s %8s %8s\n", name, money(&c.Revenue),
			pct(c.RevenueGrowth), pct(c.GrossMargin), pct(c.OperatingMargin), pct(c.NetMargin))
	}
	fmt.Fprintf(stdout, "%-36s %10s %8s %8s %8s %8s\n", "", "revenue", "growth", "gross", "oper.", "net")
	if g.Company != nil {
		row("» "+g.Company.Name, *g.Company)
	}
	for _, p := range g.Peers {
		row(fmt.Sprintf("%s (%s)", p.Name, p.Ticker), p)
	}
	m := g.Medians
	fmt.Fprintf(stdout, "%-36s %10s %8s %8s %8s %8s\n", "median of peers", money(m.Revenue), pct(m.RevenueGrowth), pct(m.GrossMargin), pct(m.OperatingMargin), pct(m.NetMargin))
	return nil
}

func money(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("$%.1fB", *v/1e9)
}

func pct(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", *v*100)
}
