package notification

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestWaitingCollectorCountsOnScrape: the counts are read when Prometheus scrapes, and every channel and waiting
// status is reported, at zero when nothing waits there, so a drained queue reads 0 rather than its last value.
func TestWaitingCollectorCountsOnScrape(t *testing.T) {
	t.Parallel()
	store, pool := newTestStore(t)
	mustCreate(t, store, newQueued(testDraft))
	mustCreate(t, store, newQueued(testDraft))
	retrying := mustCreate(t, store, newQueued(Draft{To: "a@example.com", Channel: ChannelEmail, Content: "x", Priority: PriorityLow}))
	makeWaiting(t, pool, retrying.ID, StatusRetrying, time.Now().Add(time.Minute))

	want := `
# HELP notifications_outbox_pending Outbox rows not yet published to RabbitMQ: how far the relay is behind.
# TYPE notifications_outbox_pending gauge
notifications_outbox_pending 3
# HELP notifications_waiting Notifications not yet finished, by channel and status, counted when scraped.
# TYPE notifications_waiting gauge
notifications_waiting{channel="email",status="queued"} 0
notifications_waiting{channel="email",status="retrying"} 1
notifications_waiting{channel="email",status="scheduled"} 0
notifications_waiting{channel="email",status="sending"} 0
notifications_waiting{channel="push",status="queued"} 0
notifications_waiting{channel="push",status="retrying"} 0
notifications_waiting{channel="push",status="scheduled"} 0
notifications_waiting{channel="push",status="sending"} 0
notifications_waiting{channel="sms",status="queued"} 2
notifications_waiting{channel="sms",status="retrying"} 0
notifications_waiting{channel="sms",status="scheduled"} 0
notifications_waiting{channel="sms",status="sending"} 0
`
	if err := testutil.CollectAndCompare(NewWaitingCollector(store), strings.NewReader(want)); err != nil {
		t.Error(err)
	}
}
