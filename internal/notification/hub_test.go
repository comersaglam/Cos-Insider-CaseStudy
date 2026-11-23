package notification

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"uuid"
)

func event(id uuid.UUID, batch *uuid.UUID, s Status) StatusEvent {
	return StatusEvent{NotificationID: id, BatchID: batch, Status: s, At: time.Now().UTC()}
}

// received drains what a subscription holds now.
func received(s *Subscription) []StatusEvent {
	var out []StatusEvent
	for {
		select {
		case e, ok := <-s.Events():
			if !ok {
				return out
			}
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestHubDeliversToMatchingSubscribers(t *testing.T) {
	hub := NewHub(slog.New(slog.DiscardHandler))
	n1, n2, batch := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	byID := hub.Subscribe(StatusFilter{NotificationIDs: []uuid.UUID{n1}})
	byBatch := hub.Subscribe(StatusFilter{BatchID: &batch})
	other := hub.Subscribe(StatusFilter{NotificationIDs: []uuid.UUID{n2}})

	hub.Publish(event(n1, &batch, StatusSent))
	if len(received(byID)) != 1 || len(received(byBatch)) != 1 || len(received(other)) != 0 {
		t.Error("the event did not reach exactly the subscribers to its notification and its batch")
	}

	hub.Unsubscribe(byID)
	hub.Unsubscribe(byID) // twice is harmless
	hub.Publish(event(n1, nil, StatusFailed))
	if len(received(byID)) != 0 {
		t.Error("an unsubscribed subscriber still received")
	}
}

// TestHubDropsASlowSubscriberWithoutWaiting: a subscriber that does not keep up is closed with ErrSubscriberTooSlow;
// publishing never waits for it, and the others still receive.
func TestHubDropsASlowSubscriberWithoutWaiting(t *testing.T) {
	hub := NewHub(slog.New(slog.DiscardHandler))
	id := uuid.NewV7()
	slow := hub.Subscribe(StatusFilter{NotificationIDs: []uuid.UUID{id}})
	fast := hub.Subscribe(StatusFilter{NotificationIDs: []uuid.UUID{id}})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range subscriptionBuffer + 1 {
			hub.Publish(event(id, nil, StatusSending))
			received(fast)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publishing waited for a slow subscriber")
	}
	if got := len(received(slow)); got != subscriptionBuffer {
		t.Errorf("the slow subscriber holds %d events, want the %d that fit", got, subscriptionBuffer)
	}
	if _, open := <-slow.Events(); open || !errors.Is(slow.Err(), ErrSubscriberTooSlow) {
		t.Errorf("the slow subscriber: open %v, err %v; want closed, too slow", open, slow.Err())
	}
}

// fakeListener answers Next from its events, then fails as a lost connection does.
type fakeListener struct {
	events []StatusEvent
}

func (l *fakeListener) Next(ctx context.Context) (StatusEvent, error) {
	if len(l.events) == 0 {
		return StatusEvent{}, errors.New("connection lost")
	}
	e := l.events[0]
	l.events = l.events[1:]
	return e, nil
}

func (l *fakeListener) Close(context.Context) error { return nil }

// TestHubListensAgainAndClosesSubscriptionsWhenStopped: a lost listener is replaced, and when Run stops every
// subscription closes with ErrHubStopped, as does one taken afterwards.
func TestHubListensAgainAndClosesSubscriptionsWhenStopped(t *testing.T) {
	hub := NewHub(slog.New(slog.DiscardHandler))
	id := uuid.NewV7()
	sub := hub.Subscribe(StatusFilter{NotificationIDs: []uuid.UUID{id}})

	var mu sync.Mutex
	opened := 0
	listen := func(context.Context) (StatusSource, error) {
		mu.Lock()
		defer mu.Unlock()
		opened++
		if opened == 1 {
			return nil, errors.New("database starting")
		}
		return &fakeListener{events: []StatusEvent{event(id, nil, StatusQueued)}}, nil
	}
	ctx, stop := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() { hub.Run(ctx, listen); close(stopped) }()

	select {
	case e := <-sub.Events():
		if e.Status != StatusQueued {
			t.Errorf("event = %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event after the listener was opened again")
	}
	stop()
	<-stopped
	if _, open := <-sub.Events(); open || !errors.Is(sub.Err(), ErrHubStopped) {
		t.Errorf("after Run stopped: open %v, err %v", open, sub.Err())
	}
	late := hub.Subscribe(StatusFilter{NotificationIDs: []uuid.UUID{id}})
	if _, open := <-late.Events(); open || !errors.Is(late.Err(), ErrHubStopped) {
		t.Errorf("a subscription after the stop: open %v, err %v", open, late.Err())
	}
}
