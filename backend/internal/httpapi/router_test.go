package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YLaguna506/tickerlens/backend/internal/edgar"
	"github.com/YLaguna506/tickerlens/backend/internal/pipeline"
	"github.com/YLaguna506/tickerlens/backend/internal/store"
)

// --- Fakes ------------------------------------------------------------------------

type fakeReports struct {
	mu      sync.Mutex
	reports map[string]store.Report
	latest  *store.Report // returned by LatestFinished
}

func (f *fakeReports) Create(_ context.Context, ticker, name string) (store.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := store.Report{ID: "11111111-1111-1111-1111-111111111111", Ticker: ticker, CompanyName: name, Status: store.StatusQueued}
	f.reports[r.ID] = r
	return r, nil
}

func (f *fakeReports) Get(_ context.Context, id string) (store.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.reports[id]
	if !ok {
		return r, store.ErrNotFound
	}
	return r, nil
}

func (f *fakeReports) LatestFinished(context.Context, string, time.Time) (store.Report, error) {
	if f.latest == nil {
		return store.Report{}, store.ErrNotFound
	}
	return *f.latest, nil
}

func (f *fakeReports) ListByTicker(_ context.Context, ticker string, _ int) ([]store.Report, error) {
	return []store.Report{{ID: "x", Ticker: ticker, Status: store.StatusDone}}, nil
}

type fakeJobs struct {
	mu      sync.Mutex
	started []string
}

func (f *fakeJobs) Start(_, ticker string, _ bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, ticker)
}

func (f *fakeJobs) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.started...)
}

func (f *fakeReports) put(r store.Report) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports[r.ID] = r
}

type fakeTickers struct{}

func (fakeTickers) Tickers(context.Context) ([]edgar.Company, error) {
	return []edgar.Company{
		{Ticker: "V", CIK: "0001403161", Name: "VISA INC."},
		{Ticker: "VZ", CIK: "0000732712", Name: "VERIZON COMMUNICATIONS INC"},
		{Ticker: "AAPL", CIK: "0000320193", Name: "Apple Inc."},
		{Ticker: "BRK-B", CIK: "0001067983", Name: "BERKSHIRE HATHAWAY INC"},
		{Ticker: "VSAT", CIK: "0000797721", Name: "VIASAT INC"},
	}, nil
}

