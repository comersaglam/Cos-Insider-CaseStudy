package observability

import "testing"

func TestMaskRecipient(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"phone", "+905551234567", "+90***67"},
		{"email", "ayse@example.com", "a***@example.com"},
		{"email with a non-ASCII first letter", "ömer@example.com", "ö***@example.com"},
		{"push token", "fcm:APA91bH-x_y.z", "fcm***.z"},
		{"email without a local part", "@example.com", "***@example.com"},
		{"short value", "abc123", "***"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		if got := MaskRecipient(tt.in); got != tt.want {
			t.Errorf("%s: MaskRecipient(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}
