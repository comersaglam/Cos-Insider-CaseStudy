package notification

import (
	"encoding/hex"
	"testing"
	"time"
)

func TestDraftFingerprint(t *testing.T) {
	base := Draft{To: "+905551234567", Channel: ChannelSMS, Content: "Your order has shipped.", Priority: PriorityNormal}

	equal := Draft{To: "+905551234567", Channel: ChannelSMS, Content: "Your order has shipped.", Priority: PriorityNormal}
	if equal.Fingerprint() != base.Fingerprint() {
		t.Fatal("two equal drafts gave different fingerprints")
	}

	tests := []struct {
		name  string
		other Draft
	}{
		{"another recipient", Draft{To: "+905551234568", Channel: base.Channel, Content: base.Content, Priority: base.Priority}},
		{"another channel", Draft{To: base.To, Channel: ChannelPush, Content: base.Content, Priority: base.Priority}},
		{"another content", Draft{To: base.To, Channel: base.Channel, Content: "Your order is late.", Priority: base.Priority}},
		{"another priority", Draft{To: base.To, Channel: base.Channel, Content: base.Content, Priority: PriorityHigh}},
		{"scheduled", Draft{To: base.To, Channel: base.Channel, Content: base.Content, Priority: base.Priority, ScheduledAt: &testNow}},
	}
	for _, tt := range tests {
		if tt.other.Fingerprint() == base.Fingerprint() {
			t.Errorf("%s: the fingerprint did not change", tt.name)
		}
	}
}

// TestDraftFingerprintKeepsFieldsApart guards against concatenating fields without their lengths, which would
// make "a" + "bc" and "ab" + "c" the same request.
func TestDraftFingerprintKeepsFieldsApart(t *testing.T) {
	a := Draft{To: "a", Channel: ChannelPush, Content: "bc", Priority: PriorityLow}
	b := Draft{To: "ab", Channel: ChannelPush, Content: "c", Priority: PriorityLow}
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("two different drafts share a fingerprint")
	}
}

// TestDraftFingerprintWithoutScheduleIsStable: a request without scheduledAt has the fingerprint it had before
// scheduling existed, so a key stored then still matches its repeat.
func TestDraftFingerprintWithoutScheduleIsStable(t *testing.T) {
	d := Draft{To: "+905551234567", Channel: ChannelSMS, Content: "Your order has shipped.", Priority: PriorityHigh}
	fp := d.Fingerprint()
	if got := hex.EncodeToString(fp[:]); got != "8df354c09c8cfd15456ee5f66c11808c8b0e140ee899aa23745343a51a510e82" {
		t.Errorf("fingerprint = %s, changed for a request without a schedule", got)
	}
}

// TestDraftFingerprintComparesInstants: one moment written in two offsets is one schedule.
func TestDraftFingerprintComparesInstants(t *testing.T) {
	utc := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	istanbul := utc.In(time.FixedZone("+03", 3*60*60))
	a := Draft{To: "+905551234567", Channel: ChannelSMS, Content: "hi", Priority: PriorityNormal, ScheduledAt: &utc}
	b := a
	b.ScheduledAt = &istanbul
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("the same instant in another offset gave another fingerprint")
	}
}
