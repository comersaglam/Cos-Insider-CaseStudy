package notification

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"
)

func TestListPassesTheQueryOn(t *testing.T) {
	next := &Cursor{CreatedAt: time.Date(2026, 11, 23, 12, 0, 0, 123456000, time.UTC), ID: uuid.NewV7()}
	svc := &fakeService{page: Page{Items: []Notification{stored}, Next: next}}
	batchID := uuid.NewV7()
	q := url.Values{
		"status": {"queued", "failed"}, "channel": {"sms"}, "batchId": {batchID.String()},
		"createdFrom": {"2026-11-23T00:00:00Z"}, "createdTo": {"2026-11-24T00:00:00+03:00"}, "limit": {"10"},
	}
	rec := serve(t, svc, http.MethodGet, "/v1/notifications?"+q.Encode(), "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	want := ListFilter{
		Statuses: []Status{StatusQueued, StatusFailed}, Channels: []Channel{ChannelSMS}, BatchID: &batchID,
		CreatedFrom: time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC), CreatedTo: time.Date(2026, 11, 23, 21, 0, 0, 0, time.UTC),
	}
	got := svc.filters[0]
	if fmt.Sprint(got.Statuses, got.Channels, *got.BatchID) != fmt.Sprint(want.Statuses, want.Channels, *want.BatchID) ||
		!got.CreatedFrom.Equal(want.CreatedFrom) || !got.CreatedTo.Equal(want.CreatedTo) || svc.limits[0] != 10 {
		t.Errorf("filter = %+v limit %d, want %+v limit 10", got, svc.limits[0], want)
	}

	body := body(t, rec)
	items, _ := body["items"].([]any)
	cursor, _ := body["nextCursor"].(string)
	if len(items) != 1 || cursor == "" {
		t.Fatalf("body = %v, want one item and a next cursor", body)
	}

	// The cursor given out comes back as the same position.
	serve(t, svc, http.MethodGet, "/v1/notifications?cursor="+url.QueryEscape(cursor), "", nil)
	if c := svc.cursors[1]; c == nil || !c.CreatedAt.Equal(next.CreatedAt) || c.ID != next.ID {
		t.Errorf("cursor came back as %+v, want %+v", c, next)
	}
}

func TestListDefaults(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, http.MethodGet, "/v1/notifications", "", nil)
	got := body(t, rec)
	if rec.Code != 200 || svc.limits[0] != 50 || svc.cursors[0] != nil {
		t.Errorf("got %d, limit %d, cursor %v; want 200, 50, none", rec.Code, svc.limits[0], svc.cursors[0])
	}
	if items, ok := got["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("items = %v, want an empty list, not null", got["items"])
	}
	if _, ok := got["nextCursor"]; ok {
		t.Error("the last page has a next cursor")
	}
}

func TestListRefusesABadQuery(t *testing.T) {
	tests := []struct{ name, query, parameter string }{
		{"unknown status", "status=pending", "status"},
		{"unknown channel", "channel=fax", "channel"},
		{"batch ID not a UUID", "batchId=7", "batchId"},
		{"date without a time zone", "createdFrom=2026-11-23T10:00:00", "createdFrom"},
		{"an empty range", "createdFrom=2026-11-23T10:00:00Z&createdTo=2026-11-23T10:00:00Z", "createdTo"},
		{"limit zero", "limit=0", "limit"},
		{"limit over the maximum", "limit=101", "limit"},
		{"limit not a number", "limit=ten", "limit"},
		{"a parameter twice", "limit=5&limit=6", "limit"},
		{"a forged cursor", "cursor=bm90LWEtY3Vyc29y", "cursor"},
		{"a misspelt parameter", "stauts=failed", "stauts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{}
			rec := serve(t, svc, http.MethodGet, "/v1/notifications?"+tt.query, "", nil)
			got := body(t, rec)
			errs, _ := got["errors"].([]any)
			if rec.Code != 400 || got["code"] != "invalid_query" || len(errs) == 0 {
				t.Fatalf("got %d %v", rec.Code, got)
			}
			if first, _ := errs[0].(map[string]any); first["parameter"] != tt.parameter {
				t.Errorf("parameter = %v, want %s", first["parameter"], tt.parameter)
			}
			if len(svc.filters) != 0 {
				t.Error("the service was reached with a bad query")
			}
		})
	}
}

