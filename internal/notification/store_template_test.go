package notification

import (
	"errors"
	"testing"
	"time"

	"uuid"
)

func newTemplate(name string) Template {
	return Template{ID: uuid.NewV7(), Name: name, Channel: ChannelSMS, Body: "Hi {{.name}}, your order {{.order}} has shipped."}
}

func TestStoreCreatesReadsUpdatesAndDeletesATemplate(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	created, err := store.CreateTemplate(t.Context(), newTemplate("order-shipped"))
	if err != nil || created.Name != "order-shipped" || created.CreatedAt.IsZero() || created.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreateTemplate = %+v, %v", created, err)
	}
	if got, err := store.Template(t.Context(), created.ID); err != nil || got != created {
		t.Errorf("Template = %+v, %v; want what was created", got, err)
	}

	changed := created
	changed.Name, changed.Channel, changed.Body = "order-dispatched", ChannelPush, "Order {{.order}} is on its way."
	updated, err := store.UpdateTemplate(t.Context(), changed)
	if err != nil || updated.Name != "order-dispatched" || updated.Channel != ChannelPush || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("UpdateTemplate = %+v, %v", updated, err)
	}

	if err := store.DeleteTemplate(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Template(t.Context(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Template after delete = %v, want ErrNotFound", err)
	}
	if err := store.DeleteTemplate(t.Context(), created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteTemplate twice = %v, want ErrNotFound", err)
	}
	if _, err := store.UpdateTemplate(t.Context(), changed); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateTemplate of a deleted template = %v, want ErrNotFound", err)
	}
}

func TestStoreRefusesATemplateNameTaken(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	if _, err := store.CreateTemplate(t.Context(), newTemplate("order-shipped")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTemplate(t.Context(), newTemplate("order-shipped")); !errors.Is(err, ErrTemplateNameTaken) {
		t.Errorf("a second order-shipped = %v, want ErrTemplateNameTaken", err)
	}
	other, err := store.CreateTemplate(t.Context(), newTemplate("order-late"))
	if err != nil {
		t.Fatal(err)
	}
	other.Name = "order-shipped"
	if _, err := store.UpdateTemplate(t.Context(), other); !errors.Is(err, ErrTemplateNameTaken) {
		t.Errorf("renaming to a taken name = %v, want ErrTemplateNameTaken", err)
	}
}

func TestStoreListsTemplatesNewestFirstByCursor(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)
	var ids []uuid.UUID
	for _, name := range []string{"first", "second", "third"} {
		tmpl, err := store.CreateTemplate(t.Context(), newTemplate(name))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, tmpl.ID)
	}
	page, err := store.ListTemplates(t.Context(), nil, 2)
	if err != nil || len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[1] {
		t.Fatalf("first page = %v, %v", page, err)
	}
	rest, err := store.ListTemplates(t.Context(), &Cursor{CreatedAt: page[1].CreatedAt, ID: page[1].ID}, 2)
	if err != nil || len(rest) != 1 || rest[0].ID != ids[0] {
		t.Errorf("second page = %v, %v", rest, err)
	}
}
