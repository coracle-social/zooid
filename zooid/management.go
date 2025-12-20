package zooid

import (
	"context"
	"fmt"

	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip86"
)

// Management store takes care of all nip 86 methods, as well as defining actions for internal use.
//
// The banned pubkeys list is a NIP 78 application-specific event, which keeps track of which pubkeys
// have been banned, independently of the members list. Banned events works the same way.
//
// Membership is implemented as defined here https://github.com/nostr-protocol/nips/pull/1079/files, using
// both membership lists and add/remove events.
//
// Actions like BanPubkey and AllowPubkey synchronize ban and membership lists. These should be called in most
// cases, unless you're trying to do something more advanced.
//
// All actions are idempotent, and won't do anything if conditions are already correct.

type ManagementStore struct {
	Config *Config
	Events *EventStore
}

// Banned events

func (m *ManagementStore) GetBannedEventItems() []nip86.IDReason {
	items := make([]nip86.IDReason, 0)
	for tag := range m.Events.GetOrCreateApplicationSpecificData(BANNED_EVENTS).Tags.FindAll("event") {
		items = append(items, nip86.IDReason{
			ID:     tag[1],
			Reason: tag[2],
		})
	}

	return items
}

func (m *ManagementStore) BanEvent(id string, reason string) error {
	if err := m.Events.deleteEventByID(id); err != nil {
		return err
	}

	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_EVENTS)
	event.Tags = append(event.Tags, nostr.Tag{"event", id, reason})

	return m.Events.SignAndStoreEvent(&event, false)
}

func (m *ManagementStore) AllowEvent(id string, reason string) error {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_EVENTS)
	event.Tags = Filter(event.Tags, func(t nostr.Tag) bool {
		return t[1] != id
	})

	return m.Events.SignAndStoreEvent(&event, false)
}

func (m *ManagementStore) EventIsBanned(id string) bool {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_EVENTS)
	tag := event.Tags.FindWithValue("event", id)

	return tag != nil
}

// Internal banned pubkeys list

func (m *ManagementStore) GetBannedPubkeyItems() []nip86.PubKeyReason {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_PUBKEYS)

	items := make([]nip86.PubKeyReason, 0)
	for tag := range event.Tags.FindAll("banned") {
		items = append(items, nip86.PubKeyReason{
			PubKey: tag[1],
			Reason: tag[2],
		})
	}

	return items
}

func (m *ManagementStore) AddBannedPubkey(pubkey string, reason string) error {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_PUBKEYS)

	if event.Tags.FindWithValue("banned", pubkey) == nil {
		event.Tags = append(event.Tags, nostr.Tag{"banned", pubkey, reason})

		if err := m.Events.SignAndStoreEvent(&event, false); err != nil {
			return err
		}
	}

	return nil
}

func (m *ManagementStore) RemoveBannedPubkey(pubkey string) error {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_PUBKEYS)

	if event.Tags.FindWithValue("banned", pubkey) != nil {
		event.Tags = Filter(event.Tags, func(t nostr.Tag) bool {
			return len(t) >= 2 && t[1] != pubkey
		})

		if err := m.Events.SignAndStoreEvent(&event, false); err != nil {
			return err
		}
	}

	return nil
}

func (m *ManagementStore) PubkeyIsBanned(pubkey string) bool {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_PUBKEYS)
	tag := event.Tags.FindWithValue("banned", pubkey)

	return tag != nil
}

// Admins

func (m *ManagementStore) IsAdmin(pubkey string) bool {
	return m.Config.IsOwner(pubkey) || m.Config.IsSelf(pubkey)
}

func (m *ManagementStore) GetAdmins() []string {
	members := make([]string, 0)

	members = append(members, m.Config.GetOwner())

	members = append(members, m.Config.GetSelf())

	for _, role := range m.Config.Roles {
		if role.CanManage {
			for _, pubkey := range role.Pubkeys {
				members = append(members, pubkey)
			}
		}
	}

	return members
}

// Membership

func (m *ManagementStore) GetMembers() []string {
	pubkeys := make([]string, 0)
	for tag := range m.Events.GetOrCreateRelayMembersList().Tags.FindAll("member") {
		pubkeys = append(pubkeys, tag[1])
	}

	return pubkeys
}

func (m *ManagementStore) IsMember(pubkey string) bool {
	return m.Events.GetOrCreateRelayMembersList().Tags.FindWithValue("member", pubkey) != nil
}

func (m *ManagementStore) AddMember(pubkey string) error {
	membersEvent := m.Events.GetOrCreateRelayMembersList()

	if membersEvent.Tags.FindWithValue("member", pubkey) == nil {
		addMemberEvent := nostr.Event{
			Kind:      RELAY_ADD_MEMBER,
			CreatedAt: nostr.Now(),
			Tags: nostr.Tags{
				[]string{"-"},
				[]string{"p", pubkey},
			},
		}

		if err := m.Events.SignAndStoreEvent(&addMemberEvent, true); err != nil {
			return err
		}

		membersEvent.Tags = append(membersEvent.Tags, nostr.Tag{"member", pubkey})

		if err := m.Events.SignAndStoreEvent(&membersEvent, true); err != nil {
			return err
		}
	}

	return nil
}

