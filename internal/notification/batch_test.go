package notification

import (
	"errors"
	"strings"
	"testing"
	"uuid"
)

func TestBatchFingerprint(t *testing.T) {
	a := Draft{To: "+905551234567", Channel: ChannelSMS, Content: "one", Priority: PriorityNormal}
	b := Draft{To: "+905551234568", Channel: ChannelSMS, Content: "two", Priority: PriorityNormal}

	if first, second := []Draft{a, b}, []Draft{a, b}; BatchFingerprint(first) != BatchFingerprint(second) {
		t.Error("two equal batches gave different fingerprints")
	}
	if BatchFingerprint([]Draft{a, b}) == BatchFingerprint([]Draft{b, a}) {
		t.Error("the order of the items does not change the fingerprint")
	}
	if BatchFingerprint([]Draft{a}) == BatchFingerprint([]Draft{a, a}) {
		t.Error("a repeated item does not change the fingerprint")
	}
}

func TestCreateBatchAcceptsTheValidItemsAndReportsTheRest(t *testing.T) {
	store := &fakeStore{}
	drafts := []Draft{
		testDraft,
		{To: "05551234567", Channel: ChannelSMS, Content: "national format", Priority: PriorityNormal}, // invalid
		{To: "ayse@example.com", Channel: ChannelEmail, Content: "hello", Priority: PriorityLow},
		{Channel: "fax", Priority: PriorityLow}, // invalid three times over
	}

	b, created, err := newTestService(store).CreateBatch(t.Context(), drafts, "")
	if err != nil || !created {
		t.Fatalf("CreateBatch = created %v, err %v", created, err)
	}
	if b.Total != 4 || len(b.Items) != 2 || len(b.Rejections) != 2 {
		t.Fatalf("batch = %d total, %d accepted, %d rejected; want 4, 2, 2", b.Total, len(b.Items), len(b.Rejections))
	}
	if b.Items[0].Index != 0 || b.Items[1].Index != 2 {
		t.Errorf("accepted indexes = %d, %d; want 0, 2", b.Items[0].Index, b.Items[1].Index)
	}
	if r := b.Rejections[0]; r.Index != 1 || len(r.Fields) != 1 || r.Fields[0].Field != "to" {
		t.Errorf("first rejection = %+v, want index 1 for its recipient", r)
	}
	if r := b.Rejections[1]; r.Index != 3 || len(r.Fields) != 3 {
		t.Errorf("second rejection = %+v, want index 3 with three fields", r)
	}

	stored := store.batches[0]
	if len(stored.Items) != 2 {
		t.Fatalf("stored %d notifications, want 2", len(stored.Items))
	}
	for _, item := range stored.Items {
		n := item.Notification
		if n.Status != StatusQueued || n.ID == (uuid.UUID{}) || n.CorrelationID == "" {
			t.Errorf("item %d = %+v, want queued with an ID and a correlation ID", item.Index, n)
		}
	}
	if stored.Items[1].Notification.To != "ayse@example.com" {
		t.Errorf("second notification goes to %q", stored.Items[1].Notification.To)
	}
}

func TestCreateBatchWithAKeyFingerprintsEveryItem(t *testing.T) {
	store := &fakeStore{}
	drafts := []Draft{testDraft, {Channel: "fax"}}
	if _, _, err := newTestService(store).CreateBatch(t.Context(), drafts, "campaign-7"); err != nil {
		t.Fatal(err)
	}
	if got := store.batchKeys[0]; got.Key != "campaign-7" || got.Fingerprint != BatchFingerprint(drafts) {
		t.Errorf("key = %+v, want the key with the fingerprint of all items, rejected ones included", got)
	}
}

func TestCreateBatchRefusesABatchWithNothingValid(t *testing.T) {
	store := &fakeStore{}
	_, _, err := newTestService(store).CreateBatch(t.Context(), []Draft{{Channel: "fax"}, {Channel: ChannelSMS}}, "")

	berr, ok := errors.AsType[*BatchError](err)
	if !ok || len(berr.Rejections) != 2 {
		t.Fatalf("CreateBatch = %v, want a *BatchError with two rejections", err)
	}
	if len(store.batches) != 0 {
		t.Error("a batch with nothing valid was stored")
	}
}

func TestCreateBatchSize(t *testing.T) {
	tests := []struct {
		name  string
		size  int
		code  Code
		valid bool
	}{
		{"empty", 0, CodeRequired, false},
		{"one", 1, "", true},
		{"the most allowed", MaxBatchSize, "", true},
		{"one too many", MaxBatchSize + 1, CodeTooLong, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drafts := make([]Draft, tt.size)
			for i := range drafts {
				drafts[i] = testDraft
			}
			_, _, err := newTestService(&fakeStore{}).CreateBatch(t.Context(), drafts, "")
			if tt.valid {
				if err != nil {
					t.Errorf("CreateBatch = %v, want accepted", err)
				}
				return
			}
			verr, ok := errors.AsType[*ValidationError](err)
			if !ok || len(verr.Fields) != 1 || verr.Fields[0].Field != "notifications" || verr.Fields[0].Code != tt.code {
				t.Errorf("CreateBatch = %v, want notifications %s", err, tt.code)
			}
		})
	}
}

func TestServiceGetBatch(t *testing.T) {
	want := Batch{ID: uuid.NewV7(), Total: 3}
	if b, err := newTestService(&fakeStore{storedBatch: want}).GetBatch(t.Context(), want.ID); err != nil || b.ID != want.ID {
		t.Errorf("GetBatch = %v, %v", b.ID, err)
	}
	if _, err := newTestService(&fakeStore{batchNotFound: true}).GetBatch(t.Context(), want.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetBatch = %v, want ErrNotFound", err)
	}
}

func TestBatchErrorMessage(t *testing.T) {
	err := &BatchError{Rejections: []Rejection{{Index: 4, Fields: []FieldError{{Field: "to", Message: "is required"}}}}}
	if !strings.Contains(err.Error(), "4") {
		t.Errorf("Error() = %q, want the index", err.Error())
	}
}
