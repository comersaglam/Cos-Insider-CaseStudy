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
}

// SchedulerConfig sets the scheduler's pace.
type SchedulerConfig struct {
	// Interval is how often the scheduler looks for due work: the precision of retries and scheduled sends.
	Interval time.Duration
	// BatchSize is the most notifications one statement queues.
	BatchSize int
}

// Scheduler queues waiting notifications whose time has come: scheduled ones at their time and retrying ones at
// their next attempt (ADR 0006). Queueing writes their outbox rows, whose trigger wakes the relay.
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
