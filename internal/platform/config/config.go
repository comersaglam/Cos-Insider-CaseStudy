package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// secretMask replaces a secret's value wherever the configuration is shown.
const secretMask = "***"

// Reader reads settings from environment variables. It never stops at the first problem: each method records
// what is wrong and returns a usable value, and Err reports every problem at once, so a misconfigured process
// lists all its mistakes in one start.
//
// Numbers and durations must be greater than zero, since every setting of either kind (a pool size, a timeout,
// a rate) is meaningless at zero or below. A variable set to the empty string counts as unset.
type Reader struct {
	lookup func(key string) (string, bool)
	errs   []error
	shown  []slog.Attr
}

// NewReader returns a Reader that looks variables up with lookup, normally os.LookupEnv.
func NewReader(lookup func(key string) (string, bool)) *Reader {
	return &Reader{lookup: lookup}
}

// String returns the variable key, or fallback when it is unset.
func (r *Reader) String(key, fallback string) string {
	v, ok := r.get(key)
	if !ok {
		v = fallback
	}
	r.show(key, v)
	return v
}

// Required returns the variable key and records a problem when it is unset.
func (r *Reader) Required(key string) string {
	v, ok := r.get(key)
	if !ok {
		r.fail(key, "is required")
	}
	r.show(key, v)
	return v
}

// Secret is Required for a value that must not be shown: it appears as "***" when the configuration is logged.
func (r *Reader) Secret(key string) string {
	v, ok := r.get(key)
	if !ok {
		r.fail(key, "is required")
	}
	r.show(key, secretMask)
	return v
}

// Int returns the variable key as a whole number greater than zero, or fallback when it is unset.
func (r *Reader) Int(key string, fallback int) int {
	raw, ok := r.get(key)
	if !ok {
		r.show(key, strconv.Itoa(fallback))
		return fallback
	}
	r.show(key, raw)
	n, err := strconv.Atoi(raw)
	switch {
	case err != nil:
		r.fail(key, fmt.Sprintf("%q is not a whole number", raw))
		return fallback
	case n <= 0:
		r.fail(key, fmt.Sprintf("%d must be greater than zero", n))
		return fallback
	}
	return n
}

// Duration returns the variable key as a duration greater than zero, written with a unit as time.ParseDuration
// reads it ("250ms", "10s", "15m"), or fallback when it is unset.
func (r *Reader) Duration(key string, fallback time.Duration) time.Duration {
	raw, ok := r.get(key)
	if !ok {
		r.show(key, fallback.String())
		return fallback
	}
	r.show(key, raw)
	d, err := time.ParseDuration(raw)
	switch {
	case err != nil:
		r.fail(key, fmt.Sprintf("%q is not a duration with a unit, such as 10s or 250ms", raw))
		return fallback
	case d <= 0:
		r.fail(key, fmt.Sprintf("%s must be greater than zero", d))
		return fallback
	}
	return d
}

// LogLevel returns the variable key as a log level (debug, info, warn or error), or fallback when it is unset.
func (r *Reader) LogLevel(key string, fallback slog.Level) slog.Level {
	raw, ok := r.get(key)
	if !ok {
		r.show(key, fallback.String())
		return fallback
	}
	r.show(key, raw)
	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		r.fail(key, fmt.Sprintf("%q is not one of debug, info, warn, error", raw))
		return fallback
	}
	return level
}

// Err returns every problem recorded so far, joined, or nil when there is none.
func (r *Reader) Err() error {
	return errors.Join(r.errs...)
}

// LogValue shows every setting read so far, in the order read, with secrets masked. It lets the configuration
// be logged as one value: logger.Info("config loaded", "config", reader).
func (r *Reader) LogValue() slog.Value {
	return slog.GroupValue(r.shown...)
}

func (r *Reader) get(key string) (string, bool) {
	v, ok := r.lookup(key)
	return v, ok && v != ""
}

func (r *Reader) show(key, value string) {
	r.shown = append(r.shown, slog.String(key, value))
}

func (r *Reader) fail(key, problem string) {
	r.errs = append(r.errs, fmt.Errorf("%s %s", key, problem))
}
