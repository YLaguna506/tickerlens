package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient returns a Client whose sleeps are recorded instead of waited.
func newTestClient(opts Options) (*Client, *[]time.Duration) {
	c := New(opts)
	var slept []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	return c, &slept
}

// statusSequence serves the given statuses in order, then 200 "ok".
func statusSequence(t *testing.T, statuses []int, header http.Header) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(calls.Add(1))
		if n <= len(statuses) {
			for k, v := range header {
				w.Header()[k] = v
			}
			w.WriteHeader(statuses[n-1])
			_, _ = w.Write([]byte("nope"))
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestGetRetries(t *testing.T) {
	tests := []struct {
		name       string
		statuses   []int
		header     http.Header
		wantBody   string
		wantStatus int // expected StatusError code when the call fails
		wantCalls  int32
		wantSleeps []time.Duration // nil = don't check exact values
	}{
		{name: "success first try", wantBody: "ok", wantCalls: 1},
		{name: "retries 503 then succeeds", statuses: []int{503, 503}, wantBody: "ok", wantCalls: 3},
		{
			name:       "honours Retry-After seconds",
			statuses:   []int{429},
			header:     http.Header{"Retry-After": {"7"}},
			wantBody:   "ok",
			wantCalls:  2,
			wantSleeps: []time.Duration{7 * time.Second},
		},
		{
			name:       "caps a long Retry-After at MaxDelay",
			statuses:   []int{429},
			header:     http.Header{"Retry-After": {"3600"}},
			wantBody:   "ok",
			wantCalls:  2,
			wantSleeps: []time.Duration{30 * time.Second},
		},
		{name: "does not retry 404", statuses: []int{404}, wantStatus: 404, wantCalls: 1},
		{name: "does not retry 403", statuses: []int{403}, wantStatus: 403, wantCalls: 1},
		{name: "gives up after max attempts", statuses: []int{500, 500, 500, 500, 500}, wantStatus: 500, wantCalls: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, calls := statusSequence(t, tc.statuses, tc.header)
			c, slept := newTestClient(Options{Name: "test"})

			body, err := c.Get(t.Context(), Request{URL: srv.URL})

			if got := calls.Load(); got != tc.wantCalls {
				t.Errorf("calls = %d, want %d", got, tc.wantCalls)
			}
			if tc.wantStatus != 0 {
				var se *StatusError
				if !errors.As(err, &se) || se.StatusCode != tc.wantStatus {
					t.Fatalf("err = %v, want StatusError %d", err, tc.wantStatus)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(body) != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
			if tc.wantSleeps != nil {
				if len(*slept) != len(tc.wantSleeps) || (*slept)[0] != tc.wantSleeps[0] {
					t.Errorf("slept = %v, want %v", *slept, tc.wantSleeps)
				}
			}
		})
	}
}

func TestGetUsesCache(t *testing.T) {
	srv, calls := statusSequence(t, nil, nil)
	c, _ := newTestClient(Options{Name: "test", Cache: NewMemoryCache()})
	req := Request{URL: srv.URL, CacheKey: "k", TTL: time.Hour}

	for range 3 {
		if _, err := c.Get(t.Context(), req); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls = %d, want 1 (later calls should hit the cache)", got)
	}
}

func TestGetDoesNotCacheInvalidBody(t *testing.T) {
	srv, calls := statusSequence(t, nil, nil)
	cache := NewMemoryCache()
	c, _ := newTestClient(Options{Name: "test", Cache: cache})
	errBad := errors.New("bad body")
	req := Request{URL: srv.URL, CacheKey: "k", TTL: time.Hour, Validate: func([]byte) error { return errBad }}

	if _, err := c.Get(t.Context(), req); !errors.Is(err, errBad) {
		t.Fatalf("err = %v, want %v", err, errBad)
	}
	if _, ok, _ := cache.Get(t.Context(), "k"); ok {
		t.Error("invalid body was cached")
	}
	if calls.Load() != 1 {
		t.Errorf("validation errors must not be retried; calls = %d", calls.Load())
	}
}

func TestBudgetStopsRequests(t *testing.T) {
	srv, calls := statusSequence(t, nil, nil)
	c, _ := newTestClient(Options{
		Name:          "test",
		BeforeNetwork: func(context.Context) error { return ErrBudgetExhausted },
	})

	_, err := c.Get(t.Context(), Request{URL: srv.URL})

	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("err = %v, want ErrBudgetExhausted", err)
	}
	if calls.Load() != 0 {
		t.Errorf("server was called %d times", calls.Load())
	}
}

func TestStatusErrorRedactsSecrets(t *testing.T) {
	srv, _ := statusSequence(t, []int{400}, nil)
	c, _ := newTestClient(Options{Name: "test"})

	_, err := c.Get(t.Context(), Request{URL: srv.URL + "/query?symbol=V&apikey=SECRET123"})

	if err == nil || strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("error leaks the API key: %v", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in     string
		want   time.Duration
		wantOK bool
	}{
		{"", 0, false},
		{"5", 5 * time.Second, true},
		{"Wed, 14 Oct 2026 12:01:00 GMT", time.Minute, true},
		{"Wed, 14 Oct 2026 11:00:00 GMT", 0, true}, // in the past
		{"soon", 0, false},
	}
	for _, tc := range tests {
		got, ok := parseRetryAfter(tc.in, now)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}
