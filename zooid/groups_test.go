package zooid

import (
	"testing"

	"fiatjaf.com/nostr"
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

func TestMakeGroupMetadataFilter(t *testing.T) {
	h := "group123"
	filter := MakeGroupMetadataFilter(h)

	if len(filter.Kinds) != 1 || filter.Kinds[0] != nostr.KindSimpleGroupMetadata {
		t.Errorf("MakeGroupMetadataFilter() kinds = %v, want [%v]", filter.Kinds, nostr.KindSimpleGroupMetadata)
	}

	if filter.Tags["a"][0] != h {
		t.Errorf("MakeGroupMetadataFilter() tags a = %v, want %v", filter.Tags["a"], h)
	}
}

func TestMakeGroupEventFilters(t *testing.T) {
	h := "group123"
	filters := MakeGroupEventFilters(h)

	if len(filters) != 2 {
		t.Errorf("MakeGroupEventFilters() length = %v, want 2", len(filters))
	}

	if filters[0].Tags["a"][0] != h {
		t.Errorf("MakeGroupEventFilters() first filter tag a = %v, want %v", filters[0].Tags["a"], h)
	}

	if filters[1].Tags["h"][0] != h {
		t.Errorf("MakeGroupEventFilters() second filter tag h = %v, want %v", filters[1].Tags["h"], h)
	}
}

func TestMakeGroupMembershipCheckFilter(t *testing.T) {
	h := "group123"
	pubkey := nostr.MustPubKeyFromHex("1234567890123456789012345678901234567890123456789012345678901234")
	filter := MakeGroupMembershipCheckFilter(h, pubkey)

	expectedKinds := []nostr.Kind{nostr.KindSimpleGroupPutUser, nostr.KindSimpleGroupRemoveUser}
	if len(filter.Kinds) != 2 {
		t.Errorf("MakeGroupMembershipCheckFilter() kinds length = %v, want 2", len(filter.Kinds))
	}
	for i, kind := range expectedKinds {
		if filter.Kinds[i] != kind {
			t.Errorf("MakeGroupMembershipCheckFilter() kinds[%d] = %v, want %v", i, filter.Kinds[i], kind)
		}
	}

	if filter.Tags["p"][0] != pubkey.Hex() {
		t.Errorf("MakeGroupMembershipCheckFilter() tag p = %v, want %v", filter.Tags["p"], pubkey.Hex())
	}

	if filter.Tags["h"][0] != h {
		t.Errorf("MakeGroupMembershipCheckFilter() tag h = %v, want %v", filter.Tags["h"], h)
	}
}

func TestCheckGroupMembership(t *testing.T) {

	tests := []struct {
		name   string
		events []nostr.Event
		want   bool
	}{
		{
			name: "put user event",
			events: []nostr.Event{
				{Kind: nostr.KindSimpleGroupPutUser},
			},
			want: true,
		},
		{
			name: "remove user event",
			events: []nostr.Event{
				{Kind: nostr.KindSimpleGroupRemoveUser},
			},
			want: false,
		},
		{
			name:   "no events",
			events: []nostr.Event{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := func(yield func(nostr.Event) bool) {
				for _, event := range tt.events {
					if !yield(event) {
						return
					}
				}
			}
			result := CheckGroupMembership(seq)
			if result != tt.want {
				t.Errorf("CheckGroupMembership() = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestMakePutUserEvent(t *testing.T) {
	h := "group123"
	pubkey := nostr.MustPubKeyFromHex("1234567890123456789012345678901234567890123456789012345678901234")

	event := MakePutUserEvent(h, pubkey)

	if event.Kind != nostr.KindSimpleGroupPutUser {
		t.Errorf("MakePutUserEvent() kind = %v, want %v", event.Kind, nostr.KindSimpleGroupPutUser)
	}

	if event.CreatedAt == 0 {
		t.Error("MakePutUserEvent() should set CreatedAt")
	}

	pTag := event.Tags.Find("p")
	if pTag == nil || pTag[1] != pubkey.Hex() {
		t.Errorf("MakePutUserEvent() p tag = %v, want %v", pTag, pubkey.Hex())
	}

	hTag := event.Tags.Find("h")
	if hTag == nil || hTag[1] != h {
		t.Errorf("MakePutUserEvent() h tag = %v, want %v", hTag, h)
	}
}

func TestMakeRemoveUserEvent(t *testing.T) {
	h := "group123"
	pubkey := nostr.MustPubKeyFromHex("1234567890123456789012345678901234567890123456789012345678901234")

	event := MakeRemoveUserEvent(h, pubkey)

	if event.Kind != nostr.KindSimpleGroupRemoveUser {
		t.Errorf("MakeRemoveUserEvent() kind = %v, want %v", event.Kind, nostr.KindSimpleGroupRemoveUser)
	}

	if event.CreatedAt == 0 {
		t.Error("MakeRemoveUserEvent() should set CreatedAt")
	}

	pTag := event.Tags.Find("p")
	if pTag == nil || pTag[1] != pubkey.Hex() {
		t.Errorf("MakeRemoveUserEvent() p tag = %v, want %v", pTag, pubkey.Hex())
	}

	hTag := event.Tags.Find("h")
	if hTag == nil || hTag[1] != h {
		t.Errorf("MakeRemoveUserEvent() h tag = %v, want %v", hTag, h)
	}
}

func TestMakeMetadataEvent(t *testing.T) {
	originalEvent := nostr.Event{
		Kind:      nostr.KindSimpleGroupCreateGroup,
		CreatedAt: nostr.Timestamp(1234567890),
		Tags:      nostr.Tags{{"name", "Test Group"}},
	}

	metadataEvent := MakeMetadataEvent(originalEvent)

	if metadataEvent.Kind != nostr.KindSimpleGroupMetadata {
		t.Errorf("MakeMetadataEvent() kind = %v, want %v", metadataEvent.Kind, nostr.KindSimpleGroupMetadata)
	}

	if metadataEvent.CreatedAt != originalEvent.CreatedAt {
		t.Errorf("MakeMetadataEvent() CreatedAt = %v, want %v", metadataEvent.CreatedAt, originalEvent.CreatedAt)
	}

	if len(metadataEvent.Tags) != len(originalEvent.Tags) {
		t.Errorf("MakeMetadataEvent() tags length = %v, want %v", len(metadataEvent.Tags), len(originalEvent.Tags))
	}
}
