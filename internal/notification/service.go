package notification

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"go.opentelemetry.io/otel/propagation"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// store is what the service needs from storage; *Store satisfies it.
type store interface {
	Create(ctx context.Context, n Notification, idem IdempotencyKey) (Notification, bool, error)
	Get(ctx context.Context, id uuid.UUID) (Notification, error)
	CreateBatch(ctx context.Context, b NewBatch, idem IdempotencyKey) (Batch, bool, error)
	GetBatch(ctx context.Context, id uuid.UUID) (Batch, error)
	List(ctx context.Context, f ListFilter, after *Cursor, limit int) ([]Notification, error)
	Cancel(ctx context.Context, id uuid.UUID) (Notification, error)
	Retry(ctx context.Context, id uuid.UUID) (Notification, error)
	CountFailed(ctx context.Context, f RetryFilter) (int, error)
	RequeueFailed(ctx context.Context, f RetryFilter, maxRows int) (int, error)
	Summary(ctx context.Context, window time.Duration) (Summary, error)
	CreateTemplate(ctx context.Context, t Template) (Template, error)
	Template(ctx context.Context, id uuid.UUID) (Template, error)
	ListTemplates(ctx context.Context, after *Cursor, limit int) ([]Template, error)
	UpdateTemplate(ctx context.Context, t Template) (Template, error)
	DeleteTemplate(ctx context.Context, id uuid.UUID) error
}

// Service holds the notification use cases. It knows the rules; it neither reads requests nor writes SQL.
type Service struct {
	store store
	log   *slog.Logger
}

// NewService returns a service over the given storage.
func NewService(s store, log *slog.Logger) *Service {
	return &Service{store: s, log: log}
}

// Create validates d and stores it as a queued notification, with its outbox row, so that it is sent. An
// idempotency key, when not empty, makes a repeated request return the first notification with created = false;
// the same key with a different request is refused with ErrIdempotencyKeyReused. An invalid draft is refused with
// a *ValidationError and nothing is stored.
func (s *Service) Create(ctx context.Context, d Draft, idempotencyKey string) (n Notification, created bool, err error) {
	// The fingerprint is of the request as written, before a template fills it in.
	fingerprint := d.Fingerprint()
	now := time.Now()
	if d, err = s.render(ctx, d, nil); err != nil {
		return Notification{}, false, err
	}
	if err := d.Validate(now); err != nil {
		return Notification{}, false, err
	}

	correlationID := correlationIDOf(ctx)
	var idem IdempotencyKey
	if idempotencyKey != "" {
		idem = IdempotencyKey{Key: idempotencyKey, Fingerprint: fingerprint}
	}

	n, created, err = s.store.Create(ctx, Notification{
		ID:            uuid.NewV7(),
		To:            d.To,
		Channel:       d.Channel,
		Content:       d.Content,
		Priority:      d.Priority,
		Status:        d.Status(now),
		ScheduledAt:   d.ScheduledAt,
		TemplateID:    d.TemplateID,
		CorrelationID: correlationID,
		TraceParent:   traceParentOf(ctx),
	}, idem)
	if err != nil {
		return Notification{}, false, err
	}
	s.log.InfoContext(ctx, "notification accepted",
		"notification_id", n.ID, "channel", n.Channel, "priority", n.Priority,
		"to", observability.MaskRecipient(n.To), "replayed", !created)
	return n, created, nil
}

// Get returns the notification with the given ID, or ErrNotFound.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Notification, error) {
	return s.store.Get(ctx, id)
}

