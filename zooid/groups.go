package zooid

import (
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
	"slices"
)

// Utils

func GetGroupIDFromEvent(event nostr.Event) string {
	var tagName string

	if slices.Contains(nip29.MetadataEventKinds, event.Kind) {
		tagName = "d"
	} else {
		tagName = "h"
	}

	tag := event.Tags.Find(tagName)

	if tag != nil {
		return tag[1]
	}

	return ""
}

// Struct definition

type GroupStore struct {
	Config     *Config
	Events     *EventStore
	Management *ManagementStore
}

// Metadata

func (g *GroupStore) GetMetadata(h string) (nostr.Event, bool) {
	filter := nostr.Filter{
		Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
		Tags: nostr.TagMap{
			"d": []string{h},
		},
	}

	for event := range g.Events.QueryEvents(filter, 1) {
		return event, true
	}

	return nostr.Event{}, false
}

func (g *GroupStore) UpdateMetadata(event nostr.Event) error {
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

	return g.Events.SignAndStoreEvent(&metadataEvent, true)
}

// Deletion

func (g *GroupStore) DeleteGroup(h string) {
	filters := []nostr.Filter{
		{
			Kinds: nip29.MetadataEventKinds,
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
			if event.Kind != nostr.KindSimpleGroupDeleteEvent {
				g.Events.DeleteEvent(event.ID)
			}
		}
	}
}

// Admins

func (g *GroupStore) IsAdmin(h string, pubkey nostr.PubKey) bool {
	return g.Management.IsAdmin(pubkey)
}

func (g *GroupStore) GetAdmins(h string) []nostr.PubKey {
	return g.Management.GetAdmins()
}

func (g *GroupStore) UpdateAdminsList(h string) error {
	tags := nostr.Tags{
		nostr.Tag{"-"},
		nostr.Tag{"d", h},
	}

	for _, pubkey := range g.GetAdmins(h) {
		tags = append(tags, nostr.Tag{"p", pubkey.Hex()})
	}

	event := nostr.Event{
		Kind:      nostr.KindSimpleGroupAdmins,
		CreatedAt: nostr.Now(),
		Tags:      tags,
	}

	return g.Events.SignAndStoreEvent(&event, true)
}

// Membership

func (g *GroupStore) AddMember(h string, pubkey nostr.PubKey) error {
	event := nostr.Event{
		Kind:      nostr.KindSimpleGroupPutUser,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			nostr.Tag{"p", pubkey.Hex()},
			nostr.Tag{"h", h},
		},
	}

	return g.Events.SignAndStoreEvent(&event, true)
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

	return g.Events.SignAndStoreEvent(&event, true)
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

func (g *GroupStore) GetMembers(h string) []nostr.PubKey {
	filter := nostr.Filter{
		Kinds: []nostr.Kind{nostr.KindSimpleGroupPutUser, nostr.KindSimpleGroupRemoveUser},
		Tags: nostr.TagMap{
			"h": []string{h},
		},
	}

	members := make([]nostr.PubKey, 0)

	for event := range g.Events.QueryEvents(filter, 0) {
		for hex := range event.Tags.FindAll("p") {
			if pubkey, err := nostr.PubKeyFromHex(hex[1]); err != nil {
				if event.Kind == nostr.KindSimpleGroupPutUser {
					members = append(members, pubkey)
				} else {
					members = Remove(members, pubkey)
				}
			}
		}
	}

	return members
}

func (g *GroupStore) UpdateMembersList(h string) error {
	tags := nostr.Tags{
		nostr.Tag{"-"},
		nostr.Tag{"d", h},
	}

	for _, pubkey := range g.GetMembers(h) {
		tags = append(tags, nostr.Tag{"p", pubkey.Hex()})
	}

	event := nostr.Event{
		Kind:      nostr.KindSimpleGroupMembers,
		CreatedAt: nostr.Now(),
		Tags:      tags,
	}

	return g.Events.SignAndStoreEvent(&event, true)
}

// Other stuff

func (g *GroupStore) HasAccess(h string, pubkey nostr.PubKey) bool {
	meta, found := g.GetMetadata(h)

	if !found {
		return false
	}

	if !HasTag(meta.Tags, "closed") {
		return true
	}

	if g.IsAdmin(h, pubkey) {
		return true
	}

	if g.IsMember(h, pubkey) {
		return true
	}

	return false
}

// Middleware

func (g *GroupStore) Enable(instance *Instance) {
	instance.Relay.Info.SupportedNIPs = append(instance.Relay.Info.SupportedNIPs, 29)
}
