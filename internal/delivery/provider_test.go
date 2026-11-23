package delivery

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
)

var testNotification = notification.Notification{
	ID: uuid.MustParse("01938f2e-7c1e-7b3a-9d4e-2f1a3b4c5d6e"), To: "+905551234567", Channel: notification.ChannelSMS,
	Content: "Your order has shipped.", Priority: notification.PriorityHigh, CorrelationID: "corr-7",
}

// provider starts a fake provider that answers every request with h.
func provider(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, time.Second)
}

func TestSendPostsTheProviderContract(t *testing.T) {
	var got struct {
		method, contentType, key, correlation string
		body                                  map[string]any
	}
	c := provider(t, func(w http.ResponseWriter, r *http.Request) {
		got.method, got.contentType = r.Method, r.Header.Get("Content-Type")
		got.key, got.correlation = r.Header.Get("Idempotency-Key"), r.Header.Get("X-Correlation-ID")
		_ = json.UnmarshalRead(r.Body, &got.body)
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"messageId":"m-123","status":"accepted","timestamp":"2026-11-23T12:00:00Z"}`)
	})

	out := c.Send(t.Context(), testNotification)
	if out.Class != Success || out.StatusCode != 202 || out.MessageID != "m-123" || out.Err != nil {
		t.Errorf("outcome = %+v, want success with the message ID", out)
	}
	if out.Latency <= 0 {
		t.Errorf("latency = %v, want it measured", out.Latency)
	}
	want := map[string]any{"to": "+905551234567", "channel": "sms", "content": "Your order has shipped."}
	if len(got.body) != 3 || got.body["to"] != want["to"] || got.body["channel"] != want["channel"] || got.body["content"] != want["content"] {
		t.Errorf("body = %v, want exactly %v", got.body, want)
	}
	if got.method != http.MethodPost || got.contentType != "application/json" ||
		got.key != testNotification.ID.String() || got.correlation != "corr-7" {
		t.Errorf("request = %s %q key %q correlation %q", got.method, got.contentType, got.key, got.correlation)
	}
}

func TestSendClassifiesEveryAnswer(t *testing.T) {
	tests := []struct {
		status int
		want   Class
	}{
		{200, Success}, {202, Success},
		{400, Permanent}, {404, Permanent}, {409, Permanent}, {422, Permanent},
		{401, PermanentAlert}, {403, PermanentAlert},
		{408, Transient}, {429, Transient}, {500, Transient}, {502, Transient}, {503, Transient}, {504, Transient},
		{302, Permanent}, // a redirect is not followed: re-sending a POST elsewhere is not ours to decide
	}
	for _, tt := range tests {
		c := provider(t, func(w http.ResponseWriter, _ *http.Request) {
			if tt.status == 302 {
				w.Header().Set("Location", "/elsewhere")
			}
			w.WriteHeader(tt.status)
			_, _ = io.WriteString(w, `{"error":"some reason"}`)
		})
		out := c.Send(t.Context(), testNotification)
		if out.Class != tt.want || out.StatusCode != tt.status {
			t.Errorf("%d: class %s status %d, want %s", tt.status, out.Class, out.StatusCode, tt.want)
		}
		if tt.want != Success && (out.Err == nil || !strings.Contains(out.Err.Error(), "some reason")) {
			t.Errorf("%d: error %v, want the provider's answer in it", tt.status, out.Err)
		}
	}
}

func TestSendReadsRetryAfter(t *testing.T) {
	tests := []struct {
		name, value string
		want        time.Duration
	}{
		{"seconds", "7", 7 * time.Second},
		{"an HTTP date", time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat), 30 * time.Second},
		{"absent", "", 0},
		{"malformed", "soon", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := provider(t, func(w http.ResponseWriter, _ *http.Request) {
				if tt.value != "" {
					w.Header().Set("Retry-After", tt.value)
				}
				w.WriteHeader(http.StatusTooManyRequests)
			})
			out := c.Send(t.Context(), testNotification)
			// An HTTP date has a precision of a second.
			if out.RetryAfter < tt.want-2*time.Second || out.RetryAfter > tt.want {
				t.Errorf("RetryAfter = %v, want about %v", out.RetryAfter, tt.want)
			}
		})
	}
}

// TestSendTreatsATimeoutAsTransient: a timeout is ambiguous, the message may have arrived, so it is retried with the
// same idempotency key (ADR 0007).
func TestSendTreatsATimeoutAsTransient(t *testing.T) {
	c := provider(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	c.http.Timeout = 100 * time.Millisecond

	start := time.Now()
	out := c.Send(t.Context(), testNotification)
	if out.Class != Transient || out.StatusCode != 0 || out.Err == nil {
		t.Errorf("outcome = %+v, want transient with no status", out)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %v, want the timeout to cut it short", took)
	}
}

func TestSendTreatsAnUnreachableProviderAsTransient(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if out := NewClient(url, time.Second).Send(t.Context(), testNotification); out.Class != Transient || out.Err == nil {
		t.Errorf("outcome = %+v, want transient", out)
	}
}

// TestSendAcceptsAnAcceptanceItCannotRead: the provider took the message, so it is sent; retrying would send it again.
func TestSendAcceptsAnAcceptanceItCannotRead(t *testing.T) {
	c := provider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `not json`)
	})
	if out := c.Send(t.Context(), testNotification); out.Class != Success || out.MessageID != "" {
		t.Errorf("outcome = %+v, want success without a message ID", out)
	}
}

func TestSendReadsALimitedAnswer(t *testing.T) {
	huge := strings.Repeat("x", 10<<20)
	c := provider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, huge)
	})
	out := c.Send(t.Context(), testNotification)
	if out.Class != Transient || len(out.Err.Error()) > 1024 {
		t.Errorf("class %s, error of %d bytes; want transient with the answer cut short", out.Class, len(out.Err.Error()))
	}
}

func TestSendStopsWithItsContext(t *testing.T) {
	c := provider(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if out := c.Send(ctx, testNotification); out.Class != Transient || !errors.Is(out.Err, context.Canceled) {
		t.Errorf("outcome = %+v, want transient with context.Canceled", out)
	}
}
