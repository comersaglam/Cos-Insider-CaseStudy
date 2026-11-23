package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// due is what the scheduler needs from storage; *notification.Store satisfies it.
type due interface {
	QueueDue(ctx context.Context, limit int) (int, error)
	ReapExpiredClaims(ctx context.Context, maxAttempts, limit int) (int, error)
}

// SchedulerConfig sets the scheduler's pace.
type SchedulerConfig struct {
	// Interval is how often the scheduler looks for due work: the precision of retries and scheduled sends.
	Interval time.Duration
	// BatchSize is the most notifications one statement queues or reaps.
	BatchSize int
	// MaxAttempts is the delivery policy's limit: a reaped notification with none left fails.
	MaxAttempts int
}

// Scheduler queues waiting notifications whose time has come: scheduled ones at their time and retrying ones at
// their next attempt (ADR 0006). Queueing writes their outbox rows, whose trigger wakes the relay. Before that, as
// the reaper of ADR 0012, it returns notifications whose worker's lease ran out to retrying, due at once.
type Scheduler struct {
	due due
	cfg SchedulerConfig
	log *slog.Logger
}

// NewScheduler returns a scheduler over the given storage.
func NewScheduler(d due, cfg SchedulerConfig, log *slog.Logger) *Scheduler {
	return &Scheduler{due: d, cfg: cfg, log: log}
}

// Run queues due work every Interval until ctx ends, then returns nil; it returns an error when the database
// fails, and the process stops to be restarted (ADR 0009).
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		if err := s.reap(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		for {
			n, err := s.due.QueueDue(ctx, s.cfg.BatchSize)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("queue due notifications: %w", err)
			}
			if n > 0 {
				s.log.InfoContext(ctx, "due notifications queued", "queued", n)
			}
			if n < s.cfg.BatchSize {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// reap returns notifications held by lost workers, a batch at a time.
func (s *Scheduler) reap(ctx context.Context) error {
	for {
		n, err := s.due.ReapExpiredClaims(ctx, s.cfg.MaxAttempts, s.cfg.BatchSize)
		if err != nil {
			return fmt.Errorf("reap expired claims: %w", err)
		}
		if n > 0 {
			s.log.WarnContext(ctx, "notifications of lost workers reaped", "reaped", n)
		}
		if n < s.cfg.BatchSize {
			return nil
		}
	}
}
