package zooid

import (
	"context"
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip86"
	"fmt"
)

type ManagementStore struct {
	Config *Config
	Events *EventStore
}

// Banned pubkeys

type BannedPubkeyItem struct {
	Pubkey nostr.PubKey
	Reason string
}

func (m *ManagementStore) GetBannedPubkeyItems() []BannedPubkeyItem {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_PUBKEYS)

	items := make([]BannedPubkeyItem, 0)
	for tag := range event.Tags.FindAll("pubkey") {
		items = append(items, BannedPubkeyItem{
			Pubkey: nostr.MustPubKeyFromHex(tag[1]),
			Reason: tag[2],
		})
	}

	return items
}

func (m *ManagementStore) GetBannedPubkeys() []nostr.PubKey {
	pubkeys := make([]nostr.PubKey, 0)
	for _, item := range m.GetBannedPubkeyItems() {
		pubkeys = append(pubkeys, item.Pubkey)
	}

	return pubkeys
}

func (m *ManagementStore) BanPubkey(pubkey nostr.PubKey, reason string) error {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_PUBKEYS)
	event.Tags = append(event.Tags, nostr.Tag{"pubkey", pubkey.Hex(), reason})

	return m.Events.SaveEvent(event)
}

func (m *ManagementStore) AllowPubkey(pubkey nostr.PubKey, reason string) error {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_PUBKEYS)
	event.Tags = Filter(event.Tags, func(t nostr.Tag) bool {
		return t[1] != pubkey.Hex()
	})

	return m.Events.SaveEvent(event)
}

func (m *ManagementStore) PubkeyIsBanned(pubkey nostr.PubKey) bool {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_PUBKEYS)
	tag := event.Tags.FindWithValue("pubkey", pubkey.Hex())

	return tag != nil
}

// Banned events

type BannedEventItem struct {
	ID     nostr.ID
	Reason string
}

func (m *ManagementStore) GetBannedEventItems() []BannedEventItem {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_EVENTS)

	items := make([]BannedEventItem, 0)
	for tag := range event.Tags.FindAll("event") {
		items = append(items, BannedEventItem{
			ID:     nostr.MustIDFromHex(tag[1]),
			Reason: tag[2],
		})
	}

	return items
}

func (m *ManagementStore) GetBannedEvents() []nostr.ID {
	ids := make([]nostr.ID, 0)
	for _, item := range m.GetBannedEventItems() {
		ids = append(ids, item.ID)
	}

	return ids
}

func (m *ManagementStore) BanEvent(id nostr.ID, reason string) error {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_EVENTS)
	event.Tags = append(event.Tags, nostr.Tag{"event", id.Hex(), reason})

	return m.Events.SaveEvent(event)
}

func (m *ManagementStore) AllowEvent(id nostr.ID, reason string) error {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_EVENTS)
	event.Tags = Filter(event.Tags, func(t nostr.Tag) bool {
		return t[1] == id.Hex()
	})

	return m.Events.SaveEvent(event)
}

func (m *ManagementStore) EventIsBanned(id nostr.ID) bool {
	event := m.Events.GetOrCreateApplicationSpecificData(BANNED_EVENTS)
	tag := event.Tags.FindWithValue("event", id.Hex())

	return tag != nil
}

// Middleware

func (m *ManagementStore) Enable(instance *Instance) {
	instance.Relay.ManagementAPI.OnAPICall = func(ctx context.Context, mp nip86.MethodParams) (reject bool, msg string) {
		pubkey, ok := khatru.GetAuthed(ctx)

		if ok && m.Config.CanManage(pubkey) {
			return true, "blocked: only relay admins can manage this relay."
		}

		return false, ""
	}

	instance.Relay.ManagementAPI.BanPubKey = func(ctx context.Context, pubkey nostr.PubKey, reason string) error {
		filter := nostr.Filter{
			Authors: []nostr.PubKey{pubkey},
		}

		for event := range instance.Events.QueryEvents(filter, 0) {
			instance.Events.DeleteEvent(event.ID)
		}

		return m.BanPubkey(pubkey, reason)
	}

	instance.Relay.ManagementAPI.AllowPubKey = func(ctx context.Context, pubkey nostr.PubKey, reason string) error {
		return m.AllowPubkey(pubkey, reason)
	}

	instance.Relay.ManagementAPI.ListBannedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) {
		reasons := make([]nip86.PubKeyReason, 0)
		for _, item := range m.GetBannedPubkeyItems() {
			reasons = append(
				reasons,
				nip86.PubKeyReason{
					PubKey: item.Pubkey,
					Reason: item.Reason,
				},
			)
		}

		return reasons, nil
	}

	instance.Relay.ManagementAPI.ListAllowedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) {
		reasons := make([]nip86.PubKeyReason, 0)

		reasons = append(reasons, nip86.PubKeyReason{
			PubKey: nostr.MustPubKeyFromHex(m.Config.Self.Pubkey),
			Reason: "relay owner",
		})

		reasons = append(reasons, nip86.PubKeyReason{
			PubKey: m.Config.Secret.Public(),
			Reason: "relay self",
		})

		for name, role := range m.Config.Roles {
			for _, pubkey := range role.Pubkeys {
				reasons = append(reasons, nip86.PubKeyReason{
					PubKey: nostr.MustPubKeyFromHex(pubkey),
					Reason: fmt.Sprintf("assigned to role: %s", name),
				})
			}
		}

		filter := nostr.Filter{
			Kinds: []nostr.Kind{AUTH_JOIN},
		}

		for event := range m.Events.QueryEvents(filter, 0) {
			reasons = append(
				reasons,
				nip86.PubKeyReason{
					PubKey: event.PubKey,
					Reason: "joined via invite code",
				},
			)
		}

		return reasons, nil
	}

	instance.Relay.ManagementAPI.BanEvent = func(ctx context.Context, id nostr.ID, reason string) error {
		instance.Events.DeleteEvent(id)

		return m.BanEvent(id, reason)
	}

	instance.Relay.ManagementAPI.AllowEvent = func(ctx context.Context, id nostr.ID, reason string) error {
		return m.AllowEvent(id, reason)
	}

	instance.Relay.ManagementAPI.ListBannedEvents = func(ctx context.Context) ([]nip86.IDReason, error) {
		reasons := make([]nip86.IDReason, 0)
		for _, item := range m.GetBannedEventItems() {
			reasons = append(
				reasons,
				nip86.IDReason{
					ID:     item.ID.Hex(),
					Reason: item.Reason,
				},
			)
		}

		return reasons, nil
	}
}
