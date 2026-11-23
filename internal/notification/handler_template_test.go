package notification

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"uuid"
)

var storedTemplate = Template{
	ID: uuid.MustParse("01a20a40-5c1e-7b3a-9d4e-2f1a3b4c5d6e"), Name: "order-shipped", Channel: ChannelSMS,
	Body:      "Hi {{.name}}, your order {{.order}} has shipped.",
	CreatedAt: time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC),
}

const templateBody = `{"name":"order-shipped","channel":"sms","body":"Hi {{.name}}, your order {{.order}} has shipped."}`

func TestCreateTemplate(t *testing.T) {
	svc := &fakeService{template: storedTemplate}
	rec := serve(t, svc, http.MethodPost, "/v1/templates", templateBody, nil)
	if rec.Code != http.StatusCreated || rec.Header().Get("Location") != "/v1/templates/"+storedTemplate.ID.String() {
		t.Fatalf("status %d, Location %q: %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if d := svc.templateDrafts[0]; d.Name != "order-shipped" || d.Channel != ChannelSMS || d.Body != storedTemplate.Body {
		t.Errorf("draft = %+v", d)
	}
	b := body(t, rec)
	if fmt.Sprint(b["variables"]) != "[name order]" || b["createdAt"] != "2026-11-24T09:00:00Z" {
		t.Errorf("answer = %v", b)
	}
}

func TestTemplateErrors(t *testing.T) {
	invalid := &ValidationError{Fields: []FieldError{{Field: "body", Code: CodeInvalid, Message: "may hold only text and {{.name}} placeholders"}}}
	id := "/v1/templates/" + storedTemplate.ID.String()
	tests := []struct {
		name, method, path, body string
		err                      error
		status                   int
		code                     string
	}{
		{"a name taken", http.MethodPost, "/v1/templates", templateBody, ErrTemplateNameTaken, 409, "template_name_taken"},
		{"an invalid body", http.MethodPost, "/v1/templates", templateBody, invalid, 422, "validation_failed"},
		{"an unknown member", http.MethodPost, "/v1/templates", `{"name":"x","channel":"sms","body":"x","vars":[]}`, nil, 400, "unknown_field"},
		{"read an unknown one", http.MethodGet, id, "", ErrNotFound, 404, "not_found"},
		{"read with an ID that is not one", http.MethodGet, "/v1/templates/42", "", nil, 404, "not_found"},
		{"replace an unknown one", http.MethodPut, id, templateBody, ErrNotFound, 404, "not_found"},
		{"rename to a name taken", http.MethodPut, id, templateBody, ErrTemplateNameTaken, 409, "template_name_taken"},
		{"delete an unknown one", http.MethodDelete, id, "", ErrNotFound, 404, "not_found"},
		{"list with a bad limit", http.MethodGet, "/v1/templates?limit=0", "", nil, 400, "invalid_query"},
		{"list with a filter it has not", http.MethodGet, "/v1/templates?channel=sms", "", nil, 400, "invalid_query"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, &fakeService{err: tt.err}, tt.method, tt.path, tt.body, nil)
			if b := body(t, rec); rec.Code != tt.status || b["code"] != tt.code {
				t.Errorf("got %d %v, want %d %s", rec.Code, b["code"], tt.status, tt.code)
			}
		})
	}
}

func TestReadReplaceAndDeleteATemplate(t *testing.T) {
	path := "/v1/templates/" + storedTemplate.ID.String()
	svc := &fakeService{template: storedTemplate}
	if rec := serve(t, svc, http.MethodGet, path, "", nil); rec.Code != http.StatusOK || body(t, rec)["name"] != "order-shipped" {
		t.Errorf("GET = %d %s", rec.Code, rec.Body)
	}
	if rec := serve(t, svc, http.MethodPut, path, templateBody, nil); rec.Code != http.StatusOK || svc.templateIDs[1] != storedTemplate.ID {
		t.Errorf("PUT = %d, replaced %v", rec.Code, svc.templateIDs)
	}
	rec := serve(t, svc, http.MethodDelete, path, "", nil)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 || svc.templateIDs[2] != storedTemplate.ID {
		t.Errorf("DELETE = %d %q", rec.Code, rec.Body)
	}
}

func TestListTemplates(t *testing.T) {
	next := Cursor{CreatedAt: storedTemplate.CreatedAt, ID: storedTemplate.ID}
	svc := &fakeService{templatePage: TemplatePage{Items: []Template{storedTemplate}, Next: &next}}
	rec := serve(t, svc, http.MethodGet, "/v1/templates?limit=1&cursor="+encodeCursor(next), "", nil)
	b := body(t, rec)
	if items, _ := b["items"].([]any); rec.Code != http.StatusOK || len(items) != 1 || b["nextCursor"] != encodeCursor(next) {
		t.Errorf("list = %d %v", rec.Code, b)
	}
	if svc.limits[0] != 1 || svc.cursors[0] == nil || *svc.cursors[0] != next {
		t.Errorf("asked for limit %v after %v", svc.limits, svc.cursors)
	}
}
