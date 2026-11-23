package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/comersaglam/Cos-Insider-CaseStudy/internal/notification/internal/db"
)

// SummaryWindow is how far back the summary counts attempts and measures latency.
const SummaryWindow = 5 * time.Minute

// Summary is the state of the service as a person reads it (ADR 0026): what waits, and how sending has gone over a
// recent window, by channel.
type Summary struct {
	Window   time.Duration
	Channels map[Channel]ChannelSummary
}

// ChannelSummary is one channel's part of a Summary.
type ChannelSummary struct {
	// Waiting counts the notifications not yet finished, by status.
	Waiting map[Status]int
	// Attempts counts the window's attempts by outcome.
	Attempts map[AttemptOutcome]int
	// Latency is the provider's over the window's answered attempts; nil when there were none.
	Latency *Latency
}

// Latency is a distribution's percentiles.
type Latency struct {
	P50, P95, P99 time.Duration
}

// Summary reads what waits now and what was attempted over the last window, from the database, so every replica
// answers the same.
func (s *Store) Summary(ctx context.Context, window time.Duration) (Summary, error) {
	q := db.New(s.pool)
	out := Summary{Window: window, Channels: map[Channel]ChannelSummary{}}
	for _, c := range Channels() {
		out.Channels[c] = ChannelSummary{Waiting: map[Status]int{}, Attempts: map[AttemptOutcome]int{}}
	}
	for _, st := range waitingStatuses {
		for _, c := range Channels() {
			out.Channels[c].Waiting[st] = 0
		}
	}

	waiting, err := q.CountWaiting(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("count waiting notifications: %w", err)
	}
	for _, w := range waiting {
		out.Channels[Channel(w.Channel)].Waiting[Status(w.Status)] = w.Count
	}

	windowMs := int(window.Milliseconds())
	attempts, err := q.CountAttemptsSince(ctx, windowMs)
	if err != nil {
		return Summary{}, fmt.Errorf("count attempts: %w", err)
	}
	for _, a := range attempts {
		out.Channels[Channel(a.Channel)].Attempts[AttemptOutcome(a.Outcome)] = a.Count
	}

	latencies, err := q.AttemptLatencySince(ctx, windowMs)
	if err != nil {
		return Summary{}, fmt.Errorf("measure latency: %w", err)
	}
	for _, l := range latencies {
		cs := out.Channels[Channel(l.Channel)]
		cs.Latency = &Latency{P50: ms(l.P50), P95: ms(l.P95), P99: ms(l.P99)}
		out.Channels[Channel(l.Channel)] = cs
	}
	return out, nil
}

// ms converts milliseconds, possibly fractional, to a duration.
func ms(v float64) time.Duration {
	return time.Duration(v * float64(time.Millisecond))
}
