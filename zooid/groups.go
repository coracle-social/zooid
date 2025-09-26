package zooid

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
)

type GroupsStore struct {
  Host string
	Config *Config
	Schema *Schema
}

func (groups *GroupsStore) Init() error {
	schema := groups.Schema.Render(`
	CREATE TABLE IF NOT EXISTS {{.Prefix}}__groups (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		about TEXT NOT NULL,
		closed BOOLEAN NOT NULL,
		private BOOLEAN NOT NULL,
		last_metadata_update INTEGER,
		last_admins_update INTEGER,
		last_members_update INTEGER
	);

	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_groups_id ON {{.Prefix}}__groups(id);

	CREATE TABLE IF NOT EXISTS {{.Prefix}}__group_members (
  	id TEXT PRIMARY KEY,
		group_id TEXT NOT NULL,
		pubkey TEXT NOT NULL,
		FOREIGN KEY (group_id) REFERENCES {{.Prefix}}__groups(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_group_members_group_id ON {{.Prefix}}__group_members(group_id);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_group_members_pubkey ON {{.Prefix}}__group_members(pubkey);
	`)

	if _, err := GetDb().Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// Group CRUD

func (groups *GroupsStore) SelectGroups() squirrel.SelectBuilder {
	return squirrel.Select("id", "name", "about", "closed", "private", "last_metadata_update", "last_admins_update", "last_members_update").From(groups.Schema.Prefix("groups"))
}

func (groups *GroupsStore) QueryGroups(builder squirrel.SelectBuilder) []Group {
	rows, err := builder.RunWith(GetDb()).Query()
	if err != nil {
		return []Group{}
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var group Group
		var id string
		err := rows.Scan(&id, &group.Name, &group.About, &group.Closed, &group.Private, &group.LastMetadataUpdate, &group.LastAdminsUpdate, &group.LastMembersUpdate)
		if err != nil {
			continue
		}

		group.Address = nip29.GroupAddress{
  		ID: id
  		Relay: groups.Config.Host
		}

		groups = append(groups, group)
	}

	return groups
}

func (groups *GroupStore) PutGroup(group *nip29.Group) {
  // Insert, on duplicate update
}

func (groups *GroupStore) DeleteGroup(id string) {
  // Delete group
}

func (groups *GroupsStore) GetGroups() []nip29.Group {
	return groups.QueryGroups(groups.SelectGroups())
}

func (groups *GroupsStore) GetGroupByID(id string) (nip29.Group, bool) {
	groupList := groups.QueryGroups(groups.SelectGroups().Where(squirrel.Eq{"id": id}))

	return First(groupList), len(groupList) > 0
}

// Group Utils

func (groups *GroupStore) MakeGroup(h string) *nip29.Group {
	qualifiedID := fmt.Sprintf("%s'%s", groups.Config.Host, h)
	group, err := nip29.NewGroup(qualifiedID)
	if err != nil {
		log.Printf("Failed to create group with qualified ID %s", qualifiedID)
		return nil
	}

	return &group
}

func (groups *GroupStore) GetGroupIDFromEvent(event *nostr.Event) string {
	hTag := event.Tags.GetFirst([]string{"h"})
	if hTag == nil {
		return ""
	}

	return hTag.Value()
}

func (groups *GroupStore) GetGroupFromEvent(event *nostr.Event) *nip29.Group {
  id = GetGroupIDFromEvent(event)

  if id == "" {
    return nil
  }

	return GetGroupByID(id)
}

func (groups *GroupStore) IsGroupMember(ctx context.Context, id string, pubkey string) bool {
	filter := nostr.Filter{
		Kinds: []int{nostr.KindSimpleGroupPutUser, nostr.KindSimpleGroupRemoveUser},
		Tags: nostr.TagMap{
			"p": []string{pubkey},
			"h": []string{id},
		},
	}

	events, err := GetBackend().QueryEvents(ctx, filter)

	if err != nil {
		log.Println(err)
	}

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

func HandleCreateGroup(event *nostr.Event) {
	group := MakeGroup(GetGroupIDFromEvent(event))

	if group != nil {
		PutGroup(group)
	}
}

func HandleEditMetadata(event *nostr.Event) {
	group := GetGroupFromEvent(event)

	if group == nil {
		group = MakeGroup(GetGroupIDFromEvent(event))
	}

	group.LastMetadataUpdate = event.CreatedAt
	group.Name = group.Address.ID

	if tag := event.Tags.GetFirst([]string{"name", ""}); tag != nil {
		group.Name = (*tag)[1]
	}
	if tag := event.Tags.GetFirst([]string{"about", ""}); tag != nil {
		group.About = (*tag)[1]
	}
	if tag := event.Tags.GetFirst([]string{"picture", ""}); tag != nil {
		group.Picture = (*tag)[1]
	}

	if tag := event.Tags.GetFirst([]string{"private"}); tag != nil {
		group.Private = true
	}
	if tag := event.Tags.GetFirst([]string{"closed"}); tag != nil {
		group.Closed = true
	}

	PutGroup(group)
}

func HandleDeleteGroup(event *nostr.Event) {
	ctx := context.Background()
	id := GetGroupIDFromEvent(event)

	DeleteGroup(id)

	hFilter := nostr.Filter{
		Tags: nostr.TagMap{
			"h": []string{id},
		},
	}

	hCh, err := GetBackend().QueryEvents(ctx, hFilter)
	if err != nil {
		log.Println(err)
	} else {
		for event := range hCh {
			DeleteEvent(ctx, event)
		}
	}

	dFilter := nostr.Filter{
		Tags: nostr.TagMap{
			"d": []string{id},
		},
	}

	dCh, err := GetBackend().QueryEvents(ctx, dFilter)
	if err != nil {
		log.Println(err)
	} else {
		for event := range dCh {
			DeleteEvent(ctx, event)
		}
	}
}

func GenerateGroupMetadataEvents(ctx context.Context, filter nostr.Filter) []*nostr.Event {
	result := make([]*nostr.Event, 0)

	for _, group := range ListGroups() {
		event := group.ToMetadataEvent()

		if !filter.Matches(event) {
			continue
		}

		if err := event.Sign(RELAY_SECRET); err != nil {
			log.Println("Failed to sign metadata event", err)
		} else {
			result = append(result, event)
		}
	}

	return result
}

func GenerateGroupAdminsEvents(ctx context.Context, filter nostr.Filter) []*nostr.Event {
	result := make([]*nostr.Event, 0)

	for _, group := range ListGroups() {
		event := nostr.Event{
			Kind:      nostr.KindSimpleGroupAdmins,
			CreatedAt: nostr.Now(),
			Tags: nostr.Tags{
				nostr.Tag{"d", group.Address.ID},
			},
		}

		for _, pubkey := range RELAY_ADMINS {
			event.Tags = append(event.Tags, nostr.Tag{"p", pubkey})
		}

		if !filter.Matches(&event) {
			continue
		}

		if err := event.Sign(RELAY_SECRET); err != nil {
			log.Println("Failed to sign admins event", err)
		} else {
			result = append(result, &event)
		}
	}

	return result
}

func MakePutUserEvent(event *nostr.Event) *nostr.Event {
	putUser := nostr.Event{
		Kind:      nostr.KindSimpleGroupPutUser,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			nostr.Tag{"p", event.PubKey},
			nostr.Tag{"h", GetGroupIDFromEvent(event)},
		},
	}

	if err := putUser.Sign(RELAY_SECRET); err != nil {
		log.Println(err)
	}

	return &putUser
}

func MakeRemoveUserEvent(event *nostr.Event) *nostr.Event {
	removeUser := nostr.Event{
		Kind:      nostr.KindSimpleGroupRemoveUser,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			nostr.Tag{"p", event.PubKey},
			nostr.Tag{"h", GetGroupIDFromEvent(event)},
		},
	}

	if err := removeUser.Sign(RELAY_SECRET); err != nil {
		log.Println(err)
	}

	return &removeUser
}
