package notification

import (
	"context"
	"uuid"
)

// CreateTemplate validates d and stores it as a new template. An invalid draft is refused with a *ValidationError;
// a name another template has, with ErrTemplateNameTaken.
func (s *Service) CreateTemplate(ctx context.Context, d TemplateDraft) (Template, error) {
	if err := d.Validate(); err != nil {
		return Template{}, err
	}
	t, err := s.store.CreateTemplate(ctx, Template{ID: uuid.NewV7(), Name: d.Name, Channel: d.Channel, Body: d.Body})
	if err != nil {
		return Template{}, err
	}
	s.log.InfoContext(ctx, "template created", "template_id", t.ID, "name", t.Name, "channel", t.Channel)
	return t, nil
}

// Template returns the template with the given ID, or ErrNotFound.
func (s *Service) Template(ctx context.Context, id uuid.UUID) (Template, error) {
	return s.store.Template(ctx, id)
}

// ListTemplates returns a page of templates, newest first, after the cursor when one is given.
func (s *Service) ListTemplates(ctx context.Context, after *Cursor, limit int) (TemplatePage, error) {
	// One row more than the page tells whether another page follows, as List does.
	rows, err := s.store.ListTemplates(ctx, after, limit+1)
	if err != nil {
		return TemplatePage{}, err
	}
	if len(rows) <= limit {
		return TemplatePage{Items: rows}, nil
	}
	last := rows[limit-1]
	return TemplatePage{Items: rows[:limit], Next: &Cursor{CreatedAt: last.CreatedAt, ID: last.ID}}, nil
}

// UpdateTemplate replaces the template with the given ID by d. Notifications already created from it keep the
// content they were rendered with (ADR 0032).
func (s *Service) UpdateTemplate(ctx context.Context, id uuid.UUID, d TemplateDraft) (Template, error) {
	if err := d.Validate(); err != nil {
		return Template{}, err
	}
	t, err := s.store.UpdateTemplate(ctx, Template{ID: id, Name: d.Name, Channel: d.Channel, Body: d.Body})
	if err != nil {
		return Template{}, err
	}
	s.log.InfoContext(ctx, "template updated", "template_id", t.ID, "name", t.Name, "channel", t.Channel)
	return t, nil
}

// DeleteTemplate deletes the template with the given ID, or returns ErrNotFound.
func (s *Service) DeleteTemplate(ctx context.Context, id uuid.UUID) error {
	if err := s.store.DeleteTemplate(ctx, id); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "template deleted", "template_id", id)
	return nil
}
