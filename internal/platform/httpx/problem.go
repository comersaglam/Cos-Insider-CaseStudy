package httpx

import (
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// codeInternal is the code of every answer to an unexpected error.
const codeInternal = "internal_error"

// Problem is an error answer in the format of RFC 9457 (application/problem+json), extended with a stable code
// that clients can switch on, the request's correlation ID, and a list of field errors.
//
// A *Problem is also an error, so a handler can return one, wrapped or not, and Handle writes it.
type Problem struct {
	Type          string         `json:"type"`
	Title         string         `json:"title"`
	Status        int            `json:"status"`
	Detail        string         `json:"detail,omitempty"`
	Code          string         `json:"code"`
	CorrelationID string         `json:"correlationId,omitempty"`
	Errors        []FieldProblem `json:"errors,omitempty"`
}

// FieldProblem is one part of the request that broke a rule. A member of the body is located by Pointer, a JSON
// Pointer (RFC 6901) such as "/to" or "/notifications/3/content", as RFC 9457's own example does; a query
// parameter, which a pointer cannot reach, by Parameter, its name.
type FieldProblem struct {
	Pointer   string `json:"pointer,omitzero"`
	Parameter string `json:"parameter,omitzero"`
	Code      string `json:"code"`
	Detail    string `json:"detail"`
}

// NewProblem returns a problem with the given HTTP status, stable code and human-readable detail.
func NewProblem(status int, code, detail string) *Problem {
	return &Problem{Status: status, Code: code, Detail: detail}
}

// Error returns the problem's code and detail.
func (p *Problem) Error() string {
	return p.Code + ": " + p.Detail
}

// WriteProblem writes p as the response. The type defaults to "about:blank" and the title to the status's text,
// as RFC 9457 specifies for problems without a type of their own; the correlation ID comes from the request.
func WriteProblem(w http.ResponseWriter, r *http.Request, p *Problem) {
	out := *p
	if out.Type == "" {
		out.Type = "about:blank"
	}
	if out.Title == "" {
		out.Title = http.StatusText(out.Status)
	}
	out.CorrelationID = observability.CorrelationID(r.Context())

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(out.Status)
	// The status is already sent, so an encoding failure (a client gone away) cannot be answered any more.
	_ = json.MarshalWrite(w, out)
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.MarshalWrite(w, v)
}

// HandlerFunc is an HTTP handler that returns its error instead of writing it.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Handle adapts fn to an http.Handler, so every error is answered in one way. A *Problem in the returned error's
// chain is written as it is. Any other error is logged and answered with a 500 problem that reveals nothing of
// it, since its text can hold internal details. An error returned after the response has started is only logged.
func Handle(log *slog.Logger, fn HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &recorder{ResponseWriter: w}
		err := fn(rec, r)
		if err == nil {
			return
		}

		p, ok := errors.AsType[*Problem](err)
		if !ok {
			log.ErrorContext(r.Context(), "request failed", "error", err)
			p = NewProblem(http.StatusInternalServerError, codeInternal, "the request could not be completed")
		}
		if rec.status != 0 {
			log.ErrorContext(r.Context(), "request failed after the response started", "error", err)
			return
		}
		WriteProblem(rec, r, p)
	})
}
