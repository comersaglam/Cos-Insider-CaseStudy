package notification

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"
)

var storedBatch = Batch{
	ID: uuid.MustParse("01938f2e-7c1e-7b3a-9d4e-2f1a3b4c5d70"), Total: 3, Accepted: 2,
	Items: []BatchItem{
		{Index: 0, ID: uuid.MustParse("01938f2e-7c1e-7b3a-9d4e-2f1a3b4c5d71")},
		{Index: 2, ID: uuid.MustParse("01938f2e-7c1e-7b3a-9d4e-2f1a3b4c5d72")},
	},
	Rejections:    []Rejection{{Index: 1, Fields: []FieldError{{Field: "to", Code: CodeInvalid, Message: "must be E.164"}}}},
	CorrelationID: "corr-b",
	CreatedAt:     time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC),
}

const batchBody = `{"notifications":[
	{"to":"+905551234567","channel":"sms","content":"one"},
	{"to":"05551234567","channel":"sms","content":"two"},
	{"to":"ayse@example.com","channel":"email","content":"three","priority":"low"}]}`

func TestCreateBatchAcceptsTheValidItems(t *testing.T) {
	svc := &fakeService{batch: storedBatch}
	rec := serve(t, svc, http.MethodPost, "/v1/batches", batchBody, map[string]string{"Idempotency-Key": "campaign-7"})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/v1/batches/"+storedBatch.ID.String() {
		t.Errorf("Location = %q", loc)
	}
	got := body(t, rec)
	if got["total"] != float64(3) || got["accepted"] != float64(2) || got["rejected"] != float64(1) {
		t.Errorf("counts = %v %v %v, want 3 2 1", got["total"], got["accepted"], got["rejected"])
	}
	if fmt.Sprint(got["notifications"]) != fmt.Sprintf("[map[id:%s index:0] map[id:%s index:2]]",
		storedBatch.Items[0].ID, storedBatch.Items[1].ID) {
		t.Errorf("notifications = %v", got["notifications"])
	}
	if fmt.Sprint(got["rejections"]) != "[map[errors:[map[code:invalid detail:must be E.164 pointer:/notifications/1/to]] index:1]]" {
		t.Errorf("rejections = %v", got["rejections"])
	}

	drafts := svc.batchDrafts[0]
	if len(drafts) != 3 || drafts[0].Priority != PriorityNormal || drafts[2].Priority != PriorityLow {
		t.Errorf("drafts = %+v, want three, priority defaulted to normal where left out", drafts)
	}
	if svc.keys[0] != "campaign-7" {
		t.Errorf("key = %q", svc.keys[0])
	}
}

func TestCreateBatchMarksARepeatedRequest(t *testing.T) {
	rec := serve(t, &fakeService{batch: storedBatch, replay: true}, http.MethodPost, "/v1/batches", batchBody, nil)
	if rec.Code != http.StatusAccepted || rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Errorf("got %d, Idempotent-Replayed %q", rec.Code, rec.Header().Get("Idempotent-Replayed"))
	}
}

func TestCreateBatchErrors(t *testing.T) {
	nothingValid := &BatchError{Rejections: []Rejection{
		{Index: 0, Fields: []FieldError{{Field: "channel", Code: CodeInvalid, Message: "must be one of sms, email, push"}}},
		{Index: 1, Fields: []FieldError{{Field: "content", Code: CodeRequired, Message: "is required"}}},
	}}
	tooMany := &ValidationError{Fields: []FieldError{{Field: "notifications", Code: CodeTooLong, Message: "must hold at most 1000"}}}
	tests := []struct {
		name         string
		svc          *fakeService
		limit        int64
		body         string
		header       map[string]string
		wantStatus   int
		wantCode     string
		wantPointers string
	}{
		{"nothing valid", &fakeService{err: nothingValid}, testBatchLimit, batchBody, nil, 422, "validation_failed",
			"/notifications/0/channel /notifications/1/content"},
		{"too many items", &fakeService{err: tooMany}, testBatchLimit, batchBody, nil, 422, "validation_failed", "/notifications"},
		{"key reused", &fakeService{err: ErrIdempotencyKeyReused}, testBatchLimit, batchBody,
			map[string]string{"Idempotency-Key": "k"}, 422, "idempotency_key_reused", ""},
		{"malformed key", &fakeService{}, testBatchLimit, batchBody, map[string]string{"Idempotency-Key": "a b"}, 400,
			"invalid_idempotency_key", ""},
		{"over the body limit", &fakeService{}, 64, batchBody, nil, 413, "body_too_large", ""},
		{"not an object", &fakeService{}, testBatchLimit, `[{"to":"x"}]`, nil, 400, "invalid_json", ""},
		{"unexpected failure", &fakeService{err: errors.New("down")}, testBatchLimit, batchBody, nil, 500, "internal_error", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveWithLimit(t, tt.svc, tt.limit, http.MethodPost, "/v1/batches", tt.body, tt.header)
			got := body(t, rec)
			if rec.Code != tt.wantStatus || got["code"] != tt.wantCode {
				t.Fatalf("got %d %v, want %d %s", rec.Code, got["code"], tt.wantStatus, tt.wantCode)
			}
			var pointers []string
			errs, _ := got["errors"].([]any)
			for _, e := range errs {
				pointers = append(pointers, e.(map[string]any)["pointer"].(string))
			}
			if strings.Join(pointers, " ") != tt.wantPointers {
				t.Errorf("pointers = %v, want %s", pointers, tt.wantPointers)
			}
		})
	}
}

func TestGetBatch(t *testing.T) {
	read := storedBatch
	read.Items = nil
	read.Statuses = map[Status]int{StatusQueued: 1, StatusSent: 1}

	tests := []struct {
		name, path string
		wantStatus int
	}{
		{"found", "/v1/batches/" + read.ID.String(), 200},
		{"unknown ID", "/v1/batches/" + uuid.NewV7().String(), 404},
		{"not a UUID", "/v1/batches/7", 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, &fakeService{batch: read}, http.MethodGet, tt.path, "", nil)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus != 200 {
				return
			}
			got := body(t, rec)
			if fmt.Sprint(got["statuses"]) != "map[queued:1 sent:1]" || got["rejected"] != float64(1) {
				t.Errorf("statuses = %v, rejected = %v", got["statuses"], got["rejected"])
			}
			if _, ok := got["notifications"]; ok {
				t.Error("a read batch lists its notifications; the list endpoint does that")
			}
		})
	}
}
