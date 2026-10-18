// Package peers builds a company's comparison group: the largest companies
// filing under the same SEC industry code (SIC), with margins and growth from
// the SEC's cross-company "frames" (every filer's value of a concept for a
// calendar year). The moat section compares the company with them.
package peers

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
)

// Source is the SEC data the group is built from (edgar.Client).
type Source interface {
	Profile(ctx context.Context, cik string) (edgar.Profile, error)
	CompaniesBySIC(ctx context.Context, sic string) ([]string, error)
	Frame(ctx context.Context, concept string, year int) (map[string]edgar.FrameValue, error)
	Tickers(ctx context.Context) ([]edgar.Company, error)
}

const (
	// MaxPeers is how many of the largest peers are compared.
	MaxPeers = 10
	// minPeers below this, the industry code is widened to its group (the
	// first three digits).
	minPeers = 5
	// minRevenue leaves out shells and startups.
	minRevenue = 50e6
)

// Company is one company's figures for the group's calendar year.
type Company struct {
	CIK             string   `json:"cik"`
	Ticker          string   `json:"ticker,omitempty"`
	Name            string   `json:"name"`
	Revenue         float64  `json:"revenue"`
	RevenueGrowth   *float64 `json:"revenueGrowth"` // vs the year before
	GrossMargin     *float64 `json:"grossMargin"`
	OperatingMargin *float64 `json:"operatingMargin"`
	NetMargin       *float64 `json:"netMargin"`
}

// Medians are the peers' medians (nil when fewer than three report it).
type Medians struct {
	Revenue         *float64 `json:"revenue"`
	RevenueGrowth   *float64 `json:"revenueGrowth"`
	GrossMargin     *float64 `json:"grossMargin"`
	OperatingMargin *float64 `json:"operatingMargin"`
	NetMargin       *float64 `json:"netMargin"`
}

// Group is a company's comparison group.
type Group struct {
	SIC      string `json:"sic"`
	Industry string `json:"industry"`
	// Basis says which code the peers share: the company's own, or its
	// three-digit industry group when the code alone has too few companies.
	Basis   string    `json:"basis"`
	Year    int       `json:"year"`    // calendar year of the figures
	Company *Company  `json:"company"` // the company itself, from the same data (nil if absent)
	Peers   []Company `json:"peers"`   // the largest peers, largest first
	Medians Medians   `json:"medians"`
	// Rank is the company's place by revenue among itself and every peer
	// with data (1 = largest), out of Of.
	Rank int `json:"rank,omitempty"`
	Of   int `json:"of"`
}

// FigureYear is the latest calendar year most companies have reported: the
// previous one from April on (10-Ks are due within 60–90 days).
func FigureYear(now time.Time) int {
	if now.Month() >= time.April {
		return now.Year() - 1
	}
	return now.Year() - 2
}

type frames struct {
	revenue, revenuePrior, gross, operating, net map[string]float64
	names                                        map[string]string // CIK → company name
}

// Build returns the company's comparison group.
func Build(ctx context.Context, src Source, cik string, now time.Time) (*Group, error) {
	prof, err := src.Profile(ctx, cik)
	if err != nil {
		return nil, err
	}
	if len(prof.SIC) != 4 {
		return nil, fmt.Errorf("no industry code (SIC) on file")
	}
	year := FigureYear(now)
	fr, err := loadFrames(ctx, src, year)
	if err != nil {
		return nil, err
	}

	g := &Group{SIC: prof.SIC, Industry: prof.SICDescription, Year: year, Basis: "SIC " + prof.SIC}
	members, err := src.CompaniesBySIC(ctx, prof.SIC)
	if err != nil {
		return nil, err
	}
	companies := fr.companies(members, cik)
	if len(companies) < minPeers {
		// Widen to the industry group: 2080 → 2080–2089.
		members = nil
		for d := '0'; d <= '9'; d++ {
			if more, err := src.CompaniesBySIC(ctx, prof.SIC[:3]+string(d)); err == nil {
				members = append(members, more...)
			}
		}
		companies = fr.companies(members, cik)
		g.Basis = fmt.Sprintf("SIC %sx (industry group)", prof.SIC[:3])
	}
	if len(companies) == 0 {
		return nil, fmt.Errorf("no companies with %d revenue data under %s", year, g.Basis)
	}

	if self, ok := fr.company(cik); ok {
		g.Company = &self
	}
	all := append([]Company(nil), companies...)
	if g.Company != nil {
		all = append(all, *g.Company)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Revenue > all[j].Revenue })
	g.Of = len(all)
	for i, c := range all {
		if c.CIK == cik {
			g.Rank = i + 1
		}
	}
	sort.Slice(companies, func(i, j int) bool { return companies[i].Revenue > companies[j].Revenue })
	g.Peers = companies[:min(MaxPeers, len(companies))]
	g.Medians = medians(g.Peers)

	if tickers, err := src.Tickers(ctx); err == nil {
		byCIK := map[string]string{}
		for _, t := range tickers {
			if _, seen := byCIK[t.CIK]; !seen { // the first listed is the main class
				byCIK[t.CIK] = t.Ticker
			}
		}
		for i := range g.Peers {
			g.Peers[i].Ticker = byCIK[g.Peers[i].CIK]
		}
		if g.Company != nil {
			g.Company.Ticker = byCIK[cik]
		}
	}
	return g, nil
}

