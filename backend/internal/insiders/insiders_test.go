package insiders

import (
	"os"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
)

func TestSummarizeKeepsOpenMarketTrades(t *testing.T) {
	trades := []edgar.InsiderTrade{
		{Name: "CEO", Date: "2026-03-01", Code: "S", Shares: 1000, Price: 100, Planned: true},
		{Name: "CEO", Date: "2026-04-01", Code: "S", Shares: 500, Price: 110},
		{Name: "Director", Date: "2026-05-01", Code: "P", Shares: 2000, Price: 90},
		{Name: "CFO", Date: "2026-05-02", Code: "A", Shares: 9000, Price: 0},  // grant: pay, not a decision
		{Name: "CFO", Date: "2026-05-03", Code: "F", Shares: 3000, Price: 95}, // tax withholding
		{Name: "Old", Date: "2025-01-01", Code: "P", Shares: 1, Price: 1},     // before the window
	}
	s := Summarize(trades, 6, "2025-10-18", "2026-10-18")
	if s.Purchases != 1 || s.Sales != 2 || s.Buyers != 1 || s.Sellers != 1 {
		t.Errorf("counts = %+v", s)
	}
	if s.BoughtUSD != 180_000 || s.SoldUSD != 155_000 || s.PlannedSoldUSD != 100_000 {
		t.Errorf("values bought %v sold %v planned %v", s.BoughtUSD, s.SoldUSD, s.PlannedSoldUSD)
	}
	if s.Signal != "buying" || len(s.Trades) != 3 || s.Trades[0].Date != "2026-05-01" {
		t.Errorf("signal %q, trades %+v", s.Signal, s.Trades)
	}
	if none := Summarize(nil, 0, "2025-10-18", "2026-10-18"); none.Signal != "none" || none.Trades == nil {
		t.Errorf("empty summary = %+v", none)
	}
}

func TestParseForm4(t *testing.T) {
	doc, err := os.ReadFile("testdata/form4.xml")
	if err != nil {
		t.Fatal(err)
	}
	trades, err := edgar.ParseForm4(doc, edgar.Filing{Accession: "0001-26-1", FilingDate: "2026-10-09"})
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 2 {
		t.Fatalf("trades = %+v", trades)
	}
	sale := trades[1]
	if sale.Name != "DOE JANE" || sale.Role != "Chief Executive Officer, Director" || sale.Code != "S" ||
		sale.Shares != 10000 || sale.Price != 181.25 || sale.Acquired || !sale.Planned || *sale.OwnedAfter != 250000 {
		t.Errorf("sale = %+v", sale)
	}
	if trades[0].Code != "M" || !trades[0].Acquired {
		t.Errorf("exercise = %+v", trades[0])
	}
}
