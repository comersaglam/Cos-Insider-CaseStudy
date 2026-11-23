package notification

import (
	"errors"
	"testing"
	"time"
	"uuid"
)

func TestServiceListGivesTheNextCursorOnlyWhenMoreRemain(t *testing.T) {
	var listed []Notification
	for range 3 {
		listed = append(listed, Notification{ID: uuid.NewV7(), CreatedAt: time.Now()})
	}

	store := &fakeStore{listed: listed}
	page, err := newTestService(store).List(t.Context(), ListFilter{}, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if store.listLimits[0] != 3 {
		t.Errorf("asked the store for %d, want one more than the page to learn whether more remain", store.listLimits[0])
	}
	if len(page.Items) != 2 || page.Next == nil || page.Next.ID != listed[1].ID {
		t.Errorf("page = %d items, next %v; want 2 items and a cursor at the second", len(page.Items), page.Next)
	}

	page, err = newTestService(&fakeStore{listed: listed}).List(t.Context(), ListFilter{}, nil, 3)
	if err != nil || len(page.Items) != 3 || page.Next != nil {
		t.Errorf("last page = %d items, next %v, %v; want 3 items and no cursor", len(page.Items), page.Next, err)
	}
}

func TestServiceRetryFailedChecksTheFilter(t *testing.T) {
	from := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		filter RetryFilter
		want   []string // "field:code"
	}{
		{"no window", RetryFilter{}, []string{"failedFrom:required", "failedTo:required"}},
		{"an empty window", RetryFilter{FailedFrom: from, FailedTo: from}, []string{"failedTo:invalid"}},
		{"an unknown channel", RetryFilter{Channel: "fax", FailedFrom: from, FailedTo: from.Add(time.Hour)}, []string{"channel:invalid"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTestService(&fakeStore{}).RetryFailed(t.Context(), tt.filter)
			if got := fieldCodes(t, err); len(got) != len(tt.want) || got[0] != tt.want[0] || got[len(got)-1] != tt.want[len(tt.want)-1] {
				t.Errorf("RetryFailed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceRetryFailed(t *testing.T) {
	window := RetryFilter{FailedFrom: time.Now().Add(-time.Hour), FailedTo: time.Now()}

	store := &fakeStore{failedCount: 12}
	dry := window
	dry.DryRun = true
	got, err := newTestService(store).RetryFailed(t.Context(), dry)
	if err != nil || got != (RetryResult{Matched: 12, DryRun: true}) || store.requeueLimit != 0 {
		t.Errorf("dry run = %+v, %v, requeue limit %d; want 12 matched and nothing requeued", got, err, store.requeueLimit)
	}

	store = &fakeStore{failedCount: 12}
	got, err = newTestService(store).RetryFailed(t.Context(), window)
	if err != nil || got != (RetryResult{Matched: 12, Requeued: 12}) || store.requeueLimit != MaxRequeue {
		t.Errorf("retry = %+v, %v, limit %d; want 12 requeued under MaxRequeue", got, err, store.requeueLimit)
	}

	if _, err := newTestService(&fakeStore{err: errors.New("down")}).RetryFailed(t.Context(), window); err == nil {
		t.Error("a store failure was not reported")
	}
}

func TestServiceCancelAndRetry(t *testing.T) {
	id := uuid.NewV7()
	if n, err := newTestService(&fakeStore{}).Cancel(t.Context(), id); err != nil || n.Status != StatusCancelled {
		t.Errorf("Cancel = %s, %v", n.Status, err)
	}
	if n, err := newTestService(&fakeStore{}).Retry(t.Context(), id); err != nil || n.Status != StatusQueued {
		t.Errorf("Retry = %s, %v", n.Status, err)
	}
	if _, err := newTestService(&fakeStore{err: ErrNotCancellable}).Cancel(t.Context(), id); !errors.Is(err, ErrNotCancellable) {
		t.Errorf("Cancel = %v, want ErrNotCancellable", err)
	}
}
