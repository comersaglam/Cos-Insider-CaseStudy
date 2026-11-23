package notification

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxContent is the longest content each channel accepts, counted in characters (Unicode code points), not
// bytes: "ş" is one character and two bytes.
var maxContent = map[Channel]int{
	ChannelSMS:   1600,    // a multi-part SMS, as SMS APIs accept it; the provider splits it into segments
	ChannelEmail: 100_000, // Gmail clips a message body above about 102 KB
	ChannelPush:  1000,    // well inside the 4 KB payload limit of APNs and FCM
}

// Recipient limits, in bytes: RFC 5321 limits an address in octets, and a push token is ASCII.
const (
	maxEmailAddress = 254 // the longest address an SMTP path can carry
	maxPushToken    = 512 // device tokens are far shorter; the limit only stops abuse
)

// e164 matches an international phone number: a plus sign, a country code that does not start with zero, and at
// most 15 digits in all.
var e164 = regexp.MustCompile(`^\+[1-9][0-9]{1,14}$`)

// Code is a stable, machine-readable reason a field was refused. Clients may rely on it; the message may change.
type Code string

// The reasons a field can be refused.
const (
	CodeRequired Code = "required"
	CodeInvalid  Code = "invalid"
	CodeTooLong  Code = "too_long"
)

// FieldError is one field that breaks a rule. Field is the field's name in the API and the provider contract.
type FieldError struct {
	Field   string
	Code    Code
	Message string
}

// ValidationError lists every field of a draft that breaks a rule, so a client can correct them all at once.
type ValidationError struct {
	Fields []FieldError
}

// Error joins the fields' messages into one line.
func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + ": " + f.Message
	}
	return "invalid notification: " + strings.Join(parts, "; ")
}

// Draft is a notification as a client asks for it, before it is stored. To, Channel and Content are exactly what
// the provider receives; Priority orders the work and is not sent.
type Draft struct {
	To       string
	Channel  Channel
	Content  string
	Priority Priority
}

// Validate checks every field of d and returns a *ValidationError listing each one that breaks a rule, or nil
// when d is valid. A recipient and content are checked against the rules of d's channel.
func (d Draft) Validate() error {
	var fields []FieldError
	for _, fe := range []*FieldError{
		checkChannel(d.Channel),
		checkRecipient(d.Channel, d.To),
		checkContent(d.Channel, d.Content),
		checkPriority(d.Priority),
	} {
		if fe != nil {
			fields = append(fields, *fe)
		}
	}
	if len(fields) == 0 {
		// Returned as a plain nil: a nil *ValidationError in an error interface would not compare equal to nil.
		return nil
	}
	return &ValidationError{Fields: fields}
}

func checkChannel(c Channel) *FieldError {
	switch {
	case c == "":
		return &FieldError{"channel", CodeRequired, "is required"}
	case !c.Valid():
		return &FieldError{"channel", CodeInvalid, "must be one of sms, email, push"}
	}
	return nil
}

func checkRecipient(c Channel, to string) *FieldError {
	if to == "" {
		return &FieldError{"to", CodeRequired, "is required"}
	}
	// No value is normalised: a recipient the provider would not take as written is refused, not guessed at.
	switch c {
	case ChannelSMS:
		if !e164.MatchString(to) {
			return &FieldError{"to", CodeInvalid, "must be an E.164 phone number such as +905551234567"}
		}
	case ChannelEmail:
		if len(to) > maxEmailAddress {
			return &FieldError{"to", CodeTooLong, fmt.Sprintf("must be at most %d bytes", maxEmailAddress)}
		}
		// ParseAddress also accepts "Name <address>" and strips spaces, so the result must equal the input.
		if addr, err := mail.ParseAddress(to); err != nil || addr.Name != "" || addr.Address != to {
			return &FieldError{"to", CodeInvalid, "must be one e-mail address such as ayse@example.com, without a name"}
		}
	case ChannelPush:
		if len(to) > maxPushToken {
			return &FieldError{"to", CodeTooLong, fmt.Sprintf("must be at most %d bytes", maxPushToken)}
		}
		if !isVisibleASCII(to) {
			return &FieldError{"to", CodeInvalid, "must be a device token of visible ASCII characters"}
		}
	}
	return nil
}

func checkContent(c Channel, content string) *FieldError {
	switch {
	case strings.TrimSpace(content) == "":
		return &FieldError{"content", CodeRequired, "is required"}
	case !utf8.ValidString(content):
		return &FieldError{"content", CodeInvalid, "must be valid UTF-8 text"}
	case strings.ContainsRune(content, 0):
		// PostgreSQL text cannot store NUL, so it is refused here rather than failing the insert.
		return &FieldError{"content", CodeInvalid, "must not contain the NUL character"}
	}
	if limit, ok := maxContent[c]; ok && utf8.RuneCountInString(content) > limit {
		return &FieldError{"content", CodeTooLong, fmt.Sprintf("must be at most %d characters for %s", limit, c)}
	}
	return nil
}

func checkPriority(p Priority) *FieldError {
	switch {
	case p == "":
		return &FieldError{"priority", CodeRequired, "is required"}
	case !p.Valid():
		return &FieldError{"priority", CodeInvalid, "must be one of high, normal, low"}
	}
	return nil
}

// isVisibleASCII reports whether s is made only of printable ASCII characters other than space.
func isVisibleASCII(s string) bool {
	for i := range len(s) {
		if s[i] < '!' || s[i] > '~' {
			return false
		}
	}
	return true
}
