package peers

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
)

type fakeSource struct {
	sic     string
	members map[string][]string                    // SIC → CIKs (unpadded, as EDGAR lists them)
	frames  map[string]map[string]edgar.FrameValue // "concept:year" → CIK → value
}

func (f fakeSource) Profile(context.Context, string) (edgar.Profile, error) {
	return edgar.Profile{SIC: f.sic, SICDescription: "Beverages"}, nil
}

func (f fakeSource) CompaniesBySIC(_ context.Context, sic string) ([]string, error) {
	return f.members[sic], nil
}

func (f fakeSource) Frame(_ context.Context, concept string, year int) (map[string]edgar.FrameValue, error) {
	return f.frames[fmt.Sprintf("%s:%d", concept, year)], nil
}

func (f fakeSource) Tickers(context.Context) ([]edgar.Company, error) {
	return []edgar.Company{{Ticker: "KO", CIK: edgar.PadCIK(21344)}, {Ticker: "PEP", CIK: edgar.PadCIK(77476)}}, nil
}

// company adds a company's figures (in billions) to the frames.
func (f fakeSource) company(cik int, name string, revenue, prior, operating, net float64) {
	key := edgar.PadCIK(cik)
	set := func(concept string, year int, v float64) {
		k := fmt.Sprintf("%s:%d", concept, year)
		if f.frames[k] == nil {
			f.frames[k] = map[string]edgar.FrameValue{}
		}
		f.frames[k][key] = edgar.FrameValue{Name: name, Val: v * 1e9}
	}
	set("Revenues", 2025, revenue)
	set("Revenues", 2024, prior)
	set("OperatingIncomeLoss", 2025, operating)
	set("NetIncomeLoss", 2025, net)
}

func TestBuildWidensToIndustryGroupAndRanks(t *testing.T) {
	src := fakeSource{sic: "2080", frames: map[string]map[string]edgar.FrameValue{}, members: map[string][]string{
		"2080": {"77476", "21344", "1"},
		"2086": {"2", "3", "4"},
	}}
	src.company(77476, "PepsiCo", 93.9, 91.8, 11.5, 8.2)
	src.company(21344, "Coca-Cola", 47.9, 47, 13.7, 13.1)
	src.company(1, "Tiny Wine", 0.01, 0.01, 0, 0) // under the revenue floor
	src.company(2, "Keurig Dr Pepper", 16.6, 15.4, 3.6, 2.1)
	src.company(3, "Monster", 8, 7.5, 2.4, 1.9)
	src.company(4, "Misfiled Inc", 103.5, 54, 0.00001, 0.00001) // revenue in the wrong scale

	g, err := Build(t.Context(), src, edgar.PadCIK(77476), time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	// 2080 alone has one peer with enough revenue: widened to 208x.
	if g.Basis != "SIC 208x (industry group)" || g.Year != 2025 {
		t.Errorf("basis %q, year %d", g.Basis, g.Year)
	}
	if len(g.Peers) != 3 || g.Peers[0].Name != "Coca-Cola" || g.Peers[0].Ticker != "KO" {
		t.Fatalf("peers = %+v", g.Peers)
	}
	if g.Rank != 1 || g.Of != 4 {
		t.Errorf("rank %d of %d, want 1 of 4", g.Rank, g.Of)
	}
	if g.Company == nil || g.Company.Ticker != "PEP" || math.Abs(*g.Company.OperatingMargin-11.5/93.9) > 1e-9 {
		t.Errorf("company = %+v", g.Company)
	}
	// Median operating margin of KO 28.6%, KDP 21.7%, Monster 30%.
	if m := g.Medians.OperatingMargin; m == nil || math.Abs(*m-13.7/47.9) > 1e-9 {
		t.Errorf("median operating margin = %v", m)
	}
}

func TestFigureYear(t *testing.T) {
	if y := FigureYear(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)); y != 2024 {
		t.Errorf("February 2026 → %d, want 2024 (10-Ks for 2025 still coming in)", y)
	}
	if y := FigureYear(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); y != 2025 {
		t.Errorf("October 2026 → %d, want 2025", y)
	}
}
