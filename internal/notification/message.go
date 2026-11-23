package notification

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"uuid"
)

// QueueMessage is the body of the message that asks a worker to send a notification. It names the notification
// only: the worker reads the rest from the database, where its status says whether it is still to be sent.
type QueueMessage struct {
	NotificationID uuid.UUID `json:"notificationId"`
}

// Marshal encodes m as JSON.
func (m QueueMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// ParseQueueMessage decodes a message body, refusing one that does not name a notification.
func ParseQueueMessage(body []byte) (QueueMessage, error) {
	var m QueueMessage
	if err := json.Unmarshal(body, &m, json.RejectUnknownMembers(true)); err != nil {
		return QueueMessage{}, fmt.Errorf("read queue message: %w", err)
	}
	if m.NotificationID == (uuid.UUID{}) {
		return QueueMessage{}, errors.New("read queue message: no notification ID")
	}
	return m, nil
}