// CreateBatch accepts the valid drafts of a batch as queued notifications and records the invalid ones with their
// reasons; one invalid item does not refuse the others. A batch with no item, or more than MaxBatchSize, is refused
// with a *ValidationError, and one in which no item is valid with a *BatchError; neither is stored. An idempotency
// key, when not empty, covers the whole batch as Create's covers one notification.
func (s *Service) CreateBatch(ctx context.Context, drafts []Draft, idempotencyKey string) (b Batch, created bool, err error) {
	switch {
	case len(drafts) == 0:
		return Batch{}, false, &ValidationError{Fields: []FieldError{
			{Field: "notifications", Code: CodeRequired, Message: "must hold at least one notification"}}}
	case len(drafts) > MaxBatchSize:
		return Batch{}, false, &ValidationError{Fields: []FieldError{
			{Field: "notifications", Code: CodeTooLong, Message: fmt.Sprintf("must hold at most %d notifications", MaxBatchSize)}}}
	}

	correlationID, traceParent := correlationIDOf(ctx), traceParentOf(ctx)
	nb := NewBatch{ID: uuid.NewV7(), Total: len(drafts), CorrelationID: correlationID}
	now := time.Now()
	// A batch reads each template it names once, however many items use it.
	templates := map[uuid.UUID]Template{}
	for i, d := range drafts {
		d, err := s.render(ctx, d, templates)
		if err == nil {
			err = d.Validate(now)
		}
		if err != nil {
			verr, ok := errors.AsType[*ValidationError](err)
			if !ok {
				return Batch{}, false, err
			}
			nb.Rejections = append(nb.Rejections, Rejection{Index: i, Fields: verr.Fields})
			continue
		}
		nb.Items = append(nb.Items, NewBatchItem{Index: i, Notification: Notification{
			ID: uuid.NewV7(), To: d.To, Channel: d.Channel, Content: d.Content, Priority: d.Priority,
			Status: d.Status(now), ScheduledAt: d.ScheduledAt, TemplateID: d.TemplateID, CorrelationID: correlationID,
			TraceParent: traceParent,
		}})
	}
	if len(nb.Items) == 0 {
		return Batch{}, false, &BatchError{Rejections: nb.Rejections}
	}

	var idem IdempotencyKey
	if idempotencyKey != "" {
		idem = IdempotencyKey{Key: idempotencyKey, Fingerprint: BatchFingerprint(drafts)}
	}
	b, created, err = s.store.CreateBatch(ctx, nb, idem)
	if err != nil {
		return Batch{}, false, err
	}
	s.log.InfoContext(ctx, "batch accepted",
		"batch_id", b.ID, "total", b.Total, "accepted", b.Accepted, "rejected", len(b.Rejections), "replayed", !created)
	return b, created, nil
}

// GetBatch returns the batch with the given ID, with its accepted notifications counted by status, or ErrNotFound.
func (s *Service) GetBatch(ctx context.Context, id uuid.UUID) (Batch, error) {
	return s.store.GetBatch(ctx, id)
}

// traceParentOf returns the W3C traceparent of the span ctx carries, or "" outside a trace (ADR 0034).
func traceParentOf(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	observability.Propagator().Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// correlationIDOf returns ctx's correlation ID, or a new one for callers outside HTTP, whose context carries none;
// the column is NOT NULL, and the ID is what joins the logs.
func correlationIDOf(ctx context.Context) string {
	if id := observability.CorrelationID(ctx); id != "" {
		return id
	}
	return observability.NewCorrelationID()
}

// Summary returns what waits now and how sending went over the last SummaryWindow (ADR 0026).
func (s *Service) Summary(ctx context.Context) (Summary, error) {
	return s.store.Summary(ctx, SummaryWindow)
}

// render fills d's channel and content from the template it names, when it names one (ADR 0032). A template that
// does not exist or lacks a variable, and content written beside a template, are refused with a *ValidationError;
// the rendered content is checked afterwards, as written content is. templates, when not nil, keeps the templates
// read, so a batch reads each once.
func (s *Service) render(ctx context.Context, d Draft, templates map[uuid.UUID]Template) (Draft, error) {
	refuse := func(field string, code Code, message string) (Draft, error) {
		return Draft{}, &ValidationError{Fields: []FieldError{{Field: field, Code: code, Message: message}}}
	}
	if d.TemplateID == nil {
		if len(d.Variables) > 0 {
			return refuse("variables", CodeInvalid, "are given only with a templateId")
		}
		return d, nil
	}
	if d.Content != "" {
		return refuse("content", CodeInvalid, "must be left out with a templateId: the template writes it")
	}

	t, ok := templates[*d.TemplateID]
	if !ok {
		var err error
		t, err = s.store.Template(ctx, *d.TemplateID)
		if errors.Is(err, ErrNotFound) {
			return refuse("templateId", CodeInvalid, "no template has this ID")
		}
		if err != nil {
			return Draft{}, err
		}
		if templates != nil {
			templates[t.ID] = t
		}
	}
	if d.Channel != "" && d.Channel != t.Channel {
		return refuse("channel", CodeInvalid, "must be the template's channel, "+string(t.Channel)+", or be left out")
	}
	content, err := t.Render(d.Variables)
	if missing, ok := errors.AsType[*MissingVariablesError](err); ok {
		return refuse("variables", CodeRequired, "the template needs "+strings.Join(missing.Names, ", "))
	}
	if err != nil {
		return Draft{}, err
	}
	d.Channel, d.Content = t.Channel, content
	return d, nil
}
