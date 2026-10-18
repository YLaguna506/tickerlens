package edgar

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/YLaguna506/tickerlens/backend/internal/fetch"
)

// newFixtureServer serves files from testdata/edgar and records the
// User-Agent of each request.
func newFixtureServer(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var agents []string
	dir := filepath.Join("..", "..", "testdata", "edgar")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents = append(agents, r.Header.Get("User-Agent"))
		var file string
		switch r.URL.Path {
		case "/files/company_tickers.json":
			file = "company_tickers.json"
		case "/api/xbrl/companyfacts/CIK0001403161.json":
			file = "companyfacts_V.json"
		default:
			http.NotFound(w, r)
			return
		}
		body, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Errorf("read fixture: %v", err)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	f := fetch.New(fetch.Options{
		Name:   "sec",
		Header: http.Header{"User-Agent": {"TickerLens test@example.com"}},
	})
	return NewClient(f, Options{WWWBaseURL: srv.URL, DataBaseURL: srv.URL}), &agents
}

func TestLookupTicker(t *testing.T) {
	c, agents := newFixtureServer(t)

	tests := []struct {
		in      string
		wantCIK string
		wantErr error
	}{
		{in: "V", wantCIK: "0001403161"},
		{in: " aapl ", wantCIK: "0000320193"},
		{in: "brk.b", wantCIK: "0001067983"},
		{in: "NOPE", wantErr: ErrUnknownTicker},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := c.LookupTicker(t.Context(), tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.CIK != tc.wantCIK {
				t.Errorf("CIK = %s, want %s", got.CIK, tc.wantCIK)
			}
		})
	}

	if len(*agents) == 0 || (*agents)[0] != "TickerLens test@example.com" {
		t.Errorf("SEC requests must carry the configured User-Agent; got %v", *agents)
	}
}

func TestCompanyFacts(t *testing.T) {
	c, _ := newFixtureServer(t)

	facts, err := c.CompanyFacts(t.Context(), "0001403161")
	if err != nil {
		t.Fatal(err)
	}
	if facts.EntityName != "VISA INC." {
		t.Errorf("EntityName = %q", facts.EntityName)
	}
	if _, ok := facts.Facts["us-gaap"]["NetIncomeLoss"]; !ok {
		t.Error("expected us-gaap NetIncomeLoss in fixture")
	}
}

// Some new registrants' feeds have the CIK as a string (ExxonMobil Holdings).
func TestCompanyFactsCIKAsString(t *testing.T) {
	for _, raw := range []string{`{"cik": 2115436}`, `{"cik": "2115436"}`} {
		var cf CompanyFacts
		if err := json.Unmarshal([]byte(raw), &cf); err != nil || cf.CIK.String() != "2115436" {
			t.Errorf("%s: cik %q, err %v", raw, cf.CIK, err)
		}
	}
}
