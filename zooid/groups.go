package zooid

import (
	"fiatjaf.com/nostr"
)

func GetGroupIDFromEvent(event nostr.Event) string {
	tag := event.Tags.Find("h")

	if tag != nil {
		return tag[1]
	}

	return ""
}

type GroupStore struct {
	Config *Config
	Events *EventStore
}

func (g *GroupStore) GetMetadata(h string) nostr.Event {
	filter := nostr.Filter{
		Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
		Tags: nostr.TagMap{
			"d": []string{h},
		},
	}

	for event := range g.Events.QueryEvents(filter, 1) {
		return event
	}

	return nostr.Event{}
}

func (g *GroupStore) AddMember(h string, pubkey nostr.PubKey) error {
	event := nostr.Event{
		Kind:      nostr.KindSimpleGroupPutUser,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			nostr.Tag{"p", pubkey.Hex()},
			nostr.Tag{"h", h},
		},
	}

	return g.Events.SignAndSaveEvent(event, true)
}

func (g *GroupStore) RemoveMember(h string, pubkey nostr.PubKey) error {
	event := nostr.Event{
		Kind:      nostr.KindSimpleGroupRemoveUser,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			nostr.Tag{"p", pubkey.Hex()},
			nostr.Tag{"h", h},
		},
	}

	return g.Events.SignAndSaveEvent(event, true)
}

func (g *GroupStore) SetMetadataFromEvent(event nostr.Event) error {
	tags := nostr.Tags{}

	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "h" {
			tags = append(tags, nostr.Tag{"d", tag[1]})
		} else {
			tags = append(tags, tag)
		}
	}

	metadataEvent := nostr.Event{
		Kind:      nostr.KindSimpleGroupMetadata,
		CreatedAt: event.CreatedAt,
		Tags:      tags,
	}

	return g.Events.SignAndSaveEvent(metadataEvent, true)
}

func (g *GroupStore) DeleteGroup(h string) {
	filters := []nostr.Filter{
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

	for _, filter := range filters {
		for event := range g.Events.QueryEvents(filter, 0) {
			g.Events.DeleteEvent(event.ID)
		}
	}
}

func (g *GroupStore) IsMember(h string, pubkey nostr.PubKey) bool {
	filter := nostr.Filter{
		Kinds: []nostr.Kind{nostr.KindSimpleGroupPutUser, nostr.KindSimpleGroupRemoveUser},
		Tags: nostr.TagMap{
			"p": []string{pubkey.Hex()},
			"h": []string{h},
		},
	}

	for event := range g.Events.QueryEvents(filter, 1) {
		if event.Kind == nostr.KindSimpleGroupPutUser {
			return true
		}

		if event.Kind == nostr.KindSimpleGroupRemoveUser {
			return false
		}
	}

	return false
}

func (g *GroupStore) HasAccess(h string, pubkey nostr.PubKey) bool {
	if !HasTag(g.GetMetadata(h).Tags, "closed") {
		return true
	}

	return g.IsMember(h, pubkey)
}
