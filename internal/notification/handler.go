package notification

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
)

// maxCreateBody bounds a create request. The largest valid one, an e-mail of 100,000 characters, is about 600 KB
// even if every character is written as a JSON escape.
const maxCreateBody = 1 << 20

// maxIdempotencyKey is the longest idempotency key accepted, as the database's CHECK allows.
const maxIdempotencyKey = 255

// service is what the handlers need from the use cases; *Service satisfies it.
type service interface {
	Create(ctx context.Context, d Draft, idempotencyKey string) (Notification, bool, error)
	Get(ctx context.Context, id uuid.UUID) (Notification, error)
}

// Handler serves the notification endpoints. It reads requests and writes answers; the rules are in Service.
type Handler struct {
	svc service
	log *slog.Logger
}

// NewHandler returns the handlers over svc; log receives the errors they cannot answer in detail.
func NewHandler(svc service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// RegisterRoutes mounts the module's endpoints on mux (ADR 0008).
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("POST /v1/notifications", httpx.Handle(h.log, h.create))
	mux.Handle("GET /v1/notifications/{id}", httpx.Handle(h.log, h.get))
}

// createRequest is the body of POST /v1/notifications. The member names are the provider contract's.
type createRequest struct {
	To       string `json:"to"`
	Channel  string `json:"channel"`
	Content  string `json:"content"`
	Priority string `json:"priority"`
}

// notificationResponse is a notification as the API shows it.
type notificationResponse struct {
	ID            uuid.UUID `json:"id"`
	To            string    `json:"to"`
	Channel       Channel   `json:"channel"`
	Content       string    `json:"content"`
	Priority      Priority  `json:"priority"`
	Status        Status    `json:"status"`
	CorrelationID string    `json:"correlationId"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// toResponse converts n field by field; struct tags do not take part in a conversion. A field added to
// Notification stops this from compiling until notificationResponse decides whether the API shows it.
func toResponse(n Notification) notificationResponse {
	return notificationResponse(n)
}

// create accepts a notification for sending: 202 with the notification and its Location, since it is accepted,
// not yet sent. A repeated request gets the same answer, marked Idempotent-Replayed.
func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	key := r.Header.Get("Idempotency-Key")
	if key != "" && (len(key) > maxIdempotencyKey || !isVisibleASCII(key)) {
		return httpx.NewProblem(http.StatusBadRequest, "invalid_idempotency_key",
			"the Idempotency-Key header must be 1 to 255 visible ASCII characters")
	}
	var req createRequest
	if err := httpx.DecodeJSON(w, r, maxCreateBody, &req); err != nil {
		return err
	}

	d := Draft{To: req.To, Channel: Channel(req.Channel), Content: req.Content, Priority: Priority(req.Priority)}
	if d.Priority == "" {
		d.Priority = PriorityNormal
	}
	n, created, err := h.svc.Create(r.Context(), d, key)
	if err != nil {
		return toProblem(err)
	}

	w.Header().Set("Location", "/v1/notifications/"+n.ID.String())
	if !created {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	return httpx.WriteJSON(w, http.StatusAccepted, toResponse(n))
}

// get answers a notification by its ID. An ID that is not a UUID cannot name a notification, so it is not found
// either.
func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return toProblem(ErrNotFound)
	}
	n, err := h.svc.Get(r.Context(), id)
	if err != nil {
		return toProblem(err)
	}
	return httpx.WriteJSON(w, http.StatusOK, toResponse(n))
}

// toProblem turns the module's errors into their HTTP answers; any other error stays as it is, for httpx.Handle to
// log and answer with 500.
func toProblem(err error) error {
	if verr, ok := errors.AsType[*ValidationError](err); ok {
		p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the notification is invalid")
		for _, f := range verr.Fields {
			p.Errors = append(p.Errors, httpx.FieldProblem{Pointer: "/" + f.Field, Code: string(f.Code), Detail: f.Message})
		}
		return p
	}
	switch {
	case errors.Is(err, ErrIdempotencyKeyReused):
		return httpx.NewProblem(http.StatusUnprocessableEntity, "idempotency_key_reused",
			"this Idempotency-Key was used for a different request")
	case errors.Is(err, ErrNotFound):
		return httpx.NewProblem(http.StatusNotFound, "not_found", "no notification has this ID")
	}
	return err
}
