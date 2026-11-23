package notification

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestTemplateDraftValidate(t *testing.T) {
	valid := TemplateDraft{Name: "order-shipped", Channel: ChannelSMS, Body: "Hi {{.name}}, your order {{.order}} has shipped."}
	tests := []struct {
		name string
		edit func(d *TemplateDraft)
		want []string
	}{
		{"valid", func(*TemplateDraft) {}, nil},
		{"a comment is for the reader only", func(d *TemplateDraft) { d.Body = "{{/* sent after dispatch */}}Hi {{.name}}" }, nil},
		{"trim markers", func(d *TemplateDraft) { d.Body = "Hi {{- .name -}} !" }, nil},

		{"no name", func(d *TemplateDraft) { d.Name = "" }, []string{"name:required"}},
		{"a name with capitals", func(d *TemplateDraft) { d.Name = "Order-Shipped" }, []string{"name:invalid"}},
		{"a name with spaces", func(d *TemplateDraft) { d.Name = "order shipped" }, []string{"name:invalid"}},
		{"a name too long", func(d *TemplateDraft) { d.Name = strings.Repeat("a", maxTemplateName+1) }, []string{"name:too_long"}},
		{"an unknown channel", func(d *TemplateDraft) { d.Channel = "fax" }, []string{"channel:invalid"}},
		{"no body", func(d *TemplateDraft) { d.Body = " " }, []string{"body:required"}},
		{"a body over its channel's limit", func(d *TemplateDraft) { d.Body = strings.Repeat("a", maxContent[ChannelSMS]+1) }, []string{"body:too_long"}},
		{"a body that does not parse", func(d *TemplateDraft) { d.Body = "Hi {{.name" }, []string{"body:invalid"}},

		// Only text and {{.name}}: anything else could make rendering slow or surprising.
		{"a loop", func(d *TemplateDraft) { d.Body = "{{range 200000000}}{{end}}" }, []string{"body:invalid"}},
		{"a condition", func(d *TemplateDraft) { d.Body = "{{if .vip}}Dear customer{{end}}" }, []string{"body:invalid"}},
		{"a function", func(d *TemplateDraft) { d.Body = `{{printf "%s" .name}}` }, []string{"body:invalid"}},
		{"a pipeline", func(d *TemplateDraft) { d.Body = "{{.name | len}}" }, []string{"body:invalid"}},
		{"a nested field", func(d *TemplateDraft) { d.Body = "{{.user.name}}" }, []string{"body:invalid"}},
		{"the whole data", func(d *TemplateDraft) { d.Body = "{{.}}" }, []string{"body:invalid"}},
		{"a variable", func(d *TemplateDraft) { d.Body = "{{$x := .name}}{{$x}}" }, []string{"body:invalid"}},
		{"another template", func(d *TemplateDraft) { d.Body = `{{define "x"}}y{{end}}Hi` }, []string{"body:invalid"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := valid
			tt.edit(&d)
			if got := fieldCodes(t, d.Validate()); !slices.Equal(got, tt.want) {
				t.Errorf("Validate = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTemplateVariables(t *testing.T) {
	tmpl := Template{Body: "Hi {{.name}}, order {{.order}} for {{.name}}."}
	if got := tmpl.Variables(); !slices.Equal(got, []string{"name", "order"}) {
		t.Errorf("Variables = %v, want [name order], each once in order of first use", got)
	}
}

func TestTemplateRender(t *testing.T) {
	tmpl := Template{Name: "order-shipped", Channel: ChannelSMS, Body: "Hi {{.name}}, your order {{.order}} has shipped."}

	got, err := tmpl.Render(map[string]string{"name": "Ayşe", "order": "42", "unused": "x"})
	if err != nil || got != "Hi Ayşe, your order 42 has shipped." {
		t.Errorf("Render = %q, %v", got, err)
	}

	// A value is inserted as it is, never rendered again.
	if got, _ := tmpl.Render(map[string]string{"name": "{{.order}}", "order": "42"}); got != "Hi {{.order}}, your order 42 has shipped." {
		t.Errorf("a value was rendered again: %q", got)
	}

	_, err = tmpl.Render(map[string]string{"order": "42", "nam": "Ayşe"})
	missing, ok := errors.AsType[*MissingVariablesError](err)
	if !ok || !slices.Equal(missing.Names, []string{"name"}) {
		t.Errorf("Render without name = %v, want MissingVariablesError naming name", err)
	}
	if _, err := tmpl.Render(nil); err == nil || !strings.Contains(err.Error(), "name, order") {
		t.Errorf("Render with nothing = %v, want every missing variable named", err)
	}
}
