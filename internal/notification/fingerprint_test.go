package notification

import "testing"

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
