package notification

import (
	"crypto/sha256"
	"maps"
	"slices"
	"strconv"
	"time"
)

// Fingerprint returns the SHA-256 of the draft's fields. Two requests with one idempotency key are the same
// request exactly when their fingerprints are equal.
func (d Draft) Fingerprint() [32]byte {
	h := sha256.New()
	fields := []string{d.To, string(d.Channel), d.Content, string(d.Priority)}
	// Only a schedule that is there is written, so a request without one keeps the fingerprint it had before
	// scheduling existed. The instant is written in UTC: one moment in two offsets is one schedule.
	if d.ScheduledAt != nil {
		fields = append(fields, d.ScheduledAt.UTC().Format(time.RFC3339Nano))
	}
	// A request from a template is the template and its variables as written, not the rendered content, so a
	// repeat is still the same request after the template has changed. Variables are written sorted by name: a
	// map has no order.
	if d.TemplateID != nil {
		fields = append(fields, "templateId:"+d.TemplateID.String())
		for _, name := range slices.Sorted(maps.Keys(d.Variables)) {
			fields = append(fields, "variable:"+name, d.Variables[name])
		}
	}
	for _, field := range fields {
		// Each field is written after its length, so moving characters from one field into the next changes
		// the result.
		h.Write([]byte(strconv.Itoa(len(field)) + ":" + field))
	}
	return [32]byte(h.Sum(nil))
}