func loadFrames(ctx context.Context, src Source, year int) (*frames, error) {
	get := func(concept string, y int) (map[string]float64, map[string]string, error) {
		m, err := src.Frame(ctx, concept, y)
		if err != nil {
			return nil, nil, err
		}
		vals, names := map[string]float64{}, map[string]string{}
		for cik, v := range m {
			vals[cik], names[cik] = v.Val, v.Name
		}
		return vals, names, nil
	}
	// Revenue is under either concept; the larger is the total.
	revenue := func(y int) (map[string]float64, map[string]string, error) {
		a, names, err := get("Revenues", y)
		if err != nil {
			return nil, nil, err
		}
		b, names2, err := get("RevenueFromContractWithCustomerExcludingAssessedTax", y)
		if err != nil {
			return nil, nil, err
		}
		for cik, v := range b {
			if v > a[cik] {
				a[cik] = v
			}
			if names[cik] == "" {
				names[cik] = names2[cik]
			}
		}
		return a, names, nil
	}
	f := &frames{}
	var err error
	if f.revenue, f.names, err = revenue(year); err != nil {
		return nil, err
	}
	if f.revenuePrior, _, err = revenue(year - 1); err != nil {
		return nil, err
	}
	for concept, dst := range map[string]*map[string]float64{
		"GrossProfit": &f.gross, "OperatingIncomeLoss": &f.operating, "NetIncomeLoss": &f.net,
	} {
		if *dst, _, err = get(concept, year); err != nil {
			return nil, err
		}
	}
	return f, nil
}

func (f *frames) company(cik string) (Company, bool) {
	rev, ok := f.revenue[cik]
	if !ok || rev <= 0 {
		return Company{}, false
	}
	c := Company{CIK: cik, Name: f.names[cik], Revenue: rev}
	ratio := func(m map[string]float64) *float64 {
		if v, ok := m[cik]; ok {
			r := v / rev
			return &r
		}
		return nil
	}
	c.GrossMargin, c.OperatingMargin, c.NetMargin = ratio(f.gross), ratio(f.operating), ratio(f.net)
	if prior, ok := f.revenuePrior[cik]; ok && prior > 0 {
		g := rev/prior - 1
		c.RevenueGrowth = &g
	}
	return c, true
}

// companies returns the members with revenue data, without the company.
func (f *frames) companies(members []string, self string) []Company {
	seen := map[string]bool{self: true}
	var out []Company
	for _, cik := range members {
		if n, err := strconv.Atoi(cik); err == nil {
			cik = edgar.PadCIK(n)
		}
		if seen[cik] {
			continue
		}
		seen[cik] = true
		if c, ok := f.company(cik); ok && c.Revenue >= minRevenue && !misScaled(c) {
			out = append(out, c)
		}
	}
	return out
}

// misScaled catches a filer whose revenue is in the wrong scale (Tigo
// Energy's CY2025 revenue reads $103.5B): both margins are then almost
// exactly zero, which real companies rarely are.
func misScaled(c Company) bool {
	tiny := func(v *float64) bool { return v != nil && *v > -0.001 && *v < 0.001 }
	return tiny(c.OperatingMargin) && tiny(c.NetMargin)
}

func medians(cs []Company) Medians {
	pick := func(get func(Company) *float64) *float64 {
		var vals []float64
		for _, c := range cs {
			if v := get(c); v != nil {
				vals = append(vals, *v)
			}
		}
		if len(vals) < 3 {
			return nil
		}
		slices.Sort(vals)
		m := vals[len(vals)/2]
		if len(vals)%2 == 0 {
			m = (vals[len(vals)/2-1] + m) / 2
		}
		return &m
	}
	return Medians{
		Revenue:         pick(func(c Company) *float64 { return &c.Revenue }),
		RevenueGrowth:   pick(func(c Company) *float64 { return c.RevenueGrowth }),
		GrossMargin:     pick(func(c Company) *float64 { return c.GrossMargin }),
		OperatingMargin: pick(func(c Company) *float64 { return c.OperatingMargin }),
		NetMargin:       pick(func(c Company) *float64 { return c.NetMargin }),
	}
}
