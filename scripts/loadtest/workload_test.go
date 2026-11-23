package main

import (
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// TestWorkloadIsReproducible: one seed gives one workload, so a run can be repeated and compared; another seed
// gives another.
func TestWorkloadIsReproducible(t *testing.T) {
	a, b, other := workload(1, 1000, 0.1, 0.05), workload(1, 1000, 0.1, 0.05), workload(2, 1000, 0.1, 0.05)
	if !slices.Equal(a, b) {
		t.Error("one seed gave two workloads")
	}
	if slices.Equal(a, other) {
		t.Error("two seeds gave one workload")
	}
}

// TestWorkloadMix: the channels, priorities and failing recipients come in about their shares.
func TestWorkloadMix(t *testing.T) {
	const n = 20_000
	count := map[string]int{}
	for _, item := range workload(7, n, 0.10, 0.05) {
		count[item.Channel]++
		count[item.Priority]++
		switch item.To {
		case unavailableRecipient:
			count["unavailable"]++
		case rejectedRecipient:
			count["rejected"]++
		}
	}
	for key, share := range map[string]float64{
		"sms": 0.5, "email": 0.3, "push": 0.2, "high": 0.2, "normal": 0.6, "low": 0.2,
		"unavailable": 0.10 * 0.5, "rejected": 0.05 * 0.5, // failing recipients are SMS numbers
	} {
		if got := float64(count[key]) / n; math.Abs(got-share) > 0.02 {
			t.Errorf("%s: %.3f of the workload, want about %.3f", key, got, share)
		}
	}
}

// TestWaitingNowReadsTheSummary: queued and sending are counted from GET /v1/metrics, member names as the API
// writes them.
func TestWaitingNowReadsTheSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"channels":{"sms":{"waiting":{"scheduled":4,"queued":980,"retrying":7,"sending":32}}}}`)
	}))
	defer srv.Close()
	got, err := waitingNow(t.Context(), srv.URL)
	if err != nil || got["sms"] != 1012 {
		t.Errorf("waitingNow = %v, %v; want sms 1012, queued and sending", got, err)
	}
}
