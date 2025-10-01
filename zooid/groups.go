package zooid

import (
	"iter"

	"fiatjaf.com/nostr"
)

func GetGroupIDFromEvent(event nostr.Event) string {
	tag := event.Tags.Find("h")

	if tag != nil {
		return tag[1]
	}

	return ""
}

func MakeGroupMetadataFilter(h string) nostr.Filter {
	return nostr.Filter{
		Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
		Tags: nostr.TagMap{
			"d": []string{h},
		},
	}
}

func MakeGroupEventFilters(h string) []nostr.Filter {
	return []nostr.Filter{
		{
			Tags: nostr.TagMap{
				"d": []string{h},
			},
		},
		{
			Tags: nostr.TagMap{
				"h": []string{h},
			},
		},
	}
}

func MakeGroupMembershipCheckFilter(h string, pubkey nostr.PubKey) nostr.Filter {
	return nostr.Filter{
		Kinds: []nostr.Kind{nostr.KindSimpleGroupPutUser, nostr.KindSimpleGroupRemoveUser},
		Tags: nostr.TagMap{
			"p": []string{pubkey.Hex()},
			"h": []string{h},
		},
	}
}

func CheckGroupMembership(events iter.Seq[nostr.Event]) bool {
	for event := range events {
		if event.Kind == nostr.KindSimpleGroupPutUser {
			return true
		}

		if event.Kind == nostr.KindSimpleGroupRemoveUser {
			return false
		}
	}

	return false
}

func MakePutUserEvent(h string, pubkey nostr.PubKey) nostr.Event {
	return nostr.Event{
		Kind:      nostr.KindSimpleGroupPutUser,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			nostr.Tag{"p", pubkey.Hex()},
			nostr.Tag{"h", h},
		},
	}
}

func MakeRemoveUserEvent(h string, pubkey nostr.PubKey) nostr.Event {
	return nostr.Event{
		Kind:      nostr.KindSimpleGroupRemoveUser,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			nostr.Tag{"p", pubkey.Hex()},
			nostr.Tag{"h", h},
		},
	}
}

func MakeMetadataEvent(event nostr.Event) nostr.Event {
	tags := nostr.Tags{}

	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "h" {
			tags = append(tags, nostr.Tag{"d", tag[1]})
		} else {
			tags = append(tags, tag)
		}
	}

	return nostr.Event{
		Kind:      nostr.KindSimpleGroupMetadata,
		CreatedAt: event.CreatedAt,
		Tags:      tags,
	}
}
