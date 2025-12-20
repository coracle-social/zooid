package zooid

import (
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func TestGetGroupIDFromEvent(t *testing.T) {
	tests := []struct {
		name string
		tags nostr.Tags
		want string
	}{
		{
			name: "with h tag",
			tags: nostr.Tags{{"h", "group123"}},
			want: "group123",
		},
		{
			name: "without h tag",
			tags: nostr.Tags{{"p", "pubkey123"}},
			want: "",
		},
		{
			name: "empty tags",
			tags: nostr.Tags{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := nostr.Event{Tags: tt.tags}
			result := GetGroupIDFromEvent(event)
			if result != tt.want {
				t.Errorf("GetGroupIDFromEvent() = %v, want %v", result, tt.want)
			}
		})
	}
}
