// Package agentclient calls the Python agents service over HTTP.
package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Document is filing text given to an agent.
type Document struct {
	ID     string `json:"id"`     // e.g. "10k-item1"
	Source string `json:"source"` // e.g. "10-K FY2025 Item 1"
	URL    string `json:"url,omitempty"`
	Text   string `json:"text"`
}

// Task is the request body of POST /analyze/{section}.
type Task struct {
	JobID         string         `json:"jobId"`
	Section       string         `json:"section"`
	Ticker        string         `json:"ticker"`
	CompanyName   string         `json:"companyName"`
	PromptVersion string         `json:"promptVersion"`
	Metrics       map[string]any `json:"metrics"`
	Documents     []Document     `json:"documents"`
}

// Evidence backs a judgment with a verbatim filing quote.
type Evidence struct {
	Claim      string `json:"claim"`
	DocumentID string `json:"documentId"`
	Quote      string `json:"quote"`
}

// Usage is the token and cost accounting of one agent call.
type Usage struct {
	Model        string  `json:"model"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	CostUSD      float64 `json:"costUsd"`
	CostKnown    bool    `json:"costKnown"`
}

// Result is the response of POST /analyze/{section}.
type Result struct {
	JobID         string          `json:"jobId"`
	Section       string          `json:"section"`
	Status        string          `json:"status"` // ok | failed
	PromptVersion string          `json:"promptVersion"`
	Output        json.RawMessage `json:"output,omitempty"`
	Evidence      []Evidence      `json:"evidence"`
	Usage         Usage           `json:"usage"`
	Attempts      int             `json:"attempts"`
	Errors        []string        `json:"errors"`
}

// OK reports whether the agent produced a validated answer.
func (r Result) OK() bool { return r.Status == "ok" }

// Client talks to the agents service.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client for the service at baseURL (e.g. http://agents:8000).
// Agent calls run an LLM and can take a minute or more.
func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 4 * time.Minute}}
}

// Sections sends a filing's HTML and returns its text split by Item
// ("1", "1A", "7", ...).
func (c *Client) Sections(ctx context.Context, html []byte) (map[string]string, error) {
	var out struct {
		Items map[string]string `json:"items"`
	}
	if err := c.post(ctx, "/filings/sections", "text/html", html, &out); err != nil {
		return nil, fmt.Errorf("filing sections: %w", err)
	}
	return out.Items, nil
}

// Analyze runs one agent. A validation failure inside the agent is not an
// error: it comes back as a Result with Status "failed" and Errors.
func (c *Client) Analyze(ctx context.Context, task Task) (Result, error) {
	body, err := json.Marshal(task)
	if err != nil {
		return Result{}, err
	}
	var out Result
	// One retry covers a restarting agents container; the agent itself
	// already retries the LLM provider.
	for attempt := range 2 {
		err = c.post(ctx, "/analyze/"+task.Section, "application/json", body, &out)
		if err == nil || ctx.Err() != nil || errors.Is(err, errClient) {
			break
		}
		if attempt == 0 {
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return Result{}, ctx.Err()
			}
		}
	}
	if err != nil {
		return Result{}, fmt.Errorf("agent %s: %w", task.Section, err)
	}
	return out, nil
}

// errClient marks 4xx responses, which a retry would not fix.
var errClient = errors.New("rejected by agents service")

func (c *Client) post(ctx context.Context, path, contentType string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return fmt.Errorf("%w: HTTP %d: %.300s", errClient, resp.StatusCode, data)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("HTTP %d: %.300s", resp.StatusCode, data)
	}
	return json.Unmarshal(data, out)
}
