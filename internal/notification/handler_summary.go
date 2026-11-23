package notification

import (
	"math"
	"net/http"
	"time"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
)

// summaryResponse is GET /v1/metrics: the service's state as a person reads it, by channel (ADR 0026). Prometheus
// reads /metrics instead.
type summaryResponse struct {
	GeneratedAt   time.Time                          `json:"generatedAt"`
	WindowSeconds int                                `json:"windowSeconds"`
	Channels      map[Channel]channelSummaryResponse `json:"channels"`
}

type channelSummaryResponse struct {
	Waiting  waitingResponse  `json:"waiting"`
	Attempts attemptsResponse `json:"attempts"`
	// SuccessRate is the window's successful attempts over all its attempts; null when there were none.
	SuccessRate *float64 `json:"successRate"`
	// LatencyMs is the provider's latency in milliseconds; null when no attempt was answered.
	LatencyMs *latencyResponse `json:"latencyMs"`
}

type waitingResponse struct {
	Scheduled int `json:"scheduled"`
	Queued    int `json:"queued"`
	Retrying  int `json:"retrying"`
	Sending   int `json:"sending"`
}

type attemptsResponse struct {
	Success        int `json:"success"`
	Permanent      int `json:"permanent"`
	PermanentAlert int `json:"permanentAlert"`
	Transient      int `json:"transient"`
	Unknown        int `json:"unknown"`
}

type latencyResponse struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

// summary answers GET /v1/metrics.
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) error {
	s, err := h.svc.Summary(r.Context())
	if err != nil {
		return toProblem(err)
	}
	resp := summaryResponse{
		GeneratedAt: time.Now().UTC(), WindowSeconds: int(s.Window.Seconds()),
		Channels: make(map[Channel]channelSummaryResponse, len(s.Channels)),
	}
	for c, cs := range s.Channels {
		out := channelSummaryResponse{
			Waiting: waitingResponse{
				Scheduled: cs.Waiting[StatusScheduled], Queued: cs.Waiting[StatusQueued],
				Retrying: cs.Waiting[StatusRetrying], Sending: cs.Waiting[StatusSending],
			},
			Attempts: attemptsResponse{
				Success: cs.Attempts[OutcomeSuccess], Permanent: cs.Attempts[OutcomePermanent],
				PermanentAlert: cs.Attempts[OutcomePermanentAlert], Transient: cs.Attempts[OutcomeTransient],
				Unknown: cs.Attempts[OutcomeUnknown],
			},
		}
		total := 0
		for _, n := range cs.Attempts {
			total += n
		}
		if total > 0 {
			rate := math.Round(float64(cs.Attempts[OutcomeSuccess])/float64(total)*10_000) / 10_000
			out.SuccessRate = &rate
		}
		if cs.Latency != nil {
			out.LatencyMs = &latencyResponse{P50: millis(cs.Latency.P50), P95: millis(cs.Latency.P95), P99: millis(cs.Latency.P99)}
		}
		resp.Channels[c] = out
	}
	return httpx.WriteJSON(w, http.StatusOK, resp)
}

// millis returns d in milliseconds, to a tenth.
func millis(d time.Duration) float64 {
	return math.Round(float64(d)/float64(time.Millisecond)*10) / 10
}
