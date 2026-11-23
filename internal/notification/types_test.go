package notification

import "testing"

func TestChannelValid(t *testing.T) {
	tests := []struct {
		channel Channel
		want    bool
	}{
		{ChannelSMS, true},
		{ChannelEmail, true},
		{ChannelPush, true},
		{"SMS", false},
		{"fax", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.channel.Valid(); got != tt.want {
			t.Errorf("Channel(%q).Valid() = %v, want %v", tt.channel, got, tt.want)
		}
	}
}

func TestPriorityValid(t *testing.T) {
	tests := []struct {
		priority Priority
		want     bool
	}{
		{PriorityHigh, true},
		{PriorityNormal, true},
		{PriorityLow, true},
		{"urgent", false},
		{"HIGH", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.priority.Valid(); got != tt.want {
			t.Errorf("Priority(%q).Valid() = %v, want %v", tt.priority, got, tt.want)
		}
	}
}

func TestPriorityRank(t *testing.T) {
	if PriorityHigh.Rank() <= PriorityNormal.Rank() || PriorityNormal.Rank() <= PriorityLow.Rank() || PriorityLow.Rank() == 0 {
		t.Errorf("ranks = %d %d %d, want high > normal > low > 0",
			PriorityHigh.Rank(), PriorityNormal.Rank(), PriorityLow.Rank())
	}
	if PriorityHigh.Rank() != MaxPriorityRank {
		t.Errorf("high ranks %d, want MaxPriorityRank %d", PriorityHigh.Rank(), MaxPriorityRank)
	}
}
