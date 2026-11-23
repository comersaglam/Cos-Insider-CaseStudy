package notification

import (
	"strings"
	"testing"

	"uuid"
)

// withTemplate returns a fake store holding one template, and its ID.
func withTemplate(t Template) (*fakeStore, uuid.UUID) {
	t.ID = uuid.NewV7()
	return &fakeStore{templates: map[uuid.UUID]Template{t.ID: t}}, t.ID
}

var shipped = Template{Name: "order-shipped", Channel: ChannelSMS, Body: "Hi {{.name}}, your order {{.order}} has shipped."}

func TestServiceCreateRendersATemplate(t *testing.T) {
	store, id := withTemplate(shipped)
	n, _, err := newTestService(store).Create(t.Context(), Draft{
		To: "+905551234567", Priority: PriorityNormal, TemplateID: &id, Variables: map[string]string{"name": "Ayşe", "order": "42"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	got := store.created[0]
	if got.Channel != ChannelSMS || got.Content != "Hi Ayşe, your order 42 has shipped." || got.TemplateID == nil || *got.TemplateID != id {
		t.Errorf("stored %s %q from %v", got.Channel, got.Content, got.TemplateID)
	}
	if n.Content != got.Content {
		t.Errorf("answered %q", n.Content)
	}
}

func TestServiceCreateRefusesATemplateItCannotRender(t *testing.T) {
	long := shipped
	long.Body = strings.Repeat("a", 1590) + "{{.name}}"
	tests := []struct {
		name     string
		template Template
		edit     func(d *Draft, id uuid.UUID)
		want     []string
	}{
		{"an unknown template", shipped, func(d *Draft, _ uuid.UUID) { other := uuid.NewV7(); d.TemplateID = &other }, []string{"templateId:invalid"}},
		{"a missing variable", shipped, func(d *Draft, _ uuid.UUID) { delete(d.Variables, "order") }, []string{"variables:required"}},
		{"content beside the template", shipped, func(d *Draft, _ uuid.UUID) { d.Content = "Hello" }, []string{"content:invalid"}},
		{"another channel than the template's", shipped, func(d *Draft, _ uuid.UUID) { d.Channel = ChannelPush }, []string{"channel:invalid"}},
		{"variables without a template", shipped, func(d *Draft, _ uuid.UUID) { d.TemplateID, d.Channel, d.Content = nil, ChannelSMS, "Hi" }, []string{"variables:invalid"}},
		{"rendered longer than the channel allows", long, func(d *Draft, _ uuid.UUID) { d.Variables["name"] = strings.Repeat("b", 20) }, []string{"content:too_long"}},
		{"its own channel, named", shipped, func(d *Draft, _ uuid.UUID) { d.Channel = ChannelSMS }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, id := withTemplate(tt.template)
			d := Draft{To: "+905551234567", Priority: PriorityNormal, TemplateID: &id, Variables: map[string]string{"name": "Ayşe", "order": "42"}}
			tt.edit(&d, id)
			_, _, err := newTestService(store).Create(t.Context(), d, "")
			if got := fieldCodes(t, err); strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("Create = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestServiceCreateBatchReadsEachTemplateOnce: a batch of a thousand notifications from one template reads it
// once; an item it cannot render is rejected, and the others are accepted.
func TestServiceCreateBatchReadsEachTemplateOnce(t *testing.T) {
	store, id := withTemplate(shipped)
	drafts := []Draft{
		{To: "+905551234567", Priority: PriorityNormal, TemplateID: &id, Variables: map[string]string{"name": "Ayşe", "order": "1"}},
		{To: "+905551234568", Priority: PriorityNormal, TemplateID: &id, Variables: map[string]string{"name": "Ali"}},
		{To: "+905551234569", Priority: PriorityNormal, TemplateID: &id, Variables: map[string]string{"name": "Can", "order": "3"}},
	}
	b, _, err := newTestService(store).CreateBatch(t.Context(), drafts, "")
	if err != nil {
		t.Fatal(err)
	}
	if store.templateReads != 1 {
		t.Errorf("the template was read %d times, want once", store.templateReads)
	}
	items := store.batches[0].Items
	if b.Accepted != 2 || len(b.Rejections) != 1 || b.Rejections[0].Index != 1 || items[1].Notification.Content != "Hi Can, your order 3 has shipped." {
		t.Errorf("batch = %+v, items %+v", b, items)
	}
}
