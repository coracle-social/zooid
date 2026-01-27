package zooid

import (
	"encoding/json"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
	"slices"
)

// NIP-29 group invite kind
const KindSimpleGroupCreateInvite nostr.Kind = 9009

// isPrivateGroupContent checks if group creation content contains private:true
func isPrivateGroupContent(content string) bool {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return false
	}
	if private, ok := data["private"].(bool); ok {
		return private
	}
	return false
}

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

	// Parse content JSON and add appropriate visibility tags
	var contentData map[string]interface{}
	if err := json.Unmarshal([]byte(event.Content), &contentData); err == nil {
		if private, ok := contentData["private"].(bool); ok && private {
			tags = append(tags, nostr.Tag{"private"})
		}
		if closed, ok := contentData["closed"].(bool); ok && closed {
			tags = append(tags, nostr.Tag{"closed"})
		}
		if hidden, ok := contentData["hidden"].(bool); ok && hidden {
			tags = append(tags, nostr.Tag{"hidden"})
		}
	}

	metadataEvent := nostr.Event{
		Kind:      nostr.KindSimpleGroupMetadata,
		CreatedAt: event.CreatedAt,
		Tags:      tags,
		Content:   event.Content, // Include metadata JSON (name, about, picture, etc.)
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
			if event.Kind != nostr.KindSimpleGroupDeleteGroup {
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

	members := make(map[nostr.PubKey]struct{})

	for _, event := range Reversed(slices.Collect(g.Events.QueryEvents(filter, 0))) {
		for tag := range event.Tags.FindAll("p") {
			if pubkey, err := nostr.PubKeyFromHex(tag[1]); err == nil {
				if event.Kind == nostr.KindSimpleGroupPutUser {
					members[pubkey] = struct{}{}
				} else {
					delete(members, pubkey)
				}
			}
		}
	}

	return Keys(members)
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

// Invite Codes

// ValidateInviteCode checks if an invite code is valid for a group
func (g *GroupStore) ValidateInviteCode(h string, code string) bool {
	if code == "" {
		return false
	}

	filter := nostr.Filter{
		Kinds: []nostr.Kind{KindSimpleGroupCreateInvite},
		Tags: nostr.TagMap{
			"h": []string{h},
		},
	}

	for event := range g.Events.QueryEvents(filter, 0) {
		codeTag := event.Tags.Find("code")
		if codeTag != nil && len(codeTag) >= 2 && codeTag[1] == code {
			return true
		}
	}

	return false
}

// GetInviteCodeFromEvent extracts the invite code from an event's tags
func GetInviteCodeFromEvent(event nostr.Event) string {
	tag := event.Tags.Find("code")
	if tag != nil && len(tag) >= 2 {
		return tag[1]
	}
	return ""
}

// Other stuff

func (g *GroupStore) HasAccess(h string, pubkey nostr.PubKey) bool {
	return g.Config.CanManage(pubkey) || g.IsAdmin(h, pubkey) || g.IsMember(h, pubkey)
}

func (g *GroupStore) IsGroupEvent(event nostr.Event) bool {
	if slices.Contains(nip29.MetadataEventKinds, event.Kind) {
		return true
	}

	if slices.Contains(nip29.ModerationEventKinds, event.Kind) {
		return true
	}

	joinKinds := []nostr.Kind{
		nostr.KindSimpleGroupJoinRequest,
		nostr.KindSimpleGroupLeaveRequest,
	}

	if slices.Contains(joinKinds, event.Kind) {
		return true
	}

	return GetGroupIDFromEvent(event) != ""
}

func (g *GroupStore) CanRead(pubkey nostr.PubKey, event nostr.Event) bool {
	if !g.Config.Groups.Enabled {
		return false
	}

	h := GetGroupIDFromEvent(event)

	// Relay-level events (h="_") are always readable
	// This includes the relay admin list (GROUP_ADMINS with d="_")
	if h == "_" {
		return true
	}

	meta, found := g.GetMetadata(h)

	if !found {
		return false
	}

	if HasTag(meta.Tags, "hidden") && !g.HasAccess(h, pubkey) {
		return false
	}

	if event.Kind == nostr.KindSimpleGroupMetadata {
		return true
	}

	if event.Kind == nostr.KindSimpleGroupDeleteGroup {
		return true
	}

	// For private groups, require membership
	if HasTag(meta.Tags, "private") && !g.HasAccess(h, pubkey) {
		return false
	}

	// For public groups with open policy, allow all authenticated users to read
	if g.Config.Policy.Open && !HasTag(meta.Tags, "private") {
		return true
	}

	// Otherwise require group membership
	return g.HasAccess(h, pubkey)
}

func (g *GroupStore) CheckWrite(event nostr.Event) string {
	if !g.Config.Groups.Enabled {
		return "invalid: groups are not enabled"
	}

	if slices.Contains(nip29.MetadataEventKinds, event.Kind) {
		return "invalid: group metadata cannot be set directly"
	}

	h := GetGroupIDFromEvent(event)
	meta, found := g.GetMetadata(h)

	if event.Kind == nostr.KindSimpleGroupCreateGroup {
		if found {
			return "invalid: that group already exists"
		}
		// If admin_create_only is set, only admins can create groups
		if g.Config.Groups.AdminCreateOnly && !g.Config.CanManage(event.PubKey) {
			return "restricted: only admins can create groups"
		}
		// If private_admin_only is set, check if group is private
		if g.Config.Groups.PrivateAdminOnly && !g.Config.CanManage(event.PubKey) {
			if isPrivateGroupContent(event.Content) {
				return "restricted: only admins can create private groups"
			}
		}
		// Group creation check passed, don't apply general ModerationEventKinds check
		return ""
	} else if !found {
		return "invalid: group not found"
	}

	if slices.Contains(nip29.ModerationEventKinds, event.Kind) && !g.Config.CanManage(event.PubKey) {
		return "restricted: you are not authorized to manage groups"
	}

	// Handle join requests - check invite code for private/hidden groups
	if event.Kind == nostr.KindSimpleGroupJoinRequest {
		if g.IsMember(h, event.PubKey) {
			return "duplicate: already a member"
		}

		isPrivate := HasTag(meta.Tags, "private")
		isHidden := HasTag(meta.Tags, "hidden")

		// For private or hidden groups, require a valid invite code
		if isPrivate || isHidden {
			inviteCode := GetInviteCodeFromEvent(event)
			if !g.ValidateInviteCode(h, inviteCode) {
				if isHidden {
					// Don't reveal that the group exists
					return "invalid: group not found"
				}
				return "restricted: valid invite code required to join this group"
			}
		}

		return ""
	}

	// For non-join requests, hidden groups require access
	if HasTag(meta.Tags, "hidden") && !g.HasAccess(h, event.PubKey) {
		return "invalid: group not found"
	}

	if event.Kind == nostr.KindSimpleGroupLeaveRequest {
		if !g.IsMember(h, event.PubKey) {
			return "duplicate: not currently a member"
		} else {
			return ""
		}
	}

	if HasTag(meta.Tags, "closed") && !g.HasAccess(h, event.PubKey) {
		return "restricted: you are not a member of that group"
	}

	return ""
}

// Middleware

func (g *GroupStore) Enable(instance *Instance) {
	instance.Relay.Info.SupportedNIPs = append(instance.Relay.Info.SupportedNIPs, 29)
}
