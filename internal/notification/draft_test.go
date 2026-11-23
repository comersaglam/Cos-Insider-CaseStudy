package notification

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestDraftValidate(t *testing.T) {
	valid := Draft{To: "+905551234567", Channel: ChannelSMS, Content: "Your order has shipped.", Priority: PriorityNormal}

	tests := []struct {
		name string
		edit func(d *Draft)
		want []string // "field:code" for each problem, in field order
	}{
		{"valid sms", func(d *Draft) {}, nil},
		{"valid email", func(d *Draft) { d.Channel, d.To = ChannelEmail, "ayse@example.com" }, nil},
		{"valid push", func(d *Draft) { d.Channel, d.To = ChannelPush, "fcm:APA91bH-x_y.z" }, nil},

		// channel
		{"empty channel", func(d *Draft) { d.Channel = "" }, []string{"channel:required"}},
		{"unknown channel", func(d *Draft) { d.Channel = "fax" }, []string{"channel:invalid"}},
		{"unknown channel still requires a recipient", func(d *Draft) { d.Channel, d.To = "fax", "" },
			[]string{"channel:invalid", "to:required"}},

		// sms recipient: E.164
		{"sms without plus", func(d *Draft) { d.To = "905551234567" }, []string{"to:invalid"}},
		{"sms in national format", func(d *Draft) { d.To = "05551234567" }, []string{"to:invalid"}},
		{"sms country code starting with zero", func(d *Draft) { d.To = "+05551234567" }, []string{"to:invalid"}},
		{"sms with spaces", func(d *Draft) { d.To = "+90 555 123 45 67" }, []string{"to:invalid"}},
		{"sms with 15 digits", func(d *Draft) { d.To = "+123456789012345" }, nil},
		{"sms with 16 digits", func(d *Draft) { d.To = "+1234567890123456" }, []string{"to:invalid"}},
		{"sms recipient that is an e-mail address", func(d *Draft) { d.To = "ayse@example.com" }, []string{"to:invalid"}},

		// email recipient: one bare address
		{"email with a display name", func(d *Draft) { d.Channel, d.To = ChannelEmail, "Ayşe <ayse@example.com>" },
			[]string{"to:invalid"}},
		{"email without at sign", func(d *Draft) { d.Channel, d.To = ChannelEmail, "ayse.example.com" },
			[]string{"to:invalid"}},
		{"email with surrounding spaces", func(d *Draft) { d.Channel, d.To = ChannelEmail, " ayse@example.com " },
			[]string{"to:invalid"}},
		{"email with two addresses", func(d *Draft) { d.Channel, d.To = ChannelEmail, "a@example.com, b@example.com" },
			[]string{"to:invalid"}},
		{"email at 254 characters", func(d *Draft) { d.Channel, d.To = ChannelEmail, emailOfLength(254) }, nil},
		{"email at 255 characters", func(d *Draft) { d.Channel, d.To = ChannelEmail, emailOfLength(255) },
			[]string{"to:too_long"}},

		// push recipient: an opaque device token
		{"push token with a space", func(d *Draft) { d.Channel, d.To = ChannelPush, "abc def" }, []string{"to:invalid"}},
		{"push token with a non-ASCII letter", func(d *Draft) { d.Channel, d.To = ChannelPush, "tökén" },
			[]string{"to:invalid"}},
		{"push token at 512 characters", func(d *Draft) { d.Channel, d.To = ChannelPush, strings.Repeat("a", 512) }, nil},
		{"push token at 513 characters", func(d *Draft) { d.Channel, d.To = ChannelPush, strings.Repeat("a", 513) },
			[]string{"to:too_long"}},

		// content: counted in characters, not bytes ("ş" is two bytes in UTF-8)
		{"empty content", func(d *Draft) { d.Content = "" }, []string{"content:required"}},
		{"blank content", func(d *Draft) { d.Content = " \n\t" }, []string{"content:required"}},
		{"content with a NUL character", func(d *Draft) { d.Content = "a\x00b" }, []string{"content:invalid"}},
		{"content that is not UTF-8", func(d *Draft) { d.Content = "a\xffb" }, []string{"content:invalid"}},
		{"sms content at 1600 characters", func(d *Draft) { d.Content = strings.Repeat("ş", 1600) }, nil},
		{"sms content at 1601 characters", func(d *Draft) { d.Content = strings.Repeat("ş", 1601) },
			[]string{"content:too_long"}},
		{"email content at 100000 characters", func(d *Draft) {
			d.Channel, d.To, d.Content = ChannelEmail, "ayse@example.com", strings.Repeat("ğ", 100_000)
		}, nil},
		{"email content at 100001 characters", func(d *Draft) {
			d.Channel, d.To, d.Content = ChannelEmail, "ayse@example.com", strings.Repeat("ğ", 100_001)
		}, []string{"content:too_long"}},
		{"push content at 1000 characters", func(d *Draft) {
			d.Channel, d.To, d.Content = ChannelPush, "token", strings.Repeat("ı", 1000)
		}, nil},
		{"push content at 1001 characters", func(d *Draft) {
			d.Channel, d.To, d.Content = ChannelPush, "token", strings.Repeat("ı", 1001)
		}, []string{"content:too_long"}},

		// priority
		{"empty priority", func(d *Draft) { d.Priority = "" }, []string{"priority:required"}},
		{"unknown priority", func(d *Draft) { d.Priority = "urgent" }, []string{"priority:invalid"}},

		{"every problem is reported at once", func(d *Draft) { *d = Draft{} },
			[]string{"channel:required", "to:required", "content:required", "priority:required"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := valid
			tt.edit(&d)
			if got := fieldCodes(t, d.Validate()); !slices.Equal(got, tt.want) {
				t.Errorf("Validate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDraftValidateReturnsNilWhenValid guards against returning a nil *ValidationError as an error, which is a
// non-nil interface holding a nil pointer, so err != nil would be true for a valid draft.
func TestDraftValidateReturnsNilWhenValid(t *testing.T) {
	d := Draft{To: "+905551234567", Channel: ChannelSMS, Content: "hi", Priority: PriorityLow}
	if err := d.Validate(); err != nil {
		t.Fatalf("Validate() = %#v, want nil", err)
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := Draft{Channel: ChannelSMS, To: "123", Content: "hi", Priority: PriorityHigh}.Validate()
	want := "invalid notification: to: must be an E.164 phone number such as +905551234567"
	if err == nil || err.Error() != want {
		t.Errorf("Error() = %v, want %q", err, want)
	}
}

// fieldCodes turns a validation error into "field:code" strings, so tests compare the stable codes rather than
// the wording of the messages.
func fieldCodes(t *testing.T, err error) []string {
	t.Helper()
	if err == nil {
		return nil
	}
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Validate() returned %T (%v), want *ValidationError", err, err)
	}
	codes := make([]string, 0, len(verr.Fields))
	for _, f := range verr.Fields {
		codes = append(codes, f.Field+":"+string(f.Code))
	}
	return codes
}

// emailOfLength returns an address of n characters (197 to 260) built like a real one: a short local part and
// domain labels of at most 63 characters, the limits RFC 5321 sets on each part.
func emailOfLength(n int) string {
	label := strings.Repeat("x", 63)
	prefix := "ayse@" + label + "." + label + "." + label + "."
	return prefix + strings.Repeat("x", n-len(prefix))
}
