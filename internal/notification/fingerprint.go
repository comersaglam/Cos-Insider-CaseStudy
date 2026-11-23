package notification

import (
	"crypto/sha256"
	"strconv"
)

// Fingerprint returns the SHA-256 of the draft's fields. Two requests with one idempotency key are the same
// request exactly when their fingerprints are equal.
func (d Draft) Fingerprint() [32]byte {
	h := sha256.New()
	for _, field := range []string{d.To, string(d.Channel), d.Content, string(d.Priority)} {
		// Each field is written after its length, so moving characters from one field into the next changes
		// the result.
		h.Write([]byte(strconv.Itoa(len(field)) + ":" + field))
	}
	return [32]byte(h.Sum(nil))
}
