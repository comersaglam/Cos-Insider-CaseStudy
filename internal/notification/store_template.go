package notification

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification/internal/db"
)

// uniqueViolation is PostgreSQL's error code for a duplicate key.
const uniqueViolation = "23505"

// CreateTemplate stores t, or returns ErrTemplateNameTaken when another template has its name.
func (s *Store) CreateTemplate(ctx context.Context, t Template) (Template, error) {
	row, err := db.New(s.pool).InsertTemplate(ctx, db.InsertTemplateParams{
		ID: t.ID, Name: t.Name, Channel: string(t.Channel), Body: t.Body,
	})
	if err != nil {
		return Template{}, templateError("insert template", err)
	}
	return templateFromRow(row), nil
}

// Template returns the template with the given ID, or ErrNotFound.
func (s *Store) Template(ctx context.Context, id uuid.UUID) (Template, error) {
	row, err := db.New(s.pool).GetTemplate(ctx, id)
	if err != nil {
		return Template{}, templateError("read template", err)
	}
	return templateFromRow(row), nil
}

// ListTemplates returns up to limit templates, newest first, after the cursor when one is given.
func (s *Store) ListTemplates(ctx context.Context, after *Cursor, limit int) ([]Template, error) {
	params := db.ListTemplatesParams{PageSize: limit}
	if after != nil {
		params.AfterCreatedAt, params.AfterID = &after.CreatedAt, &after.ID
	}
	rows, err := db.New(s.pool).ListTemplates(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	out := make([]Template, len(rows))
	for i, r := range rows {
		out[i] = templateFromRow(r)
	}
	return out, nil
}

// UpdateTemplate replaces the name, channel and body of the template with t's ID. It returns ErrNotFound when there
// is none, and ErrTemplateNameTaken when another template has the name.
func (s *Store) UpdateTemplate(ctx context.Context, t Template) (Template, error) {
	row, err := db.New(s.pool).UpdateTemplate(ctx, db.UpdateTemplateParams{
		ID: t.ID, Name: t.Name, Channel: string(t.Channel), Body: t.Body,
	})
	if err != nil {
		return Template{}, templateError("update template", err)
	}
	return templateFromRow(row), nil
}

// DeleteTemplate deletes the template with the given ID, or returns ErrNotFound. Notifications rendered from it keep
// their content and its ID (ADR 0032).
func (s *Store) DeleteTemplate(ctx context.Context, id uuid.UUID) error {
	n, err := db.New(s.pool).DeleteTemplate(ctx, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// templateError turns a missing row into ErrNotFound and a duplicate name into ErrTemplateNameTaken.
func templateError(what string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "templates_name" {
		return ErrTemplateNameTaken
	}
	return fmt.Errorf("%s: %w", what, err)
}

func templateFromRow(r db.Template) Template {
	return Template{
		ID: r.ID, Name: r.Name, Channel: Channel(r.Channel), Body: r.Body,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}
