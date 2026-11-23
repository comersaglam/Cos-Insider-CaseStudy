package notification

import (
	"errors"
	"slices"
	"testing"
	"time"

	"uuid"
)

var orderShipped = TemplateDraft{Name: "order-shipped", Channel: ChannelSMS, Body: "Hi {{.name}}, your order {{.order}} has shipped."}

func TestServiceCreateTemplate(t *testing.T) {
	store := &fakeStore{}
	svc := newTestService(store)

	if _, err := svc.CreateTemplate(t.Context(), TemplateDraft{Name: "Bad Name", Channel: ChannelSMS, Body: "{{if .x}}y{{end}}"}); !isValidation(err, "name", "body") {
		t.Errorf("invalid draft = %v, want a ValidationError on name and body", err)
	}
	if len(store.templates) != 0 {
		t.Fatal("an invalid template was stored")
	}

	created, err := svc.CreateTemplate(t.Context(), orderShipped)
	if err != nil || created.ID == (uuid.UUID{}) || created.Name != "order-shipped" || store.templates[created.ID].Body != orderShipped.Body {
		t.Errorf("CreateTemplate = %+v, %v", created, err)
	}
}

func TestServiceUpdateTemplate(t *testing.T) {
	store := &fakeStore{}
	svc := newTestService(store)
	created, _ := svc.CreateTemplate(t.Context(), orderShipped)

	if _, err := svc.UpdateTemplate(t.Context(), created.ID, TemplateDraft{Name: "order-shipped", Channel: "fax", Body: "x"}); !isValidation(err, "channel") {
		t.Errorf("invalid update = %v, want a ValidationError on channel", err)
	}
	changed := TemplateDraft{Name: "order-on-its-way", Channel: ChannelPush, Body: "Order {{.order}} is on its way."}
	updated, err := svc.UpdateTemplate(t.Context(), created.ID, changed)
	if err != nil || updated.ID != created.ID || store.templates[created.ID].Name != "order-on-its-way" {
		t.Errorf("UpdateTemplate = %+v, %v", updated, err)
	}
	if _, err := svc.UpdateTemplate(t.Context(), uuid.NewV7(), changed); !errors.Is(err, ErrNotFound) {
		t.Errorf("update of an unknown template = %v, want ErrNotFound", err)
	}
}

func TestServiceListTemplatesPages(t *testing.T) {
	at := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{listedTmpls: []Template{
		{ID: uuid.NewV7(), Name: "c", CreatedAt: at}, {ID: uuid.NewV7(), Name: "b", CreatedAt: at}, {ID: uuid.NewV7(), Name: "a", CreatedAt: at},
	}}
	page, err := newTestService(store).ListTemplates(t.Context(), nil, 2)
	if err != nil || len(page.Items) != 2 || page.Next == nil || page.Next.ID != store.listedTmpls[1].ID || store.listLimits[0] != 3 {
		t.Errorf("ListTemplates = %+v, %v; want two and a cursor at the second, read with one more", page, err)
	}
}

// isValidation reports whether err is a *ValidationError on exactly the given fields, in order.
func isValidation(err error, fields ...string) bool {
	verr, ok := errors.AsType[*ValidationError](err)
	if !ok {
		return false
	}
	var got []string
	for _, f := range verr.Fields {
		got = append(got, f.Field)
	}
	return slices.Equal(got, fields)
}
