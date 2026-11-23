package notification

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
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
	CreateBatch(ctx context.Context, drafts []Draft, idempotencyKey string) (Batch, bool, error)
	GetBatch(ctx context.Context, id uuid.UUID) (Batch, error)
	List(ctx context.Context, f ListFilter, after *Cursor, limit int) (Page, error)
	Cancel(ctx context.Context, id uuid.UUID) (Notification, error)
	Retry(ctx context.Context, id uuid.UUID) (Notification, error)
	RetryFailed(ctx context.Context, f RetryFilter) (RetryResult, error)
}

// Handler serves the notification and batch endpoints. It reads requests and writes answers; the rules are in
// Service.
type Handler struct {
	svc          service
	log          *slog.Logger
	maxBatchBody int64
}

// NewHandler returns the handlers over svc. log receives the errors they cannot answer in detail; maxBatchBody
// bounds the body of a batch request, in bytes.
func NewHandler(svc service, log *slog.Logger, maxBatchBody int64) *Handler {
	return &Handler{svc: svc, log: log, maxBatchBody: maxBatchBody}
}

// RegisterRoutes mounts the module's endpoints on mux (ADR 0008).
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("POST /v1/notifications", httpx.Handle(h.log, h.create))
	mux.Handle("GET /v1/notifications", httpx.Handle(h.log, h.list))
	mux.Handle("GET /v1/notifications/{id}", httpx.Handle(h.log, h.get))
	mux.Handle("POST /v1/notifications/{id}/cancel", httpx.Handle(h.log, h.cancel))
	mux.Handle("POST /v1/notifications/{id}/retry", httpx.Handle(h.log, h.retry))
	mux.Handle("POST /v1/notifications/retry", httpx.Handle(h.log, h.retryFailed))
	mux.Handle("POST /v1/batches", httpx.Handle(h.log, h.createBatch))
	mux.Handle("GET /v1/batches/{id}", httpx.Handle(h.log, h.getBatch))
}

// createRequest is the body of POST /v1/notifications. The member names are the provider contract's.
type createRequest struct {
	To       string `json:"to"`
	Channel  string `json:"channel"`
	Content  string `json:"content"`
	Priority string `json:"priority"`
}

// draft turns the request into a draft; a priority left out means normal.
func (req createRequest) draft() Draft {
	d := Draft{To: req.To, Channel: Channel(req.Channel), Content: req.Content, Priority: Priority(req.Priority)}
	if d.Priority == "" {
		d.Priority = PriorityNormal
	}
	return d
}

// idempotencyKey returns the request's Idempotency-Key header, "" when it has none, or a problem when it is
// malformed.
func idempotencyKey(r *http.Request) (string, error) {
	key := r.Header.Get("Idempotency-Key")
	if key != "" && (len(key) > maxIdempotencyKey || !isVisibleASCII(key)) {
		return "", httpx.NewProblem(http.StatusBadRequest, "invalid_idempotency_key",
			"the Idempotency-Key header must be 1 to 255 visible ASCII characters")
	}
	return key, nil
}

// notificationResponse is a notification as the API shows it.
type notificationResponse struct {
	ID                uuid.UUID     `json:"id"`
	To                string        `json:"to"`
	Channel           Channel       `json:"channel"`
	Content           string        `json:"content"`
	Priority          Priority      `json:"priority"`
	Status            Status        `json:"status"`
	CorrelationID     string        `json:"correlationId"`
	BatchID           *uuid.UUID    `json:"batchId,omitzero"`
	FailedAt          *time.Time    `json:"failedAt,omitzero"`
	Attempts          int           `json:"attempts"`
	FailureReason     FailureReason `json:"failureReason,omitzero"`
	ProviderMessageID string        `json:"providerMessageId,omitzero"`
	CreatedAt         time.Time     `json:"createdAt"`
	UpdatedAt         time.Time     `json:"updatedAt"`
}

// toResponse converts n field by field; struct tags do not take part in a conversion. A field added to
// Notification stops this from compiling until notificationResponse decides whether the API shows it.
func toResponse(n Notification) notificationResponse {
	return notificationResponse(n)
}

// create accepts a notification for sending: 202 with the notification and its Location, since it is accepted,
// not yet sent. A repeated request gets the same answer, marked Idempotent-Replayed.
func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	key, err := idempotencyKey(r)
	if err != nil {
		return err
	}
	var req createRequest
	if err := httpx.DecodeJSON(w, r, maxCreateBody, &req); err != nil {
		return err
	}

	n, created, err := h.svc.Create(r.Context(), req.draft(), key)
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
		p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the request is invalid")
		p.Errors = fieldProblems("", verr.Fields)
		return p
	}
	if berr, ok := errors.AsType[*BatchError](err); ok {
		p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "no notification of the batch is valid")
		for _, rej := range berr.Rejections {
			p.Errors = append(p.Errors, fieldProblems(itemPointer(rej.Index), rej.Fields)...)
		}
		return p
	}
	switch {
	case errors.Is(err, ErrIdempotencyKeyReused):
		return httpx.NewProblem(http.StatusUnprocessableEntity, "idempotency_key_reused",
			"this Idempotency-Key was used for a different request")
	case errors.Is(err, ErrNotFound):
		return httpx.NewProblem(http.StatusNotFound, "not_found", "nothing has this ID")
	case errors.Is(err, ErrNotCancellable):
		return httpx.NewProblem(http.StatusConflict, "not_cancellable", err.Error())
	case errors.Is(err, ErrNotRetryable):
		return httpx.NewProblem(http.StatusConflict, "not_retryable", err.Error())
	}
	return err
}

// fieldProblems locates each field error under prefix, a JSON Pointer to the object holding the fields.
func fieldProblems(prefix string, fields []FieldError) []httpx.FieldProblem {
	problems := make([]httpx.FieldProblem, 0, len(fields))
	for _, f := range fields {
		problems = append(problems, httpx.FieldProblem{Pointer: prefix + "/" + f.Field, Code: string(f.Code), Detail: f.Message})
	}
	return problems
}

// itemPointer is the JSON Pointer to a batch request's item.
func itemPointer(index int) string {
	return "/notifications/" + strconv.Itoa(index)
}
