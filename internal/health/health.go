package health

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
)

// checkTimeout bounds each dependency's check, so a probe answers in time even when a dependency hangs.
const checkTimeout = 2 * time.Second

// codeNotReady is the problem code of a readiness probe that failed.
const codeNotReady = "not_ready"

// Check reports whether a dependency answers: nil when it does. It must return once ctx ends.
type Check func(ctx context.Context) error

// Handler serves liveness and readiness (ADR 0027).
type Handler struct {
	checks  map[string]Check
	timeout time.Duration
	log     *slog.Logger
}

// NewHandler returns probes over the named checks, the process's dependencies.
func NewHandler(checks map[string]Check, log *slog.Logger) *Handler {
	return &Handler{checks: checks, timeout: checkTimeout, log: log}
}

// RegisterRoutes mounts GET /healthz and GET /readyz on mux.
func (h *Handler) RegisterRoutes(mux httpx.Mux) {
	mux.Handle("GET /healthz", httpx.Handle(h.log, h.live))
	mux.Handle("GET /readyz", httpx.Handle(h.log, h.ready))
}

type liveResponse struct {
	Status string `json:"status"`
}

type readyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// live answers 200 while the process runs; it looks at no dependency, since restarting the process would not bring
// a dependency back.
func (h *Handler) live(w http.ResponseWriter, _ *http.Request) error {
	return httpx.WriteJSON(w, http.StatusOK, liveResponse{Status: "ok"})
}

// ready answers 200 when every check passes, and a 503 problem naming the ones that failed otherwise. Why a check
// failed is logged, not answered: the error can hold a host or a user name.
func (h *Handler) ready(w http.ResponseWriter, r *http.Request) error {
	failed := h.failing(r.Context())
	if len(failed) > 0 {
		return httpx.NewProblem(http.StatusServiceUnavailable, codeNotReady, strings.Join(failed, ", ")+" did not answer")
	}
	resp := readyResponse{Status: "ready", Checks: make(map[string]string, len(h.checks))}
	for name := range h.checks {
		resp.Checks[name] = "ok"
	}
	return httpx.WriteJSON(w, http.StatusOK, resp)
}

// failing runs every check at once, each within the timeout, and returns the names of those that failed, sorted.
func (h *Handler) failing(ctx context.Context) []string {
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		failed []string
	)
	for name, check := range h.checks {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, h.timeout)
			defer cancel()
			if err := check(ctx); err != nil {
				h.log.WarnContext(ctx, "readiness check failed", "check", name, "error", err)
				mu.Lock()
				failed = append(failed, name)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	slices.Sort(failed)
	return failed
}