func newTestAPI(t *testing.T) (*httptest.Server, *fakeReports, *fakeJobs, *pipeline.Broker) {
	t.Helper()
	reports := &fakeReports{reports: map[string]store.Report{}}
	jobs := &fakeJobs{}
	broker := pipeline.NewBroker()
	h := NewRouter(Deps{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Reports: reports,
		Jobs:    jobs,
		Events:  broker,
		Tickers: fakeTickers{},
		Ready:   func(context.Context) error { return nil },
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, reports, jobs, broker
}

// result is what a test needs from a response (the body is already read and closed).
type result struct {
	StatusCode int
	Header     http.Header
}

func do(t *testing.T, method, url, body string) (result, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return result{StatusCode: resp.StatusCode, Header: resp.Header}, b
}

// --- Tests ------------------------------------------------------------------------

// Table-driven tests are the Go equivalent of xUnit's [Theory] + [InlineData].
func TestRoutes(t *testing.T) {
	srv, _, _, _ := newTestAPI(t)
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"healthz", "GET", "/healthz", "", 200, `"ok"`},
		{"readyz", "GET", "/readyz", "", 200, `"ok"`},
		{"wrong method", "POST", "/healthz", "", 405, ""},
		{"unknown path", "GET", "/nope", "", 404, ""},
		{"ticker search needs q", "GET", "/api/tickers", "", 400, "q is required"},
		{"ticker search", "GET", "/api/tickers?q=v", "", 200, `"ticker":"V"`},
		{"search by name", "GET", "/api/tickers?q=berkshire", "", 200, `"BRK-B"`},
		{"create needs a ticker", "POST", "/api/reports", `{}`, 400, "ticker"},
		{"create rejects bad JSON", "POST", "/api/reports", `nope`, 400, ""},
		{"unknown ticker suggests", "POST", "/api/reports", `{"ticker":"VIS"}`, 404, `"suggestions"`},
		{"typo suggests the real ticker", "POST", "/api/reports", `{"ticker":"AAPLL"}`, 404, `"AAPL"`},
		{"unknown report", "GET", "/api/reports/22222222-2222-2222-2222-222222222222", "", 404, "not found"},
		{"list needs ticker", "GET", "/api/reports", "", 400, ""},
		{"list", "GET", "/api/reports?ticker=v", "", 200, `"ticker":"V"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := do(t, tc.method, srv.URL+tc.path, tc.body)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", resp.StatusCode, tc.wantStatus, body)
			}
			if tc.wantBody != "" && !strings.Contains(string(body), tc.wantBody) {
				t.Errorf("body %s does not contain %s", body, tc.wantBody)
			}
		})
	}
}

func TestSearchRanking(t *testing.T) {
	got := search(must(fakeTickers{}.Tickers(context.Background())), "v", 10)
	// Exact ticker first, then shorter prefixes, then name matches.
	want := []string{"V", "VZ", "VSAT"}
	for i, w := range want {
		if i >= len(got) || got[i].Ticker != w {
			t.Fatalf("search = %v, want prefix %v", got, want)
		}
	}
}

func TestCreateReportStartsJob(t *testing.T) {
	srv, _, jobs, _ := newTestAPI(t)

	resp, body := do(t, "POST", srv.URL+"/api/reports", `{"ticker":" v "}`)

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var cr createResponse
	_ = json.Unmarshal(body, &cr)
	if cr.ID == "" || cr.Cached || resp.Header.Get("Location") != "/api/reports/"+cr.ID {
		t.Errorf("response = %+v, location %q", cr, resp.Header.Get("Location"))
	}
	if started := jobs.list(); len(started) != 1 || started[0] != "V" {
		t.Errorf("jobs started = %v", started)
	}
}

func TestCreateReturnsRecentReport(t *testing.T) {
	srv, reports, jobs, _ := newTestAPI(t)
	reports.latest = &store.Report{ID: "prev", Status: store.StatusDone}

	resp, body := do(t, "POST", srv.URL+"/api/reports", `{"ticker":"V"}`)
	var cr createResponse
	_ = json.Unmarshal(body, &cr)
	if resp.StatusCode != 200 || !cr.Cached || cr.ID != "prev" || len(jobs.list()) != 0 {
		t.Errorf("status %d, response %+v, jobs %v", resp.StatusCode, cr, jobs.list())
	}

	// refresh forces a new job.
	resp, _ = do(t, "POST", srv.URL+"/api/reports", `{"ticker":"V","refresh":true}`)
	if resp.StatusCode != http.StatusAccepted || len(jobs.list()) != 1 {
		t.Errorf("refresh: status %d, jobs %v", resp.StatusCode, jobs.list())
	}
}

// readEvents reads SSE "event:" names until the stream ends.
func readEvents(t *testing.T, resp *http.Response) []string {
	t.Helper()
	var names []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if name, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
			names = append(names, name)
		}
	}
	return names
}

func TestEventsStreamLiveProgress(t *testing.T) {
	srv, reports, _, broker := newTestAPI(t)
	id := "33333333-3333-3333-3333-333333333333"
	reports.put(store.Report{ID: id, Status: store.StatusRunning, Stage: "ingesting"})

	req, _ := http.NewRequestWithContext(t.Context(), "GET", srv.URL+"/api/reports/"+id+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}

	// The handler has subscribed once headers are sent; publish the job's events.
	go func() {
		broker.Publish(id, pipeline.Event{Type: pipeline.SectionReady, Section: "financials"})
		broker.Publish(id, pipeline.Event{Type: pipeline.JobDone, Status: store.StatusDone})
	}()

	got := strings.Join(readEvents(t, resp), ",")
	if got != "snapshot,section.ready,job.done" {
		t.Errorf("events = %s", got)
	}
}

func TestEventsForFinishedReport(t *testing.T) {
	srv, reports, _, _ := newTestAPI(t)
	id := "44444444-4444-4444-4444-444444444444"
	reports.put(store.Report{ID: id, Status: store.StatusFailed, Error: "interrupted"})

	req, _ := http.NewRequestWithContext(t.Context(), "GET", srv.URL+"/api/reports/"+id+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := strings.Join(readEvents(t, resp), ","); got != "snapshot,job.failed" {
		t.Errorf("events = %s", got)
	}
}

func TestReadyReportsFailure(t *testing.T) {
	h := NewRouter(Deps{Ready: func(context.Context) error { return errors.New("db down") }})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d", rec.Code)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
