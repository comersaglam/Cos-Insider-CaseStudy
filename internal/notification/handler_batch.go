package notification

import (
	"net/http"
	"time"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
)

// createBatchRequest is the body of POST /v1/batches.
type createBatchRequest struct {
	Notifications []createRequest `json:"notifications"`
}

// batchResponse is a batch as the API shows it. A created batch lists the ID each accepted item was given; a read
// batch counts its notifications by status instead, and the list endpoint lists them.
type batchResponse struct {
	ID            uuid.UUID           `json:"id"`
	Total         int                 `json:"total"`
	Accepted      int                 `json:"accepted"`
	Rejected      int                 `json:"rejected"`
	Notifications []batchItemResponse `json:"notifications,omitzero"`
	Statuses      map[Status]int      `json:"statuses,omitzero"`
	Rejections    []rejectionResponse `json:"rejections"`
	CorrelationID string              `json:"correlationId"`
	CreatedAt     time.Time           `json:"createdAt"`
}

type batchItemResponse struct {
	Index int       `json:"index"`
	ID    uuid.UUID `json:"id"`
}

// rejectionResponse is a rejected item, its problems located by JSON Pointer in the request.
type rejectionResponse struct {
	Index  int                  `json:"index"`
	Errors []httpx.FieldProblem `json:"errors"`
}

func toBatchResponse(b Batch) batchResponse {
	resp := batchResponse{
		ID: b.ID, Total: b.Total, Accepted: b.Accepted, Rejected: len(b.Rejections), Statuses: b.Statuses,
		Rejections: make([]rejectionResponse, 0, len(b.Rejections)), CorrelationID: b.CorrelationID, CreatedAt: b.CreatedAt,
	}
	for _, item := range b.Items {
		resp.Notifications = append(resp.Notifications, batchItemResponse(item))
	}
	for _, rej := range b.Rejections {
		resp.Rejections = append(resp.Rejections,
			rejectionResponse{Index: rej.Index, Errors: fieldProblems(itemPointer(rej.Index), rej.Fields)})
	}
	return resp
}

// createBatch accepts the valid items of a batch and reports the invalid ones: 202 with the batch and its
// Location while at least one item is accepted, 422 when none is. A repeated request gets the same answer, marked
// Idempotent-Replayed.
func (h *Handler) createBatch(w http.ResponseWriter, r *http.Request) error {
	key, err := idempotencyKey(r)
	if err != nil {
		return err
	}
	var req createBatchRequest
	if err := httpx.DecodeJSON(w, r, h.maxBatchBody, &req); err != nil {
		return err
	}

	drafts := make([]Draft, len(req.Notifications))
	for i, item := range req.Notifications {
		drafts[i] = item.draft()
	}
	b, created, err := h.svc.CreateBatch(r.Context(), drafts, key)
	if err != nil {
		return toProblem(err)
	}

	w.Header().Set("Location", "/v1/batches/"+b.ID.String())
	if !created {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	return httpx.WriteJSON(w, http.StatusAccepted, toBatchResponse(b))
}

// getBatch answers a batch by its ID, with its notifications counted by status now.
func (h *Handler) getBatch(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return toProblem(ErrNotFound)
	}
	b, err := h.svc.GetBatch(r.Context(), id)
	if err != nil {
		return toProblem(err)
	}
	return httpx.WriteJSON(w, http.StatusOK, toBatchResponse(b))
}
