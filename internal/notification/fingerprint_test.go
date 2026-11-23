package notification

import (
	"encoding/hex"
	"testing"
	"time"
	"uuid"
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
		{"from a template", Draft{To: base.To, Priority: base.Priority, TemplateID: &fingerprintTemplate}},
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

var fingerprintTemplate = uuid.MustParse("01a20a40-5c1e-7b3a-9d4e-2f1a3b4c5d6e")

// TestDraftFingerprintOfATemplate: a request from a template is told apart by its template and its variables,
// whatever order the variables came in.
func TestDraftFingerprintOfATemplate(t *testing.T) {
	other := uuid.NewV7()
	d := Draft{To: "+905551234567", Priority: PriorityNormal, TemplateID: &fingerprintTemplate,
		Variables: map[string]string{"name": "Ayşe", "order": "42"}}
	for i := range 20 { // a map's order changes from run to run
		same := d
		same.Variables = map[string]string{"order": "42", "name": "Ayşe"}
		if same.Fingerprint() != d.Fingerprint() {
			t.Fatalf("run %d: the same variables gave another fingerprint", i)
		}
	}
	for name, changed := range map[string]Draft{
		"another template":   {To: d.To, Priority: d.Priority, TemplateID: &other, Variables: d.Variables},
		"another value":      {To: d.To, Priority: d.Priority, TemplateID: d.TemplateID, Variables: map[string]string{"name": "Ali", "order": "42"}},
		"a value moved":      {To: d.To, Priority: d.Priority, TemplateID: d.TemplateID, Variables: map[string]string{"name": "Ayşe42", "order": ""}},
		"one variable fewer": {To: d.To, Priority: d.Priority, TemplateID: d.TemplateID, Variables: map[string]string{"name": "Ayşe"}},
	} {
		if changed.Fingerprint() == d.Fingerprint() {
			t.Errorf("%s: the fingerprint did not change", name)
		}
	}
}
