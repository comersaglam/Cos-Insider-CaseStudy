package delivery

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/platform/observability"
)

// Class is what an outcome means for the notification (ADR 0007).
type Class string

// The classes of outcome.
const (
	// Success: the provider took the message; the notification is sent.
	Success Class = "success"
	// Permanent: the same request would fail again; the notification fails at once.
	Permanent Class = "permanent"
	// PermanentAlert is Permanent caused by our own configuration (the provider refused our credentials), which
	// someone has to fix.
	PermanentAlert Class = "permanent_alert"
	// Transient: the provider may take the message later; the notification is retried.
	Transient Class = "transient"
)

// maxAnswer bounds how much of the provider's answer is read; a successful one is a few dozen bytes.
const maxAnswer = 64 << 10

// maxDetail bounds how much of a failed answer an outcome keeps, for the attempt's record.
const maxDetail = 256

// idleConnsPerHost keeps connections to the provider open between sends. Go keeps two by default, which would
// open and close connections all the time under 32 concurrent sends (ADR 0009).
const idleConnsPerHost = 64

// Outcome is what came of one attempt to send a notification.
type Outcome struct {
	Class      Class
	StatusCode int           // 0 when there was no answer
	MessageID  string        // the provider's ID for the message, on success, when it gave one
	RetryAfter time.Duration // the wait the provider asked for, 0 when it asked for none
	Latency    time.Duration
	Err        error // what went wrong, nil on success
}

// Client sends notifications to the provider (the brief's webhook.site contract).
type Client struct {
	url  string
	http *http.Client
}

// NewClient returns a client for the provider at url; each request is given at most timeout.
func NewClient(url string, timeout time.Duration) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = idleConnsPerHost
	return &Client{url: url, http: &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// A redirect is answered, not followed: sending a POST elsewhere is not the client's to decide.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// providerRequest is the provider contract's request, fixed by the brief (ADR 0011).
type providerRequest struct {
	To      string `json:"to"`
	Channel string `json:"channel"`
	Content string `json:"content"`
}

type providerAnswer struct {
	MessageID string `json:"messageId"`
}

// Send makes one attempt to send n and classifies what came of it. Every attempt for n carries n's ID as its
// Idempotency-Key, so a provider that honours the key sends it once however often it is retried (ADR 0012).
func (c *Client) Send(ctx context.Context, n notification.Notification) Outcome {
	body, err := json.Marshal(providerRequest{To: n.To, Channel: string(n.Channel), Content: n.Content})
	if err != nil {
		return Outcome{Class: Permanent, Err: fmt.Errorf("encode the request: %w", err)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return Outcome{Class: PermanentAlert, Err: fmt.Errorf("build the request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", n.ID.String())
	req.Header.Set(observability.CorrelationHeader, n.CorrelationID)

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		// No answer: the network failed, the provider timed out, or ctx ended. The message may have arrived.
		return Outcome{Class: Transient, Latency: time.Since(start), Err: fmt.Errorf("call the provider: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	answer, readErr := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	out := Outcome{StatusCode: resp.StatusCode, Latency: time.Since(start), RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}

	out.Class = classify(resp.StatusCode)
	if out.Class == Success {
		// The provider took the message: an answer that cannot be read only loses its ID.
		var a providerAnswer
		if readErr == nil && json.Unmarshal(answer, &a) == nil {
			out.MessageID = a.MessageID
		}
		return out
	}
	out.Err = fmt.Errorf("provider answered %d: %s", resp.StatusCode, detail(answer))
	return out
}

// classify maps an HTTP status to its class (ADR 0007).
func classify(status int) Class {
	switch {
	case status >= 200 && status < 300:
		return Success
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return PermanentAlert
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
		return Transient
	}
	// Any other answer, a redirect or a client error, would be the same on another attempt.
	return Permanent
}

// retryAfter reads a Retry-After header, in seconds or as an HTTP date; 0 when absent or unreadable.
func retryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(v); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0)
	}
	return 0
}

// detail keeps the start of a failed answer for the record.
func detail(answer []byte) string {
	if len(answer) == 0 {
		return "no body"
	}
	if len(answer) > maxDetail {
		return string(answer[:maxDetail]) + "…"
	}
	return string(answer)
}
