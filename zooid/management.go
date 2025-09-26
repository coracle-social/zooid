package zooid

import (
	"context"
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip86"
	"fmt"
	"github.com/Masterminds/squirrel"
)

type ManagementStore struct {
	Config *Config
	Schema *Schema
}

func (m *ManagementStore) Init() error {
	basicSchema := m.Schema.Render(`
	CREATE TABLE IF NOT EXISTS {{.Name}}__pubkeys (
		pubkey PRIMARY KEY NOT NULL,
		status TEXT NOT NULL,
		reason TEXT
	);

	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_pubkeys_pubkey ON {{.Name}}__pubkeys(pubkey);
	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_pubkeys_status ON {{.Name}}__pubkeys(status);

	CREATE TABLE IF NOT EXISTS {{.Name}}__events (
		id PRIMARY KEY NOT NULL,
		status TEXT NOT NULL,
		reason TEXT
	);

	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_events_id ON {{.Name}}__events(id);
	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_events_status ON {{.Name}}__events(status);
	`)

	if _, err := GetDb().Exec(basicSchema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// Banned/allowed pubkeys

type Nip86PubkeyInfo struct {
	Pubkey nostr.PubKey
	Status string
	Reason string
}

func (m *ManagementStore) SelectPubkeys() squirrel.SelectBuilder {
	return squirrel.Select("pubkey", "status", "reason").From(m.Schema.Prefix("pubkeys"))
}

func (m *ManagementStore) QueryPubkeys(builder squirrel.SelectBuilder) []Nip86PubkeyInfo {
	rows, err := builder.RunWith(GetDb()).Query()
	if err != nil {
		return []Nip86PubkeyInfo{}
	}
	defer rows.Close()

	var items []Nip86PubkeyInfo
	for rows.Next() {
		var item Nip86PubkeyInfo
		var pubkeyStr string
		err := rows.Scan(&pubkeyStr, &item.Status)
		if err != nil {
			continue
		}

		if pubkey, err := nostr.PubKeyFromHex(pubkeyStr); err == nil {
			item.Pubkey = pubkey
		} else {
			continue
		}

		items = append(items, item)
	}

	return items
}

func (m *ManagementStore) BanPubkey(pubkey nostr.PubKey, reason string) error {
	_, err := squirrel.Insert(m.Schema.Prefix("pubkeys")).
		Columns("pubkey", "status", "reason").
		Values(pubkey.Hex(), "banned", reason).
		Suffix("ON CONFLICT(pubkey) DO UPDATE SET status = excluded.status, reason = excluded.reason").
		RunWith(GetDb()).Exec()
	return err
}

func (m *ManagementStore) AllowPubkey(pubkey nostr.PubKey, reason string) error {
	_, err := squirrel.Delete(m.Schema.Prefix("pubkeys")).
		Where(squirrel.Eq{"pubkey": pubkey.Hex()}).
		RunWith(GetDb()).Exec()
	return err
}

func (m *ManagementStore) PubkeyHasStatus(pubkey nostr.PubKey, status string) bool {
	builder := m.SelectPubkeys().Where(squirrel.Eq{"pubkey": pubkey.Hex()})

	for _, item := range m.QueryPubkeys(builder) {
		if item.Status == status {
			return true
		}
	}

	return false
}

// Banned/allowed events

type Nip86EventInfo struct {
	ID     nostr.ID
	Status string
	Reason string
}

func (m *ManagementStore) SelectEvents() squirrel.SelectBuilder {
	return squirrel.Select("id", "status", "reason").From(m.Schema.Prefix("events"))
}

func (m *ManagementStore) QueryEvents(builder squirrel.SelectBuilder) []Nip86EventInfo {
	rows, err := builder.RunWith(GetDb()).Query()
	if err != nil {
		return []Nip86EventInfo{}
	}
	defer rows.Close()

	var items []Nip86EventInfo
	for rows.Next() {
		var item Nip86EventInfo
		var idStr string
		err := rows.Scan(&idStr, &item.Status, &item.Reason)
		if err != nil {
			continue
		}

		if id, err := nostr.IDFromHex(idStr); err == nil {
			item.ID = id
		} else {
			continue
		}

		items = append(items, item)
	}

	return items
}

func (m *ManagementStore) BanEvent(id nostr.ID, reason string) error {
	_, err := squirrel.Insert(m.Schema.Prefix("events")).
		Columns("id", "status", "reason").
		Values(id.Hex(), "banned", reason).
		Suffix("ON CONFLICT(id) DO UPDATE SET status = excluded.status, reason = excluded.reason").
		RunWith(GetDb()).Exec()
	return err
}

func (m *ManagementStore) AllowEvent(id nostr.ID, reason string) error {
	_, err := squirrel.Delete(m.Schema.Prefix("events")).
		Where(squirrel.Eq{"id": id.Hex()}).
		RunWith(GetDb()).Exec()
	return err
}

func (m *ManagementStore) EventHasStatus(id nostr.ID, status string) bool {
	builder := m.SelectEvents().Where(squirrel.Eq{"id": id.Hex()})

	for _, item := range m.QueryEvents(builder) {
		if item.Status == status {
			return true
		}
	}

	return false
}

// Middleware

func (m *ManagementStore) Enable(instance *Instance) {
	instance.Relay.ManagementAPI.OnAPICall = func(ctx context.Context, mp nip86.MethodParams) (reject bool, msg string) {
		pubkey, ok := khatru.GetAuthed(ctx)

		if ok && m.Config.CanManage(m.Config.GetRolesForPubkey(pubkey)) {
			return true, "blocked: only relay admins can manage this relay."
		}

		return false, ""
	}

	instance.Relay.ManagementAPI.BanPubKey = func(ctx context.Context, pubkey nostr.PubKey, reason string) error {
		filter := nostr.Filter{
			Authors: []nostr.PubKey{pubkey},
		}

		for event := range instance.Events.QueryEvents(filter, 1000000) {
			instance.Events.DeleteEvent(event.ID)
		}

		return m.BanPubkey(pubkey, reason)
	}

	instance.Relay.ManagementAPI.AllowPubKey = func(ctx context.Context, pubkey nostr.PubKey, reason string) error {
		return m.AllowPubkey(pubkey, reason)
	}

	instance.Relay.ManagementAPI.ListBannedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) {
		items := m.QueryPubkeys(m.SelectPubkeys().Where(squirrel.Eq{"status": "banned"}))
		reasons := make([]nip86.PubKeyReason, 0, len(items))

		for _, item := range items {
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

	instance.Relay.ManagementAPI.BanEvent = func(ctx context.Context, id nostr.ID, reason string) error {
		filter := nostr.Filter{
			IDs: []nostr.ID{id},
		}

		for event := range instance.Events.QueryEvents(filter, 1000000) {
			instance.Events.DeleteEvent(event.ID)
		}

		return m.BanEvent(id, reason)
	}

	instance.Relay.ManagementAPI.AllowEvent = func(ctx context.Context, id nostr.ID, reason string) error {
		return m.AllowEvent(id, reason)
	}

	instance.Relay.ManagementAPI.ListBannedEvents = func(ctx context.Context) ([]nip86.IDReason, error) {
		items := m.QueryEvents(m.SelectEvents().Where(squirrel.Eq{"status": "banned"}))
		reasons := make([]nip86.IDReason, 0, len(items))

		for _, item := range items {
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
