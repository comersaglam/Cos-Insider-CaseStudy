package notification

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestMetricsSummary(t *testing.T) {
	summary := Summary{Window: 5 * time.Minute, Channels: map[Channel]ChannelSummary{
		ChannelSMS: {
			Waiting:  map[Status]int{StatusQueued: 980, StatusSending: 32},
			Attempts: map[AttemptOutcome]int{OutcomeSuccess: 1197, OutcomeTransient: 3},
			Latency:  &Latency{P50: 120 * time.Millisecond, P95: 340 * time.Millisecond, P99: 410 * time.Millisecond},
		},
		ChannelEmail: {Waiting: map[Status]int{}, Attempts: map[AttemptOutcome]int{}},
	}}
	rec := serve(t, &fakeService{summary: summary}, http.MethodGet, "/v1/metrics", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	got := body(t, rec)
	if got["windowSeconds"] != float64(300) || got["generatedAt"] == nil {
		t.Errorf("window %v, generatedAt %v", got["windowSeconds"], got["generatedAt"])
	}
	channels, _ := got["channels"].(map[string]any)
	sms, _ := channels["sms"].(map[string]any)
	for path, want := range map[string]string{
		"waiting":     "map[queued:980 retrying:0 scheduled:0 sending:32]",
		"attempts":    "map[permanent:0 permanentAlert:0 success:1197 transient:3 unknown:0]",
		"successRate": "0.9975",
		"latencyMs":   "map[p50:120 p95:340 p99:410]",
	} {
		if fmt.Sprint(sms[path]) != want {
			t.Errorf("sms.%s = %v, want %s", path, sms[path], want)
		}
	}
	email, _ := channels["email"].(map[string]any)
	if email["successRate"] != nil || email["latencyMs"] != nil {
		t.Errorf("email with no attempts = %v, want successRate and latencyMs null", email)
	}
}
