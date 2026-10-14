// Package fetch is the HTTP client every data provider uses. It adds, in order:
// a response cache, a per-provider rate limiter, and retries with exponential
// backoff that respect 429 / Retry-After.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// Cache stores raw response bodies by key.
//
// C# note: Go interfaces are satisfied implicitly (no ": ICache"), and the
// convention is to declare them where they are used, not where implemented.
// store.PGCache and MemoryCache both satisfy this without mentioning it.
type Cache interface {
	Get(ctx context.Context, key string) (body []byte, ok bool, err error)
	Set(ctx context.Context, key string, body []byte, ttl time.Duration) error
}

// Options configures a Client. Zero values get sensible defaults.
type Options struct {
	// Name identifies the provider in logs (e.g. "sec").
	Name string
	// Limiter throttles outgoing requests. nil means unlimited.
	Limiter *rate.Limiter
	// Cache is optional; nil disables caching.
	Cache Cache
	// Header is added to every request (User-Agent, Authorization, ...).
	Header http.Header
	// BeforeNetwork runs before every real HTTP attempt (not on cache hits).
	// Alpha Vantage uses it to enforce its daily request budget.
	BeforeNetwork func(ctx context.Context) error

	HTTPClient  *http.Client
	Logger      *slog.Logger
	MaxAttempts int           // default 5
	BaseDelay   time.Duration // default 500ms; doubles each retry
	MaxDelay    time.Duration // default 30s; also the longest Retry-After we will wait
}

// Client performs cached, rate-limited GET requests with retries.
type Client struct {
	opts  Options
	sleep func(ctx context.Context, d time.Duration) error // swapped in tests
}

// New builds a Client.
//
// C# note: Go has no constructors; a New... function that returns a
// configured value is the convention.
func New(opts Options) *Client {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}
	if opts.BaseDelay <= 0 {
		opts.BaseDelay = 500 * time.Millisecond
	}
	if opts.MaxDelay <= 0 {
		opts.MaxDelay = 30 * time.Second
	}
	return &Client{opts: opts, sleep: sleepCtx}
}

// Request describes one GET.
type Request struct {
	URL string
	// CacheKey enables caching when non-empty. It must not contain secrets,
	// because it is stored in the database.
	CacheKey string
	TTL      time.Duration
	// Validate, if set, checks a 200 response body before it is cached.
	// Some APIs (Alpha Vantage) report errors inside a 200 response.
	Validate func(body []byte) error
}

// StatusError is returned when the server answers with a non-2xx status that
// is not retried, or when retries run out.
type StatusError struct {
	Provider   string
	StatusCode int
	URL        string // with secrets redacted
	Body       string // first part of the response body
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s: GET %s: HTTP %d: %s", e.Provider, e.URL, e.StatusCode, e.Body)
}

// Get returns the response body for req, from the cache when possible.
func (c *Client) Get(ctx context.Context, req Request) ([]byte, error) {
	if body, ok := c.cacheGet(ctx, req.CacheKey); ok {
		return body, nil
	}

	var lastErr error
	for attempt := range c.opts.MaxAttempts {
		if attempt > 0 {
			delay := c.backoff(attempt, lastErr)
			c.opts.Logger.Warn("retrying request", "provider", c.opts.Name, "url", redact(req.URL),
				"attempt", attempt+1, "delay", delay.String(), "err", lastErr)
			if err := c.sleep(ctx, delay); err != nil {
				return nil, err
			}
		}

		body, err := c.do(ctx, req.URL)
		if err == nil {
			if req.Validate != nil {
				if err := req.Validate(body); err != nil {
					return nil, err
				}
			}
			c.cacheSet(ctx, req.CacheKey, body, req.TTL)
			return body, nil
		}
		if !retryable(ctx, err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%s: giving up after %d attempts: %w", c.opts.Name, c.opts.MaxAttempts, lastErr)
}

// retryAfterError carries the server's Retry-After hint alongside the status.
type retryAfterError struct {
	*StatusError
	after time.Duration
}

// Unwrap lets errors.As find the StatusError inside.
func (e *retryAfterError) Unwrap() error { return e.StatusError }

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, error) {
	if c.opts.Limiter != nil {
		if err := c.opts.Limiter.Wait(ctx); err != nil {
			return nil, err
		}
	}
	if c.opts.BeforeNetwork != nil {
		if err := c.opts.BeforeNetwork(ctx); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, vs := range c.opts.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.opts.HTTPClient.Do(req)
	if err != nil {
		// net/http errors include the URL, which may contain an API key.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			urlErr.URL = redact(urlErr.URL)
		}
		return nil, err
	}
	// defer runs when the function returns, like a finally block or `using`.
	// A close error after a full read is harmless, so it is ignored explicitly.
	defer func() { _ = resp.Body.Close() }()

	// Cap the size so a misbehaving server cannot exhaust memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("%s: read body: %w", c.opts.Name, err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return body, nil
	}

	statusErr := &StatusError{
		Provider:   c.opts.Name,
		StatusCode: resp.StatusCode,
		URL:        redact(rawURL),
		Body:       truncate(strings.TrimSpace(string(body)), 300),
	}
	if after, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
		return nil, &retryAfterError{StatusError: statusErr, after: after}
	}
	return nil, statusErr
}

// retryable reports whether a failed attempt is worth repeating.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false // the caller cancelled or timed out
	}
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		switch statusErr.StatusCode {
		case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	if errors.Is(err, ErrBudgetExhausted) {
		return false
	}
	// Anything else is a network-level failure (DNS, reset, timeout).
	return true
}

// backoff picks the wait before the next attempt: the server's Retry-After if
// it gave one, otherwise exponential backoff with full jitter.
func (c *Client) backoff(attempt int, lastErr error) time.Duration {
	var ra *retryAfterError
	if errors.As(lastErr, &ra) {
		return min(ra.after, c.opts.MaxDelay)
	}
	ceiling := min(c.opts.BaseDelay<<(attempt-1), c.opts.MaxDelay)
	return time.Duration(rand.Int64N(int64(ceiling) + 1))
}

// ErrBudgetExhausted is returned by BeforeNetwork hooks when a provider's
// request budget (e.g. Alpha Vantage's daily limit) is used up.
var ErrBudgetExhausted = errors.New("request budget exhausted")

func (c *Client) cacheGet(ctx context.Context, key string) ([]byte, bool) {
	if c.opts.Cache == nil || key == "" {
		return nil, false
	}
	body, ok, err := c.opts.Cache.Get(ctx, key)
	if err != nil {
		// The cache is an optimisation: log and fall through to the network.
		c.opts.Logger.Warn("cache read failed", "provider", c.opts.Name, "key", key, "err", err)
		return nil, false
	}
	return body, ok
}

func (c *Client) cacheSet(ctx context.Context, key string, body []byte, ttl time.Duration) {
	if c.opts.Cache == nil || key == "" || ttl <= 0 {
		return
	}
	if err := c.opts.Cache.Set(ctx, key, body, ttl); err != nil {
		c.opts.Logger.Warn("cache write failed", "provider", c.opts.Name, "key", key, "err", err)
	}
}

// parseRetryAfter handles both forms of the header: seconds ("120") and an
// HTTP date.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0), true
	}
	return 0, false
}

// redact hides query parameters that carry credentials.
func redact(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "<unparseable url>"
	}
	q := u.Query()
	for _, k := range []string{"apikey", "token", "api_key"} {
		if q.Has(k) {
			q.Set(k, "REDACTED")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	// select waits on whichever channel is ready first: the timer firing or
	// the context being cancelled.
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
