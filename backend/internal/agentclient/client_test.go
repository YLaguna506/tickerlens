package agentclient

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAnalyze(t *testing.T) {
	var gotTask Task
	var gotJobID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze/business_overview" {
			http.NotFound(w, r)
			return
		}
		gotJobID = r.Header.Get(JobIDHeader)
		_ = json.NewDecoder(r.Body).Decode(&gotTask)
		_, _ = w.Write([]byte(`{"section":"business_overview","status":"ok","promptVersion":"v1",
			"output":{"whatItDoes":"x"},"evidence":[{"claim":"c","documentId":"10k-item1","quote":"q"}],
			"usage":{"model":"m","inputTokens":10,"outputTokens":5,"costUsd":0.001,"costKnown":true}}`))
	}))
	t.Cleanup(srv.Close)

	res, err := New(srv.URL).Analyze(WithJobID(t.Context(), "job-1"), Task{Section: "business_overview", Ticker: "V"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() || len(res.Evidence) != 1 || res.Usage.InputTokens != 10 || string(res.Output) != `{"whatItDoes":"x"}` {
		t.Errorf("result = %+v", res)
	}
	if gotTask.Ticker != "V" || gotJobID != "job-1" {
		t.Errorf("server got %+v, job id %q", gotTask, gotJobID)
	}
}

func TestAnalyzeRetriesServerErrorsOnce(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "starting up", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"section":"segments","status":"failed","errors":["bad"]}`))
	}))
	t.Cleanup(srv.Close)

	res, err := New(srv.URL).Analyze(t.Context(), Task{Section: "segments"})
	if err != nil || res.OK() || calls.Load() != 2 {
		t.Errorf("res=%+v err=%v calls=%d", res, err, calls.Load())
	}
}

func TestAnalyzeDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, `{"detail":"unknown section"}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, err := New(srv.URL).Analyze(t.Context(), Task{Section: "nope"})
	if !errors.Is(err, errClient) || calls.Load() != 1 {
		t.Errorf("err=%v calls=%d", err, calls.Load())
	}
}

func TestSections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-Type") != "text/html" || string(body) != "<p>10-K</p>" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"items":{"1":"Business text","7":"MD&A text"}}`))
	}))
	t.Cleanup(srv.Close)

	items, err := New(srv.URL).Sections(t.Context(), []byte("<p>10-K</p>"))
	if err != nil || items["1"] != "Business text" {
		t.Errorf("items=%v err=%v", items, err)
	}
}
