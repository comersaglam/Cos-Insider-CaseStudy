package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// env returns a lookup function over a fixed set of variables, so tests neither read nor change the process
// environment and can run in parallel.
func env(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func TestReaderReadsSetValues(t *testing.T) {
	r := NewReader(env(map[string]string{
		"ADDR":        ":9090",
		"DATABASE":    "postgres://db/n",
		"PASSWORD":    "s3cret",
		"CONCURRENCY": "64",
		"TIMEOUT":     "250ms",
		"LOG_LEVEL":   "debug",
	}))

	if got := r.String("ADDR", ":8080"); got != ":9090" {
		t.Errorf("String = %q, want :9090", got)
	}
	if got := r.Required("DATABASE"); got != "postgres://db/n" {
		t.Errorf("Required = %q", got)
	}
	if got := r.Secret("PASSWORD"); got != "s3cret" {
		t.Errorf("Secret = %q, want s3cret", got)
	}
	if got := r.Int("CONCURRENCY", 32); got != 64 {
		t.Errorf("Int = %d, want 64", got)
	}
	if got := r.Duration("TIMEOUT", time.Second); got != 250*time.Millisecond {
		t.Errorf("Duration = %v, want 250ms", got)
	}
	if got := r.LogLevel("LOG_LEVEL", slog.LevelInfo); got != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want DEBUG", got)
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err = %v, want nil", err)
	}
}

// TestReaderUsesFallbackWhenUnsetOrEmpty treats an empty variable as unset: Compose passes an empty string for a
// variable that is referenced but not defined.
func TestReaderUsesFallbackWhenUnsetOrEmpty(t *testing.T) {
	r := NewReader(env(map[string]string{"ADDR": "", "TIMEOUT": ""}))

	if got := r.String("ADDR", ":8080"); got != ":8080" {
		t.Errorf("String = %q, want :8080", got)
	}
	if got := r.Int("CONCURRENCY", 32); got != 32 {
		t.Errorf("Int = %d, want 32", got)
	}
	if got := r.Duration("TIMEOUT", time.Second); got != time.Second {
		t.Errorf("Duration = %v, want 1s", got)
	}
	if got := r.LogLevel("LOG_LEVEL", slog.LevelWarn); got != slog.LevelWarn {
		t.Errorf("LogLevel = %v, want WARN", got)
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err = %v, want nil", err)
	}
}

func TestReaderReportsEveryProblemAtOnce(t *testing.T) {
	r := NewReader(env(map[string]string{
		"ZERO":        "0",
		"WORDS":       "many",
		"NEGATIVE":    "-5s",
		"NO_UNIT":     "30",
		"LOG_LEVEL":   "loud",
		"EMPTY_TOKEN": "",
	}))
	r.Required("DATABASE")
	r.Secret("EMPTY_TOKEN")
	r.Int("ZERO", 1)
	r.Int("WORDS", 1)
	r.Duration("NEGATIVE", time.Second)
	r.Duration("NO_UNIT", time.Second)
	r.LogLevel("LOG_LEVEL", slog.LevelInfo)

	err := r.Err()
	if err == nil {
		t.Fatal("Err = nil, want every problem")
	}
	for _, key := range []string{"DATABASE", "EMPTY_TOKEN", "ZERO", "WORDS", "NEGATIVE", "NO_UNIT", "LOG_LEVEL"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("Err does not name %s:\n%v", key, err)
		}
	}
}

func TestReaderLogValueShowsSettingsAndHidesSecrets(t *testing.T) {
	r := NewReader(env(map[string]string{"PROVIDER_KEY": "hunter2", "ADDR": ":9090"}))
	r.Secret("PROVIDER_KEY")
	r.String("ADDR", ":8080")
	r.Int("CONCURRENCY", 32)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config loaded", "config", r)
	line := buf.String()

	for _, want := range []string{`"PROVIDER_KEY":"***"`, `"ADDR":":9090"`, `"CONCURRENCY":"32"`} {
		if !strings.Contains(line, want) {
			t.Errorf("log line lacks %s:\n%s", want, line)
		}
	}
	if strings.Contains(line, "hunter2") {
		t.Errorf("log line shows the secret:\n%s", line)
	}
}
