package observability

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"log/slog"
	"strings"
	"testing"
)

// lines decodes each JSON line the logger wrote.
func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, l)
		}
		out = append(out, m)
	}
	return out
}

func TestLoggerAddsTheContextsCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, slog.LevelInfo)

	log.InfoContext(WithCorrelationID(context.Background(), "checkout-42"), "with id")
	log.InfoContext(context.Background(), "without id")
	log.With("module", "notification").InfoContext(WithCorrelationID(context.Background(), "abc"), "derived")
	log.WithGroup("send").InfoContext(WithCorrelationID(context.Background(), "grp"), "grouped")

	got := lines(t, &buf)
	if len(got) != 4 {
		t.Fatalf("wrote %d lines, want 4", len(got))
	}
	if got[0]["correlation_id"] != "checkout-42" {
		t.Errorf("line 1 correlation_id = %v, want checkout-42", got[0]["correlation_id"])
	}
	if _, ok := got[1]["correlation_id"]; ok {
		t.Errorf("line 2 has a correlation_id although its context has none: %v", got[1])
	}
	if got[2]["correlation_id"] != "abc" || got[2]["module"] != "notification" {
		t.Errorf("a logger derived with With lost the correlation ID or its attribute: %v", got[2])
	}
	// Inside a group the ID is nested under the group's name, as every attribute added after WithGroup is.
	if group, _ := got[3]["send"].(map[string]any); group["correlation_id"] != "grp" {
		t.Errorf("a logger derived with WithGroup lost the correlation ID: %v", got[3])
	}
}

func TestLoggerHonoursItsLevel(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, slog.LevelWarn)
	log.Info("dropped")
	log.Warn("kept")

	got := lines(t, &buf)
	if len(got) != 1 || got[0]["msg"] != "kept" {
		t.Errorf("lines = %v, want only the warning", got)
	}
}

func TestLoggerWritesTimesInUTC(t *testing.T) {
	var buf bytes.Buffer
	NewLogger(&buf, slog.LevelInfo).Info("tick")

	ts, _ := lines(t, &buf)[0]["time"].(string)
	if !strings.HasSuffix(ts, "Z") {
		t.Errorf("time = %q, want UTC ending in Z", ts)
	}
}

func TestCorrelationIDOfAContextWithoutOne(t *testing.T) {
	if id := CorrelationID(context.Background()); id != "" {
		t.Errorf("CorrelationID = %q, want empty", id)
	}
}

func TestNewCorrelationIDIsValidAndUnique(t *testing.T) {
	a, b := NewCorrelationID(), NewCorrelationID()
	if !ValidCorrelationID(a) || a == b {
		t.Errorf("NewCorrelationID gave %q and %q, want two different valid IDs", a, b)
	}
}

func TestValidCorrelationID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"checkout-42", true},
		{"0190a6f2-7c1e-7b3a-9d4e-2f1a3b4c5d6e", true},
		{strings.Repeat("a", 128), true},
		{strings.Repeat("a", 129), false},
		{"", false},
		{"has space", false},
		{"line\nbreak", false}, // would forge a second log line in a plain-text log
		{"ünicode", false},
	}
	for _, tt := range tests {
		if got := ValidCorrelationID(tt.id); got != tt.want {
			t.Errorf("ValidCorrelationID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}
