package pipeline

import (
	"strings"
	"testing"
)

func TestSegmentWindow(t *testing.T) {
	short := "Revenue by segment: Server 10."
	if got := segmentWindow(short, 100); got != short {
		t.Errorf("short text changed: %q", got)
	}

	intro := strings.Repeat("Liquidity and capital resources. ", 40)
	table := "Segment results: Server segment revenue 100; Networking segment revenue 50; Hybrid Cloud segment revenue 20."
	text := intro + table + strings.Repeat(" Critical accounting estimates.", 20)
	got := segmentWindow(text, len(table)+20)
	if !strings.Contains(got, table) {
		t.Errorf("window %q misses the segment table", got)
	}
	if len(got) > len(table)+20 {
		t.Errorf("window is %d bytes, want ≤ %d", len(got), len(table)+20)
	}
}

func TestEmptySegments(t *testing.T) {
	if !emptySegments([]byte(`{"unit":"USD millions","segments":[],"geography":[]}`)) {
		t.Error("an answer with no breakdown should count as empty")
	}
	if emptySegments([]byte(`{"segments":[{"name":"Server","revenue":17745}],"geography":[]}`)) {
		t.Error("an answer with segments is not empty")
	}
}
