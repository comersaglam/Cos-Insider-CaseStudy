package notification

import "testing"

var allStatuses = []Status{
	StatusScheduled, StatusQueued, StatusSending, StatusRetrying, StatusSent, StatusFailed, StatusCancelled,
}

func TestStatusValid(t *testing.T) {
	for _, s := range allStatuses {
		if !s.Valid() {
			t.Errorf("Status(%q).Valid() = false, want true", s)
		}
	}
	for _, s := range []Status{"pending", "SENT", ""} {
		if s.Valid() {
			t.Errorf("Status(%q).Valid() = true, want false", s)
		}
	}
}

// TestStatusCanTransitionTo checks every pair of statuses, so a transition added by mistake fails as surely as
// one that is missing.
func TestStatusCanTransitionTo(t *testing.T) {
	allowed := map[[2]Status]bool{
		{StatusScheduled, StatusQueued}:    true, // the scheduler finds it due
		{StatusScheduled, StatusCancelled}: true,
		{StatusQueued, StatusSending}:      true, // a worker claims it
		{StatusQueued, StatusCancelled}:    true,
		{StatusSending, StatusSent}:        true,
		{StatusSending, StatusRetrying}:    true, // a transient failure with attempts left
		{StatusSending, StatusFailed}:      true, // a permanent failure, or no attempts left
		{StatusRetrying, StatusQueued}:     true, // the scheduler finds the retry due
		{StatusRetrying, StatusCancelled}:  true,
		{StatusFailed, StatusQueued}:       true, // the retry endpoint
	}
	for _, from := range allStatuses {
		for _, to := range allStatuses {
			want := allowed[[2]Status{from, to}]
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%s.CanTransitionTo(%s) = %v, want %v", from, to, got, want)
			}
		}
	}
}
