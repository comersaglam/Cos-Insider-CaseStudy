package notification

import (
	"encoding/base64"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/httpx"
)

// Page sizes of the list.
const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// maxRetryBody bounds a bulk retry request, a handful of short members.
const maxRetryBody = 4 << 10

// listParameters are the query parameters the list takes; any other is refused, so a misspelt filter does not
// silently list everything.
var listParameters = map[string]bool{
	"status": true, "channel": true, "batchId": true, "createdFrom": true, "createdTo": true, "limit": true, "cursor": true,
}

// listResponse is a page of notifications. nextCursor, absent on the last page, asks for the next one.
type listResponse struct {
	Items      []notificationResponse `json:"items"`
	NextCursor string                 `json:"nextCursor,omitzero"`
}

// list answers a page of notifications, newest first, filtered by the query.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	f, after, limit, err := parseListQuery(r.URL.Query())
	if err != nil {
		return err
	}
	page, err := h.svc.List(r.Context(), f, after, limit)
	if err != nil {
		return toProblem(err)
	}
	resp := listResponse{Items: make([]notificationResponse, 0, len(page.Items))}
	for _, n := range page.Items {
		resp.Items = append(resp.Items, toResponse(n))
	}
	if page.Next != nil {
		resp.NextCursor = encodeCursor(*page.Next)
	}
	return httpx.WriteJSON(w, http.StatusOK, resp)
}

// parseListQuery reads the list's filter, cursor and page size, reporting every bad parameter at once.
func parseListQuery(q url.Values) (f ListFilter, after *Cursor, limit int, err error) {
	limit = defaultPageSize
	var problems []httpx.FieldProblem
	bad := func(parameter, detail string) {
		problems = append(problems, httpx.FieldProblem{Parameter: parameter, Code: string(CodeInvalid), Detail: detail})
	}

	for _, name := range slices.Sorted(maps.Keys(q)) {
		values := q[name]
		switch {
		case !listParameters[name]:
			bad(name, "is not a parameter of this list")
			continue
		case name != "status" && name != "channel" && len(values) > 1:
			bad(name, "must be given once")
			continue
		}
		switch v := values[0]; name {
		case "status":
			for _, v := range values {
				if s := Status(v); s.Valid() {
					f.Statuses = append(f.Statuses, s)
				} else {
					bad(name, "must be one of scheduled, queued, sending, retrying, sent, failed, cancelled")
				}
			}
		case "channel":
			for _, v := range values {
				if c := Channel(v); c.Valid() {
					f.Channels = append(f.Channels, c)
				} else {
					bad(name, "must be one of sms, email, push")
				}
			}
		case "batchId":
			if id, err := uuid.Parse(v); err == nil {
				f.BatchID = &id
			} else {
				bad(name, "must be a UUID")
			}
		case "createdFrom", "createdTo":
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				bad(name, "must be an RFC 3339 time with its offset, such as 2026-11-23T10:00:00Z")
			} else if name == "createdFrom" {
				f.CreatedFrom = t
			} else {
				f.CreatedTo = t
			}
		case "limit":
			if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= maxPageSize {
				limit = n
			} else {
				bad(name, "must be a whole number from 1 to "+strconv.Itoa(maxPageSize))
			}
		case "cursor":
			if c, ok := decodeCursor(v); ok {
				after = &c
			} else {
				bad(name, "is not a cursor this API gave out")
			}
		}
	}
	if !f.CreatedFrom.IsZero() && !f.CreatedTo.IsZero() && !f.CreatedTo.After(f.CreatedFrom) {
		bad("createdTo", "must be after createdFrom")
	}

	if len(problems) > 0 {
		p := httpx.NewProblem(http.StatusBadRequest, "invalid_query", "the query is invalid")
		p.Errors = problems
		return ListFilter{}, nil, 0, p
	}
	return f, after, limit, nil
}

// encodeCursor writes a cursor as an opaque string. Clients pass it back unchanged and must not read it; its
// form may change.
func encodeCursor(c Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.CreatedAt.UTC().Format(time.RFC3339Nano) + " " + c.ID.String()))
}

func decodeCursor(s string) (Cursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, false
	}
	at, id, ok := strings.Cut(string(raw), " ")
	if !ok {
		return Cursor{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return Cursor{}, false
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return Cursor{}, false
	}
	return Cursor{CreatedAt: t, ID: u}, true
}

// cancel cancels a notification that is still waiting: 200 with it, or 409 when it is being sent or finished.
func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return toProblem(ErrNotFound)
	}
	n, err := h.svc.Cancel(r.Context(), id)
	if err != nil {
		return toProblem(err)
	}
	return httpx.WriteJSON(w, http.StatusOK, toResponse(n))
}

// retry sends a failed notification again: 202 with it, queued, or 409 when it is not failed.
func (h *Handler) retry(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return toProblem(ErrNotFound)
	}
	n, err := h.svc.Retry(r.Context(), id)
	if err != nil {
		return toProblem(err)
	}
	w.Header().Set("Location", "/v1/notifications/"+n.ID.String())
	return httpx.WriteJSON(w, http.StatusAccepted, toResponse(n))
}

// retryRequest is the body of POST /v1/notifications/retry.
type retryRequest struct {
	Channel    string     `json:"channel"`
	BatchID    *uuid.UUID `json:"batchId"`
	FailedFrom time.Time  `json:"failedFrom"`
	FailedTo   time.Time  `json:"failedTo"`
	DryRun     bool       `json:"dryRun"`
}

type retryResponse struct {
	Matched  int  `json:"matched"`
	Requeued int  `json:"requeued"`
	DryRun   bool `json:"dryRun"`
}

// retryFailed sends failed notifications again in bulk (ADR 0014): 202 with how many matched and how many were
// requeued, or 200 on a dry run, which changes nothing.
func (h *Handler) retryFailed(w http.ResponseWriter, r *http.Request) error {
	var req retryRequest
	if err := httpx.DecodeJSON(w, r, maxRetryBody, &req); err != nil {
		return err
	}
	res, err := h.svc.RetryFailed(r.Context(), RetryFilter{
		Channel: Channel(req.Channel), BatchID: req.BatchID, FailedFrom: req.FailedFrom, FailedTo: req.FailedTo,
		DryRun: req.DryRun,
	})
	if err != nil {
		return toProblem(err)
	}
	status := http.StatusAccepted
	if res.DryRun {
		status = http.StatusOK
	}
	return httpx.WriteJSON(w, status, retryResponse(res))
}
