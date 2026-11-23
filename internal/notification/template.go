package notification

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"text/template"
	"text/template/parse"
	"time"
	"uuid"
)

// maxTemplateName bounds a template's name, which people type and read.
const maxTemplateName = 64

// templateName is lower-case words joined by hyphens, such as order-shipped.
var templateName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ErrTemplateNameTaken reports a template name another template already has.
var ErrTemplateNameTaken = errors.New("another template has this name")

// Template is a stored body with named placeholders, such as "Hi {{.name}}", for one channel. A notification created
// from it is rendered then, and keeps the rendered content (ADR 0032).
type Template struct {
	ID        uuid.UUID
	Name      string
	Channel   Channel
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TemplateDraft is a template as a client writes it, to create or replace one.
type TemplateDraft struct {
	Name    string
	Channel Channel
	Body    string
}

// TemplatePage is one page of templates, and the cursor of the next page, or nil on the last one.
type TemplatePage struct {
	Items []Template
	Next  *Cursor
}

// MissingVariablesError reports the placeholders a render had no value for.
type MissingVariablesError struct {
	Names []string
}

func (e *MissingVariablesError) Error() string {
	return "the template needs " + strings.Join(e.Names, ", ")
}

// Validate checks every field of d and returns a *ValidationError listing each one that breaks a rule, or nil. The
// body follows the content rules of d's channel, and may hold only text and {{.name}} placeholders.
func (d TemplateDraft) Validate() error {
	var fields []FieldError
	for _, fe := range []*FieldError{checkTemplateName(d.Name), checkChannel(d.Channel), checkBody(d.Channel, d.Body)} {
		if fe != nil {
			fields = append(fields, *fe)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return &ValidationError{Fields: fields}
}

func checkTemplateName(name string) *FieldError {
	switch {
	case name == "":
		return &FieldError{"name", CodeRequired, "is required"}
	case len(name) > maxTemplateName:
		return &FieldError{"name", CodeTooLong, fmt.Sprintf("must be at most %d characters", maxTemplateName)}
	case !templateName.MatchString(name):
		return &FieldError{"name", CodeInvalid, "must be lower-case letters and digits in words joined by hyphens, such as order-shipped"}
	}
	return nil
}

func checkBody(c Channel, body string) *FieldError {
	if fe := checkContent(c, body); fe != nil {
		fe.Field = "body"
		return fe
	}
	if _, _, err := compile(body); err != nil {
		return &FieldError{"body", CodeInvalid, err.Error()}
	}
	return nil
}

// compile parses a body and returns it with its placeholders' names, each once, in order of first use. A body
// holding anything but text and {{.name}} placeholders is refused: loops, conditions, functions and variables could
// make a render slow or surprising, and a placeholder is all a notification needs.
func compile(body string) (*template.Template, []string, error) {
	tmpl, err := template.New("body").Option("missingkey=error").Parse(body)
	if err != nil {
		return nil, nil, fmt.Errorf("is not a valid template: %w", err)
	}
	if len(tmpl.Templates()) > 1 {
		return nil, nil, errors.New("may not define other templates")
	}
	var names []string
	for _, node := range tmpl.Root.Nodes {
		switch n := node.(type) {
		case *parse.TextNode:
			continue
		case *parse.ActionNode:
			if name, ok := placeholder(n); ok {
				if !slices.Contains(names, name) {
					names = append(names, name)
				}
				continue
			}
		}
		return nil, nil, errors.New("may hold only text and {{.name}} placeholders")
	}
	return tmpl, names, nil
}

// placeholder returns the name of an action that is exactly {{.name}}: one field of one identifier, with no
// variable, function or pipe.
func placeholder(n *parse.ActionNode) (string, bool) {
	if len(n.Pipe.Decl) != 0 || len(n.Pipe.Cmds) != 1 || len(n.Pipe.Cmds[0].Args) != 1 {
		return "", false
	}
	f, ok := n.Pipe.Cmds[0].Args[0].(*parse.FieldNode)
	if !ok || len(f.Ident) != 1 {
		return "", false
	}
	return f.Ident[0], true
}

// Variables returns the names of t's placeholders, each once, in order of first use.
func (t Template) Variables() []string {
	_, names, _ := compile(t.Body) // a stored body was validated
	return names
}

// Render fills t's placeholders with variables and returns the content. It refuses variables that lack a
// placeholder's value with a *MissingVariablesError naming every one; values without a placeholder are ignored.
// A value is inserted as it is, never rendered again.
func (t Template) Render(variables map[string]string) (string, error) {
	tmpl, names, err := compile(t.Body)
	if err != nil {
		return "", fmt.Errorf("compile template %s: %w", t.Name, err)
	}
	var missing []string
	for _, name := range names {
		if _, ok := variables[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return "", &MissingVariablesError{Names: missing}
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, variables); err != nil {
		return "", fmt.Errorf("render template %s: %w", t.Name, err)
	}
	return b.String(), nil
}