func (m *ManagementStore) RemoveMember(pubkey string) error {
	membersEvent := m.Events.GetOrCreateRelayMembersList()

	if membersEvent.Tags.FindWithValue("member", pubkey) != nil {
		removeMemberEvent := nostr.Event{
			Kind:      RELAY_REMOVE_MEMBER,
			CreatedAt: nostr.Now(),
			Tags: nostr.Tags{
				[]string{"-"},
				[]string{"p", pubkey},
			},
		}

		if err := m.Events.SignAndStoreEvent(&removeMemberEvent, true); err != nil {
			return err
		}

		membersEvent.Tags = Filter(membersEvent.Tags, func(t nostr.Tag) bool {
			return len(t) >= 2 && t[1] != pubkey
		})

		if err := m.Events.SignAndStoreEvent(&membersEvent, true); err != nil {
			return err
		}

	}

	return nil
}

// Banning

func (m *ManagementStore) BanPubkey(pubkey string, reason string) error {
	if err := m.RemoveMember(pubkey); err != nil {
		return err
	}

	if err := m.AddBannedPubkey(pubkey, reason); err != nil {
		return err
	}

	filter := nostr.Filter{
		Authors: []string{pubkey},
	}

	ch, err := m.Events.QueryEvents(context.Background(), filter)
	if err != nil {
		return nil
	}
	for event := range ch {
		if event != nil {
			_ = m.Events.DeleteEvent(context.Background(), event)
		}
	}

	return nil
}

// Allowing

func (m *ManagementStore) GetAllowedPubkeyItems() []nip86.PubKeyReason {
	reasons := make([]nip86.PubKeyReason, 0)

	reasons = append(reasons, nip86.PubKeyReason{
		PubKey: m.Config.GetOwner(),
		Reason: "relay owner",
	})

	reasons = append(reasons, nip86.PubKeyReason{
		PubKey: m.Config.GetSelf(),
		Reason: "relay self",
	})

	for name, role := range m.Config.Roles {
		for _, pubkey := range role.Pubkeys {
			reasons = append(reasons, nip86.PubKeyReason{
				PubKey: pubkey,
				Reason: fmt.Sprintf("assigned to %s role", name),
			})
		}
	}

	for tag := range m.Events.GetOrCreateRelayMembersList().Tags.FindAll("member") {
		reasons = append(
			reasons,
			nip86.PubKeyReason{
				PubKey: tag[1],
				Reason: "relay member",
			},
		)
	}

	return reasons
}

func (m *ManagementStore) AllowPubkey(pubkey string) error {
	if err := m.AddMember(pubkey); err != nil {
		return err
	}

	if err := m.RemoveBannedPubkey(pubkey); err != nil {
		return err
	}

	return nil
}

// Joining

func (m *ManagementStore) ValidateJoinRequest(event *nostr.Event) (reject bool, msg string) {
	if event == nil {
		return true, "invalid: missing event"
	}
	if m.PubkeyIsBanned(event.PubKey) {
		return true, "invalid: you have been banned from this relay"
	}

	claimTag := event.Tags.Find("claim")

	if claimTag == nil {
		return true, "invalid: no claim tag"
	}

	filter := nostr.Filter{
		Kinds: []int{RELAY_INVITE},
	}

	ch, queryErr := m.Events.QueryEvents(context.Background(), filter)
	if queryErr == nil {
		for evt := range ch {
			if evt == nil {
				continue
			}
			if evt.Tags.FindWithValue("claim", claimTag[1]) != nil {
				return false, ""
			}
		}
	}

	return true, "invalid: failed to validate invite code"
}

// Middleware

func (m *ManagementStore) Enable(instance *Instance) {
	instance.Relay.ManagementAPI.RejectAPICall = append(instance.Relay.ManagementAPI.RejectAPICall, func(ctx context.Context, mp nip86.MethodParams) (reject bool, msg string) {
		pubkey := khatru.GetAuthed(ctx)
		if pubkey == "" {
			return true, "blocked: please authenticate in order to manage this relay"
		}

		if !m.Config.CanManage(pubkey) {
			return true, "blocked: only relay admins can manage this relay."
		}

		return false, ""
	})

	instance.Relay.ManagementAPI.BanPubKey = func(ctx context.Context, pubkey string, reason string) error {
		return m.BanPubkey(pubkey, reason)
	}

	instance.Relay.ManagementAPI.AllowPubKey = func(ctx context.Context, pubkey string, reason string) error {
		return m.AllowPubkey(pubkey)
	}

	instance.Relay.ManagementAPI.ListBannedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) {
		return m.GetBannedPubkeyItems(), nil
	}

	instance.Relay.ManagementAPI.ListAllowedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) {
		return m.GetAllowedPubkeyItems(), nil
	}

	instance.Relay.ManagementAPI.BanEvent = func(ctx context.Context, id string, reason string) error {
		return m.BanEvent(id, reason)
	}

	instance.Relay.ManagementAPI.AllowEvent = func(ctx context.Context, id string, reason string) error {
		return m.AllowEvent(id, reason)
	}

	instance.Relay.ManagementAPI.ListBannedEvents = func(ctx context.Context) ([]nip86.IDReason, error) {
		return m.GetBannedEventItems(), nil
	}
}
