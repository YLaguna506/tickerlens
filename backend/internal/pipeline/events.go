// Package pipeline runs report jobs: one goroutine per report, moving through
// the stages and publishing progress events for live updates.
package pipeline

import (
	"sync"
	"time"
)

// Event types sent to subscribers (and on to browsers over SSE).
const (
	StageStarted   = "stage.started"
	StageCompleted = "stage.completed"
	SectionReady   = "section.ready"
	SectionFailed  = "section.failed"
	JobDone        = "job.done"
	JobFailed      = "job.failed"
)

// Event is one progress update for a report.
type Event struct {
	Type    string    `json:"type"`
	Stage   string    `json:"stage,omitempty"`
	Section string    `json:"section,omitempty"`
	Status  string    `json:"status,omitempty"` // final status, on job.done / job.failed
	Message string    `json:"message,omitempty"`
	Time    time.Time `json:"time"`
}

// Terminal reports whether no more events follow this one.
func (e Event) Terminal() bool { return e.Type == JobDone || e.Type == JobFailed }

// Broker fans out events from running jobs to any number of listeners.
// It lives in memory: listeners that connect after a job ends read its final
// state from the database instead.
//
// C# note: this is the classic observer pattern built from channels instead of
// events/delegates. A mutex guards the map because publishers (job
// goroutines) and subscribers (HTTP requests) run concurrently.
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

// NewBroker returns an empty Broker.
func NewBroker() *Broker {
	return &Broker{subs: map[string]map[chan Event]struct{}{}}
}

// Subscribe returns a channel of events for a report, and a function that
// must be called to stop listening.
func (b *Broker) Subscribe(reportID string) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	b.mu.Lock()
	if b.subs[reportID] == nil {
		b.subs[reportID] = map[chan Event]struct{}{}
	}
	b.subs[reportID][ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs[reportID], ch)
			if len(b.subs[reportID]) == 0 {
				delete(b.subs, reportID)
			}
			b.mu.Unlock()
		})
	}
}

// Publish sends an event to every listener of a report. It never blocks: a
// listener whose buffer is full misses the event (a job sends ~10 events, so
// this only happens to a stuck client, which can reload the report).
func (b *Broker) Publish(reportID string, e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[reportID] {
		select {
		case ch <- e:
		default:
		}
	}
}
