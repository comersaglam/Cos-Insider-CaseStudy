package httpx

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// Correlation gives every request a correlation ID: the client's X-Correlation-ID when it is valid, a new one
// otherwise. The ID is put in the request's context, where the logger finds it, and sent back in the response.
//
// The middleware chain is Correlation, then AccessLog, then Recover, then the router: the ID is set before
// anything logs, and a recovered panic is logged by AccessLog with its 500 status.
func Correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(observability.CorrelationHeader)
		if !observability.ValidCorrelationID(id) {
			id = observability.NewCorrelationID()
		}
		w.Header().Set(observability.CorrelationHeader, id)
		next.ServeHTTP(w, r.WithContext(observability.WithCorrelationID(r.Context(), id)))
	})
}

// AccessLog logs one line per request: method, path, status, response size and duration. Server errors are
// logged at error level, everything else at info. The query string is left out, since it may hold personal data.
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &recorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			status := rec.status
			if status == 0 {
				status = http.StatusOK // net/http sends 200 when a handler writes nothing
			}
			level := slog.LevelInfo
			if status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			log.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("bytes", rec.bytes),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			)
		})
	}
}

// Recover turns a panic in a handler into a logged error and a 500 problem, so one faulty request does not stop
// the server. http.ErrAbortHandler is passed on: net/http uses that panic to abort a response on purpose.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &recorder{ResponseWriter: w}
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				log.ErrorContext(r.Context(), "panic while serving a request",
					"panic", fmt.Sprint(v), "stack", string(debug.Stack()))
				if rec.status == 0 {
					WriteProblem(rec, r, NewProblem(http.StatusInternalServerError, codeInternal,
						"the request could not be completed"))
				}
			}()
			next.ServeHTTP(rec, r)
		})
	}
}

// recorder remembers the status and size of a response as it is written.
type recorder struct {
	http.ResponseWriter
	status int // 0 until the header is written
	bytes  int
}

func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap gives http.ResponseController the underlying writer, so flushing and deadlines keep working through
// the recorder.
func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
