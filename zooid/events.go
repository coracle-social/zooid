package zooid

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/khatru"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nbd-wtf/go-nostr"
)

type EventStore struct {
	Relay        *khatru.Relay
	Config       *Config
	Schema       *Schema
	FTSAvailable bool
}

var _ eventstore.Store = (*EventStore)(nil)

func (events *EventStore) Init() error {
	// Create basic schema first
	basicSchema := events.Schema.Render(`
	CREATE TABLE IF NOT EXISTS {{.Name}}__events (
		id TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL,
		kind INTEGER NOT NULL,
		pubkey TEXT NOT NULL,
		content TEXT NOT NULL,
		tags TEXT NOT NULL,
		sig TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_events_created_at ON {{.Name}}__events(created_at);
	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_events_kind ON {{.Name}}__events(kind);
	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_events_pubkey ON {{.Name}}__events(pubkey);
	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_events_kind_pubkey ON {{.Name}}__events(kind, pubkey);
	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_events_kind_pubkey_created_at ON {{.Name}}__events(kind, pubkey, created_at DESC);

	CREATE TABLE IF NOT EXISTS {{.Name}}__event_tags (
		event_id TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		FOREIGN KEY (event_id) REFERENCES {{.Name}}__events(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_event_tags_event_id ON {{.Name}}__event_tags(event_id);
	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_event_tags_key ON {{.Name}}__event_tags(key);
	CREATE INDEX IF NOT EXISTS {{.Name}}__idx_event_tags_key_value ON {{.Name}}__event_tags(key, value);
	`)

	if _, err := GetDb().Exec(basicSchema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Try to create FTS5 schema - if it fails, continue without it
	ftsSchema := `
	CREATE VIRTUAL TABLE IF NOT EXISTS {{.Name}}__events_fts USING fts5(
		content,
		content='{{.Name}}__events',
		content_rowid='rowid'
	);

	CREATE TRIGGER IF NOT EXISTS {{.Name}}__events_ai AFTER INSERT ON {{.Name}}__events BEGIN
		INSERT INTO {{.Name}}__events_fts(rowid, content) VALUES (new.rowid, new.content);
	END;

	CREATE TRIGGER IF NOT EXISTS {{.Name}}__events_ad AFTER DELETE ON {{.Name}}__events BEGIN
		INSERT INTO {{.Name}}__events_fts({{.Name}}__events_fts, rowid, content)
		VALUES('delete', old.rowid, old.content);
	END;

	CREATE TRIGGER IF NOT EXISTS {{.Name}}__events_au AFTER UPDATE ON {{.Name}}__events BEGIN
		INSERT INTO {{.Name}}__events_fts({{.Name}}__events_fts, rowid, content)
		VALUES('delete', old.rowid, old.content);
		INSERT INTO {{.Name}}__events_fts(rowid, content)
		VALUES (new.rowid, new.content);
	END;
	`

	if _, err := GetDb().Exec(ftsSchema); err != nil {
		// FTS5 not available, continue without full-text search
		events.FTSAvailable = false
	} else {
		events.FTSAvailable = true
	}

	return nil
}

func (events *EventStore) Close() {
	// Never close the database, since it's a shared resource
}

func (events *EventStore) QueryEvents(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	ch := make(chan *nostr.Event)
	if filter.LimitZero {
		close(ch)
		return ch, nil
	}

	rows, err := events.buildSelectQuery(filter).RunWith(GetDb()).Query()
	if err != nil {
		close(ch)
		return nil, err
	}

	go func() {
		defer rows.Close()
		defer close(ch)

		for rows.Next() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			var (
				evt       nostr.Event
				tagsStr   string
				createdAt int64
				kind      int
			)

			err := rows.Scan(&evt.ID, &createdAt, &kind, &evt.PubKey, &evt.Content, &tagsStr, &evt.Sig)
			if err != nil {
				continue
			}

			evt.CreatedAt = nostr.Timestamp(createdAt)
			evt.Kind = kind

			if err := json.Unmarshal([]byte(tagsStr), &evt.Tags); err != nil {
				continue
			}

			select {
			case ch <- &evt:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (events *EventStore) buildSelectQuery(filter nostr.Filter) squirrel.SelectBuilder {
	qb := squirrel.Select("id", "created_at", "kind", "pubkey", "content", "tags", "sig").
		From(events.Schema.Prefix("events")).
		OrderBy("created_at DESC")

	// Handle search with FTS (if available)
	if filter.Search != "" && events.FTSAvailable {
		qb = qb.Join(events.Schema.Render("{{.Name}}__events_fts ON {{.Name}}__events.rowid = {{.Name}}__events_fts.rowid")).
			Where(squirrel.Eq{"events_fts": filter.Search})
	} else if filter.Search != "" {
		// Fallback to LIKE search if FTS not available
		qb = qb.Where(squirrel.Like{"content": "%" + filter.Search + "%"})
	}

	if len(filter.IDs) > 0 {
		idStrs := make([]interface{}, len(filter.IDs))
		for i, id := range filter.IDs {
			idStrs[i] = id
		}
		qb = qb.Where(squirrel.Eq{"id": idStrs})
	}

	if len(filter.Authors) > 0 {
		authorStrs := make([]interface{}, len(filter.Authors))
		for i, author := range filter.Authors {
			authorStrs[i] = author
		}
		qb = qb.Where(squirrel.Eq{"pubkey": authorStrs})
	}

	if len(filter.Kinds) > 0 {
		kindInts := make([]interface{}, len(filter.Kinds))
		for i, kind := range filter.Kinds {
			kindInts[i] = kind
		}
		qb = qb.Where(squirrel.Eq{"kind": kindInts})
	}

	if filter.Since != nil {
		qb = qb.Where(squirrel.GtOrEq{"created_at": *filter.Since})
	}

	if filter.Until != nil {
		qb = qb.Where(squirrel.LtOrEq{"created_at": *filter.Until})
	}

	for tagKey, tagValues := range filter.Tags {
		if len(tagValues) == 0 {
			continue
		}

		if len(tagKey) != 1 {
			continue
		}

		tagValueInterfaces := make([]interface{}, len(tagValues))
		for i, tagValue := range tagValues {
			tagValueInterfaces[i] = tagValue
		}

		subQuery := squirrel.Select("event_id").
			From(events.Schema.Prefix("event_tags")).
			Where(squirrel.Eq{"key": tagKey}).
			Where(squirrel.Eq{"value": tagValueInterfaces})

		subQuerySql, subQueryArgs, _ := subQuery.ToSql()
		qb = qb.Where("id IN ("+subQuerySql+")", subQueryArgs...)
	}

	if filter.Limit > 0 {
		qb = qb.Limit(uint64(filter.Limit))
	}

	return qb
}

func (events *EventStore) deleteEventByID(id string) error {
	_, err := squirrel.Delete(events.Schema.Prefix("events")).Where(squirrel.Eq{"id": id}).RunWith(GetDb()).Exec()
	return err
}

func (events *EventStore) DeleteEvent(ctx context.Context, evt *nostr.Event) error {
	if evt == nil {
		return nil
	}
	return events.deleteEventByID(evt.ID)
}

func (events *EventStore) SaveEvent(ctx context.Context, evt *nostr.Event) error {
	if evt == nil {
		return nil
	}
	// Check if event already exists
	var existingID string
	qb := squirrel.Select("id").From(events.Schema.Prefix("events")).Where(squirrel.Eq{"id": evt.ID})
	err := qb.RunWith(GetDb()).QueryRow().Scan(&existingID)
	if err == nil {
		return eventstore.ErrDupEvent
	}

	// Serialize tags to JSON
	tagsJSON, err := json.Marshal(evt.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	// Insert the event
	insertQb := squirrel.Insert(events.Schema.Prefix("events")).
		Columns("id", "created_at", "kind", "pubkey", "content", "tags", "sig").
		Values(
			evt.ID,
			int64(evt.CreatedAt),
			evt.Kind,
			evt.PubKey,
			evt.Content,
			string(tagsJSON),
			evt.Sig,
		)

	_, err = insertQb.RunWith(GetDb()).Exec()

	if err != nil {
		return fmt.Errorf("failed to save event '%s': %w", evt.ID, err)
	}

	// Insert single-letter tags into event_tags table
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && len(tag[0]) == 1 {
			tagQb := squirrel.Insert(events.Schema.Prefix("event_tags")).
				Columns("event_id", "key", "value").
				Values(evt.ID, tag[0], tag[1])

			_, err := tagQb.RunWith(GetDb()).Exec()
			if err != nil {
				// Log error but don't fail the entire save operation
				continue
			}
		}
	}

	return nil
}

func (events *EventStore) ReplaceEvent(ctx context.Context, evt *nostr.Event) error {
	if evt == nil {
		return nil
	}
	filter := nostr.Filter{Kinds: []int{evt.Kind}, Authors: []string{evt.PubKey}, Limit: 1}
	if nostr.IsAddressableKind(evt.Kind) {
		filter.Tags = nostr.TagMap{"d": []string{evt.Tags.GetD()}}
	}

	shouldStore := true
	ch, err := events.QueryEvents(ctx, filter)
	if err != nil {
		return err
	}
	for previous := range ch {
		if previous == nil {
			continue
		}
		if previous.CreatedAt <= evt.CreatedAt {
			if err := events.DeleteEvent(ctx, previous); err != nil {
				return fmt.Errorf("failed to delete event for replacing: %w", err)
			}
		} else {
			shouldStore = false
		}
	}

	if shouldStore {
		if err := events.SaveEvent(ctx, evt); err != nil && err != eventstore.ErrDupEvent {
			return fmt.Errorf("failed to save: %w", err)
		}
	}

	return nil
}

func (events *EventStore) CountEvents(ctx context.Context, filter nostr.Filter) (int64, error) {
	// Build a count query based on the select query but with COUNT(*) instead
	qb := events.buildSelectQuery(filter)

	// Convert the select query to a count query
	countQb := squirrel.Select("COUNT(*)").FromSelect(qb, "subquery")

	var count int64
	err := countQb.RunWith(GetDb()).QueryRow().Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count events: %w", err)
	}

	return count, nil
}

// Non-eventstore methods

func (events *EventStore) StoreEvent(ctx context.Context, event *nostr.Event) error {
	if event == nil {
		return nil
	}

	if nostr.IsRegularKind(event.Kind) {
		if err := events.SaveEvent(ctx, event); err != nil && err != eventstore.ErrDupEvent {
			return err
		}

		return nil
	}

	return events.ReplaceEvent(ctx, event)
}

func (events *EventStore) SignAndStoreEvent(event *nostr.Event, broadcast bool) error {
	if err := events.Config.Sign(event); err != nil {
		return err
	}

	if err := events.StoreEvent(context.Background(), event); err != nil {
		return err
	}

	if broadcast {
		events.Relay.BroadcastEvent(event)
	}

	return nil
}

func (events *EventStore) GetOrCreateApplicationSpecificData(d string) nostr.Event {
	filter := nostr.Filter{
		Kinds: []int{nostr.KindApplicationSpecificData},
		Tags: nostr.TagMap{
			"d": []string{d},
		},
	}

	ch, err := events.QueryEvents(context.Background(), filter)
	if err == nil {
		if event := <-ch; event != nil {
			return *event
		}
	}

	return nostr.Event{
		Kind:      nostr.KindApplicationSpecificData,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			[]string{"d", d},
		},
	}
}

func (events *EventStore) GetOrCreateRelayMembersList() nostr.Event {
	filter := nostr.Filter{
		Kinds: []int{RELAY_MEMBERS},
	}

	ch, err := events.QueryEvents(context.Background(), filter)
	if err == nil {
		if event := <-ch; event != nil {
			return *event
		}
	}

	return nostr.Event{
		Kind:      RELAY_MEMBERS,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			[]string{"-"},
		},
	}
}
