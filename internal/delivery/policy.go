package delivery

import (
	"math/rand/v2"
	"time"
	"uuid"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification"
)

// RetryPolicy decides what an attempt's outcome makes of a notification (ADR 0007): sent, failed, or retrying after
// an exponential backoff with full jitter.
type RetryPolicy struct {
	Base        time.Duration
	Cap         time.Duration
	MaxAttempts int
}

// Delay returns the wait after attempt n (counted from 1): a random duration in [0, min(Cap, Base·2^(n−1))], the
// full jitter that spreads out a burst failing together, but never less than retryAfter, the wait the provider
// asked for.
func (p RetryPolicy) Delay(n int, retryAfter time.Duration) time.Duration {
	ceiling := p.Cap
	// Base·2^(n−1), unless it reaches the cap or overflows on the way.
	if shift := n - 1; shift >= 0 && shift < 62 {
		if d := p.Base << shift; d > 0 && d < p.Cap {
			ceiling = d
		}
	}
	// Jitter spreads retries out; it guards no secret, so it needs no cryptographic randomness.
	return max(time.Duration(rand.Int64N(int64(ceiling)+1)), retryAfter) //nolint:gosec // G404: jitter, not a secret

}

// After turns attempt n's outcome into its record and the notification's next status: sent on success, failed on
// a permanent outcome or when no attempt is left, and otherwise retrying at now plus Delay.
func (p RetryPolicy) After(id uuid.UUID, n int, out Outcome, now time.Time) notification.Attempt {
	a := notification.Attempt{
		NotificationID: id, Number: n, Outcome: notification.AttemptOutcome(out.Class),
		StatusCode: out.StatusCode, Duration: out.Latency,
	}
	if out.Err != nil {
		a.Error = out.Err.Error()
	}
	switch {
	case out.Class == Success:
		a.Next, a.ProviderMessageID = notification.StatusSent, out.MessageID
	case out.Class == Permanent || out.Class == PermanentAlert:
		a.Next, a.FailureReason = notification.StatusFailed, notification.FailureProviderRejected
	case n >= p.MaxAttempts:
		a.Next, a.FailureReason = notification.StatusFailed, notification.FailureAttemptsExhausted
	default:
		a.Next, a.NextAttemptAt = notification.StatusRetrying, now.Add(p.Delay(n, out.RetryAfter))
	}
	return a
}