func TestCancel(t *testing.T) {
	id := uuid.NewV7().String()
	tests := []struct {
		name, id   string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"waiting", id, nil, 200, ""},
		{"being sent", id, fmt.Errorf("%w: it is sending", ErrNotCancellable), 409, "not_cancellable"},
		{"unknown", id, ErrNotFound, 404, "not_found"},
		{"not a UUID", "42", nil, 404, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, &fakeService{result: stored, err: tt.err}, http.MethodPost, "/v1/notifications/"+tt.id+"/cancel", "", nil)
			got := body(t, rec)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %v", rec.Code, tt.wantStatus, got)
			}
			if tt.wantCode == "" && got["status"] != "cancelled" {
				t.Errorf("status member = %v, want cancelled", got["status"])
			}
			if tt.wantCode != "" && got["code"] != tt.wantCode {
				t.Errorf("code = %v, want %s", got["code"], tt.wantCode)
			}
			if tt.wantStatus == 409 && !strings.Contains(fmt.Sprint(got["detail"]), "sending") {
				t.Errorf("detail = %v, want the current status named", got["detail"])
			}
		})
	}
}

func TestRetry(t *testing.T) {
	id := uuid.NewV7().String()
	rec := serve(t, &fakeService{result: stored}, http.MethodPost, "/v1/notifications/"+id+"/retry", "", nil)
	if rec.Code != http.StatusAccepted || body(t, rec)["status"] != "queued" || rec.Header().Get("Location") != "/v1/notifications/"+id {
		t.Errorf("retry = %d %s, Location %q", rec.Code, rec.Body, rec.Header().Get("Location"))
	}
	rec = serve(t, &fakeService{err: fmt.Errorf("%w: it is queued", ErrNotRetryable)}, http.MethodPost, "/v1/notifications/"+id+"/retry", "", nil)
	if rec.Code != http.StatusConflict || body(t, rec)["code"] != "not_retryable" {
		t.Errorf("retrying a queued notification = %d %s", rec.Code, rec.Body)
	}
}

func TestRetryFailed(t *testing.T) {
	batchID := uuid.NewV7()
	reqBody := `{"channel":"sms","batchId":"` + batchID.String() + `","failedFrom":"2026-11-23T10:00:00Z","failedTo":"2026-11-23T11:00:00Z"}`

	svc := &fakeService{retried: RetryResult{Matched: 12000, Requeued: MaxRequeue}}
	rec := serve(t, svc, http.MethodPost, "/v1/notifications/retry", reqBody, nil)
	got := body(t, rec)
	if rec.Code != http.StatusAccepted || got["matched"] != float64(12000) || got["requeued"] != float64(MaxRequeue) {
		t.Errorf("retry = %d %v", rec.Code, got)
	}
	f := svc.retryFilter[0]
	if f.Channel != ChannelSMS || *f.BatchID != batchID || !f.FailedFrom.Equal(time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)) || f.DryRun {
		t.Errorf("filter = %+v", f)
	}

	svc = &fakeService{retried: RetryResult{Matched: 7, DryRun: true}}
	rec = serve(t, svc, http.MethodPost, "/v1/notifications/retry", strings.Replace(reqBody, "}", `,"dryRun":true}`, 1), nil)
	if got := body(t, rec); rec.Code != http.StatusOK || got["dryRun"] != true || got["requeued"] != float64(0) || !svc.retryFilter[0].DryRun {
		t.Errorf("dry run = %d %v", rec.Code, got)
	}

	invalid := &ValidationError{Fields: []FieldError{{Field: "failedFrom", Code: CodeRequired, Message: "is required"}}}
	rec = serve(t, &fakeService{err: invalid}, http.MethodPost, "/v1/notifications/retry", `{}`, nil)
	errs, _ := body(t, rec)["errors"].([]any)
	if rec.Code != 422 || len(errs) != 1 || errs[0].(map[string]any)["pointer"] != "/failedFrom" {
		t.Errorf("invalid filter = %d %s", rec.Code, rec.Body)
	}
}
