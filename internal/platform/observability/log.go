package observability

import (
	"context"
	"io"
	"log/slog"
)

// NewLogger returns a logger that writes one JSON object per line to w, drops records below level, writes times
// in UTC, and adds the correlation ID of the context passed to the *Context methods (InfoContext, ErrorContext,
// ...).
func NewLogger(w io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(contextHandler{slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: utcTime})})
}

// utcTime writes the record's time in UTC, the one clock of the service, whatever the host's time zone.
func utcTime(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey && len(groups) == 0 {
		a.Value = slog.TimeValue(a.Value.Time().UTC())
	}
	return a
}

// contextHandler adds values carried by the context to every record. Wrapping the handler, rather than calling
// logger.With in each request, keeps the correlation ID on lines logged by code that only receives a context.
type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := CorrelationID(ctx); id != "" {
		r.AddAttrs(slog.String("correlation_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs and WithGroup re-wrap the derived handler; without them a logger made with With would lose
// contextHandler and its correlation IDs.
func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
