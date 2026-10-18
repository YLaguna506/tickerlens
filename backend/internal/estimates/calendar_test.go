package estimates

import "testing"

func TestParseCalendar(t *testing.T) {
	csv := "symbol,name,reportDate,fiscalDateEnding,estimate,currency,timeOfTheDay\n" +
		"KO,COCA-COLA COMPANY (THE),2026-10-27,2026-09-30,0.87,USD,pre-market\n" +
		"BRK.B,BERKSHIRE HATHAWAY INCORPORATED,2026-11-06,2026-09-30,5.91,USD,\n" +
		"KO,COCA-COLA COMPANY (THE),2027-02-10,2026-12-31,,USD,\n" // a later quarter: the earlier one wins
	cal, err := ParseCalendar([]byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	if ko := cal["KO"]; ko.ReportDate != "2026-10-27" || *ko.EstimateEPS != 0.87 || ko.Time != "pre-market" {
		t.Errorf("KO = %+v", ko)
	}
	if brk, ok := cal["BRK-B"]; !ok || brk.ReportDate != "2026-11-06" {
		t.Errorf("BRK-B (filed as BRK.B) = %+v", brk)
	}
}

func TestCalendarRejectsQuotaNote(t *testing.T) {
	if err := validateCalendar([]byte(`{"Information": "rate limit"}`)); err == nil {
		t.Error("a JSON note must not be cached as the calendar")
	}
}
