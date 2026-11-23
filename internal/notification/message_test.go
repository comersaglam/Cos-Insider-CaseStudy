package notification

import (
	"testing"
	"uuid"
)

func TestQueueMessageRoundTrip(t *testing.T) {
	m := QueueMessage{NotificationID: uuid.NewV7()}
	body, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseQueueMessage(body)
	if err != nil || got != m {
		t.Errorf("ParseQueueMessage(%s) = %+v, %v; want %+v", body, got, err, m)
	}
}

func TestParseQueueMessageRefusesWhatItCannotRead(t *testing.T) {
	for _, body := range []string{``, `not json`, `{}`, `{"notificationId":"42"}`, `{"notificationId":"` + uuid.NewV7().String() + `","x":1}`} {
		if _, err := ParseQueueMessage([]byte(body)); err == nil {
			t.Errorf("ParseQueueMessage(%q) = nil error", body)
		}
	}
}
