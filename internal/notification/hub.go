package notification

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"
	"uuid"
)

// subscriptionBuffer is how many events a subscriber may fall behind by before it is dropped: enough for a burst,
// such as a batch's notifications going to sending at once, while bounding the memory a stalled client holds.
const subscriptionBuffer = 64

// relistenDelay is the pause before the hub listens again after losing its listener.
const relistenDelay = time.Second

// ErrSubscriberTooSlow closes a subscription that did not keep up with its events.
var ErrSubscriberTooSlow = errors.New("the subscriber did not keep up with its events")

// ErrHubStopped closes every subscription when the hub stops, as the server does.
var ErrHubStopped = errors.New("the server is stopping")

// StatusFilter selects the status events a subscriber receives: those of the listed notifications, and those of
// the batch.
type StatusFilter struct {
	NotificationIDs []uuid.UUID
	BatchID         *uuid.UUID
}

func (f StatusFilter) matches(e StatusEvent) bool {
	return slices.Contains(f.NotificationIDs, e.NotificationID) ||
		(f.BatchID != nil && e.BatchID != nil && *f.BatchID == *e.BatchID)
}

// Subscription is one subscriber's stream of status events.
type Subscription struct {
	filter StatusFilter
	events chan StatusEvent
	err    error // why events was closed; written before the close
}

// Events delivers the subscriber's events; it is closed when the subscription ends, and Err then says why.
func (s *Subscription) Events() <-chan StatusEvent {
	return s.events
}

// Err returns why the subscription ended, once Events is closed: ErrSubscriberTooSlow or ErrHubStopped, or nil
// when it was unsubscribed.
func (s *Subscription) Err() error {
	return s.err
}

// StatusSource is where the hub hears status changes: a *StatusListener, or a fake in tests.
type StatusSource interface {
	Next(ctx context.Context) (StatusEvent, error)
	Close(ctx context.Context) error
}

// Hub passes the status changes PostgreSQL announces to the subscribers whose filter matches them (ADR 0033). It
// never waits for a subscriber: one that falls subscriptionBuffer events behind is dropped.
type Hub struct {
	log     *slog.Logger
	mu      sync.Mutex
	subs    map[*Subscription]struct{}
	stopped bool
}

// NewHub returns a hub with no subscriber; Run feeds it.
func NewHub(log *slog.Logger) *Hub {
	return &Hub{log: log, subs: map[*Subscription]struct{}{}}
}

// Subscribe starts a subscription to the events f selects. After the hub has stopped, the subscription is closed
// at once with ErrHubStopped.
func (h *Hub) Subscribe(f StatusFilter) *Subscription {
	s := &Subscription{filter: f, events: make(chan StatusEvent, subscriptionBuffer)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		s.err = ErrHubStopped
		close(s.events)
		return s
	}
	h.subs[s] = struct{}{}
	return s
}

// Unsubscribe ends s; it may be called more than once, and after the hub has ended s.
func (h *Hub) Unsubscribe(s *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.end(s, nil)
}

// Publish passes e to every matching subscriber, dropping one whose buffer is full rather than waiting for it.
func (h *Hub) Publish(e StatusEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		if !s.filter.matches(e) {
			continue
		}
		select {
		case s.events <- e:
		default:
			h.end(s, ErrSubscriberTooSlow)
		}
	}
}

// end removes s and closes its events with err, once; h.mu is held. Only the caller that removes s closes it.
func (h *Hub) end(s *Subscription, err error) {
	if _, ok := h.subs[s]; !ok {
		return
	}
	delete(h.subs, s)
	s.err = err
	close(s.events)
}

// Run listens for status changes and publishes them until ctx ends, listening again after relistenDelay when the
// listener is lost; then it ends every subscription with ErrHubStopped. listen opens a listener.
func (h *Hub) Run(ctx context.Context, listen func(context.Context) (StatusSource, error)) {
	defer h.stop()
	for {
		l, err := listen(ctx)
		if err == nil {
			err = h.relay(ctx, l)
			_ = l.Close(context.WithoutCancel(ctx))
		}
		if ctx.Err() != nil {
			return
		}
		h.log.WarnContext(ctx, "status listener lost; listening again", "error", err, "after", relistenDelay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(relistenDelay):
		}
	}
}

// relay publishes l's events until l fails.
func (h *Hub) relay(ctx context.Context, l StatusSource) error {
	for {
		e, err := l.Next(ctx)
		if err != nil {
			return err
		}
		h.Publish(e)
	}
}

func (h *Hub) stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopped = true
	for s := range h.subs {
		h.end(s, ErrHubStopped)
	}
}
