// Package insiders summarizes what a company's directors and officers did
// with their own shares: open-market purchases and sales from Form 4 filings.
//
// Grants, option exercises, tax withholding and gifts are left out: they are
// pay, not decisions to buy or sell. Insiders sell for many reasons (taxes,
// a house, diversifying), often under a trading plan set up months ahead;
// buying with their own money on the open market is the rarer signal.
package insiders

import (
	"sort"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
)

// MaxTrades is how many of the latest trades the report lists.
const MaxTrades = 25

// Summary is a year of insider buying and selling.
type Summary struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Filings int    `json:"filings"` // Form 4s read
	// Open-market purchases (code P) and sales (code S).
	Purchases      int     `json:"purchases"`
	Sales          int     `json:"sales"`
	Buyers         int     `json:"buyers"`  // distinct insiders who bought
	Sellers        int     `json:"sellers"` // distinct insiders who sold
	BoughtUSD      float64 `json:"boughtUsd"`
	SoldUSD        float64 `json:"soldUsd"`
	PlannedSoldUSD float64 `json:"plannedSoldUsd"` // sold under Rule 10b5-1 plans
	// Signal: "buying" (more bought than sold), "selling", or "none".
	Signal string `json:"signal"`
	// Trades are the latest open-market trades, newest first.
	Trades []edgar.InsiderTrade `json:"trades"`
}

// Summarize keeps the open-market trades dated from..to.
func Summarize(trades []edgar.InsiderTrade, filings int, from, to string) Summary {
	s := Summary{From: from, To: to, Filings: filings, Signal: "none", Trades: []edgar.InsiderTrade{}}
	buyers, sellers := map[string]bool{}, map[string]bool{}
	for _, t := range trades {
		if t.Date < from || t.Date > to {
			continue
		}
		value := t.Shares * t.Price
		switch t.Code {
		case "P":
			s.Purchases++
			s.BoughtUSD += value
			buyers[t.Name] = true
		case "S":
			s.Sales++
			s.SoldUSD += value
			sellers[t.Name] = true
			if t.Planned {
				s.PlannedSoldUSD += value
			}
		default:
			continue
		}
		s.Trades = append(s.Trades, t)
	}
	s.Buyers, s.Sellers = len(buyers), len(sellers)
	switch {
	case s.BoughtUSD > s.SoldUSD:
		s.Signal = "buying"
	case s.SoldUSD > 0:
		s.Signal = "selling"
	}
	sort.SliceStable(s.Trades, func(i, j int) bool { return s.Trades[i].Date > s.Trades[j].Date })
	if len(s.Trades) > MaxTrades {
		s.Trades = s.Trades[:MaxTrades]
	}
	return s
}
