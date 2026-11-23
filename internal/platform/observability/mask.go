package observability

import "strings"

// mask stands in for the hidden part of a value. It has a fixed length so it does not reveal how long the value is.
const mask = "***"

// shortest is the length below which a recipient is masked entirely, since showing five of its characters would
// show most of it.
const shortest = 7

// MaskRecipient returns a recipient in a form that may be logged. Phone numbers, e-mail addresses and device
// tokens are personal data and are never logged in full:
//
//	+905551234567     -> +90***67
//	ayse@example.com  -> a***@example.com
//
// An e-mail address keeps its domain, which says which provider to check; anything else keeps its first three
// and last two characters.
func MaskRecipient(s string) string {
	if s == "" {
		return ""
	}
	if at := strings.LastIndex(s, "@"); at >= 0 {
		local := []rune(s[:at])
		if len(local) == 0 {
			return mask + s[at:]
		}
		return string(local[0]) + mask + s[at:]
	}
	r := []rune(s)
	if len(r) < shortest {
		return mask
	}
	return string(r[:3]) + mask + string(r[len(r)-2:])
}
