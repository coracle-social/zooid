package zooid

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"iter"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"github.com/Masterminds/squirrel"
	_ "github.com/mattn/go-sqlite3"
)

type EventStore struct {
	Schema       *Schema
	FTSAvailable bool
}

var _ eventstore.Store = (*EventStore)(nil)

func (events *EventStore) Init() error {
	// Create basic schema first
	basicSchema := events.Schema.Render(`
	CREATE TABLE IF NOT EXISTS {{.Prefix}}__events (
		id TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL,
		kind INTEGER NOT NULL,
		pubkey TEXT NOT NULL,
		content TEXT NOT NULL,
		tags TEXT NOT NULL,
		sig TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_events_created_at ON {{.Prefix}}__events(created_at);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_events_kind ON {{.Prefix}}__events(kind);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_events_pubkey ON {{.Prefix}}__events(pubkey);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_events_kind_pubkey ON {{.Prefix}}__events(kind, pubkey);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_events_kind_pubkey_created_at ON {{.Prefix}}__events(kind, pubkey, created_at DESC);

	CREATE TABLE IF NOT EXISTS {{.Prefix}}__event_tags (
		event_id TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		FOREIGN KEY (event_id) REFERENCES {{.Prefix}}__events(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_event_tags_event_id ON {{.Prefix}}__event_tags(event_id);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_event_tags_key ON {{.Prefix}}__event_tags(key);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_event_tags_key_value ON {{.Prefix}}__event_tags(key, value);
	`)

	if _, err := GetDb().Exec(basicSchema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Try to create FTS5 schema - if it fails, continue without it
	ftsSchema := `
	CREATE VIRTUAL TABLE IF NOT EXISTS {{.Prefix}}__events_fts USING fts5(
		content,
		content='{{.Prefix}}__events',
		content_rowid='rowid'
	);

	CREATE TRIGGER IF NOT EXISTS {{.Prefix}}__events_ai AFTER INSERT ON {{.Prefix}}__events BEGIN
		INSERT INTO {{.Prefix}}__events_fts(rowid, content) VALUES (new.rowid, new.content);
	END;

	CREATE TRIGGER IF NOT EXISTS {{.Prefix}}__events_ad AFTER DELETE ON {{.Prefix}}__events BEGIN
		INSERT INTO {{.Prefix}}__events_fts({{.Prefix}}__events_fts, rowid, content)
		VALUES('delete', old.rowid, old.content);
	END;

	CREATE TRIGGER IF NOT EXISTS {{.Prefix}}__events_au AFTER UPDATE ON {{.Prefix}}__events BEGIN
		INSERT INTO {{.Prefix}}__events_fts({{.Prefix}}__events_fts, rowid, content)
		VALUES('delete', old.rowid, old.content);
		INSERT INTO {{.Prefix}}__events_fts(rowid, content)
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

func (events *EventStore) QueryEvents(filter nostr.Filter, maxLimit int) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		if filter.LimitZero {
			return
		}

		limit := maxLimit
		if filter.Limit > 0 && filter.Limit < limit {
			limit = filter.Limit
		}

		rows, err := events.buildSelectQuery(filter, limit).RunWith(GetDb()).Query()
		if err != nil {
			return
		}
		defer rows.Close()

		for rows.Next() {
			var evt nostr.Event
			var idStr, pubkeyStr, sigStr, tagsStr string
			var createdAt int64
			var kind int

			err := rows.Scan(&idStr, &createdAt, &kind, &pubkeyStr, &evt.Content, &tagsStr, &sigStr)
			if err != nil {
				continue
			}

			// Parse ID
			if id, err := nostr.IDFromHex(idStr); err == nil {
				evt.ID = id
			} else {
				continue
			}

			// Parse PubKey
			if pubkey, err := nostr.PubKeyFromHex(pubkeyStr); err == nil {
				evt.PubKey = pubkey
			} else {
				continue
			}

			// Parse Signature
			if sigBytes, err := hex.DecodeString(sigStr); err == nil && len(sigBytes) == 64 {
				copy(evt.Sig[:], sigBytes)
			} else {
				continue
			}

			// Set other fields
			evt.CreatedAt = nostr.Timestamp(createdAt)
			evt.Kind = nostr.Kind(kind)

			// Parse Tags
			if err := json.Unmarshal([]byte(tagsStr), &evt.Tags); err != nil {
				continue
			}

			if !yield(evt) {
				return
			}
		}
	}
}

func (events *EventStore) buildSelectQuery(filter nostr.Filter, limit int) squirrel.SelectBuilder {
	qb := squirrel.Select("id", "created_at", "kind", "pubkey", "content", "tags", "sig").
		From(events.Schema.Prefix("events")).
		OrderBy("created_at DESC")

	// Handle search with FTS (if available)
	if filter.Search != "" && events.FTSAvailable {
		qb = qb.Join(events.Schema.Render("{{.Prefix}}__events_fts ON {{.Prefix}}__events.rowid = {{.Prefix}}__events_fts.rowid")).
			Where(squirrel.Eq{"events_fts": filter.Search})
	} else if filter.Search != "" {
		// Fallback to LIKE search if FTS not available
		qb = qb.Where(squirrel.Like{"content": "%" + filter.Search + "%"})
	}

	if len(filter.IDs) > 0 {
		idStrs := make([]interface{}, len(filter.IDs))
		for i, id := range filter.IDs {
			idStrs[i] = id.Hex()
		}
		qb = qb.Where(squirrel.Eq{"id": idStrs})
	}

	if len(filter.Authors) > 0 {
		authorStrs := make([]interface{}, len(filter.Authors))
		for i, author := range filter.Authors {
			authorStrs[i] = author.Hex()
		}
		qb = qb.Where(squirrel.Eq{"pubkey": authorStrs})
	}

	if len(filter.Kinds) > 0 {
		kindInts := make([]interface{}, len(filter.Kinds))
		for i, kind := range filter.Kinds {
			kindInts[i] = int(kind)
		}
		qb = qb.Where(squirrel.Eq{"kind": kindInts})
	}

	if filter.Since != 0 {
		qb = qb.Where(squirrel.GtOrEq{"created_at": filter.Since})
	}

	if filter.Until != 0 {
		qb = qb.Where(squirrel.LtOrEq{"created_at": filter.Until})
	}

	for tagKey, tagValues := range filter.Tags {
		if len(tagValues) > 0 && len(tagKey) == 1 {
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
	}

	// Add limit
	if limit > 0 {
		qb = qb.Limit(uint64(limit))
	}

	return qb
}

func (events *EventStore) DeleteEvent(id nostr.ID) error {
	_, err := squirrel.Delete(events.Schema.Prefix("events")).Where(squirrel.Eq{"id": id.Hex()}).RunWith(GetDb()).Exec()

	return err
}

func (events *EventStore) SaveEvent(evt nostr.Event) error {
	// Check if event already exists
	var existingID string
	qb := squirrel.Select("id").From(events.Schema.Prefix("events")).Where(squirrel.Eq{"id": evt.ID.Hex()})
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
			evt.ID.Hex(),
			int64(evt.CreatedAt),
			int(evt.Kind),
			evt.PubKey.Hex(),
			evt.Content,
			string(tagsJSON),
			hex.EncodeToString(evt.Sig[:]),
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
				Values(evt.ID.Hex(), tag[0], tag[1])

			_, err := tagQb.RunWith(GetDb()).Exec()
			if err != nil {
				// Log error but don't fail the entire save operation
				continue
			}
		}
	}

	return nil
}

func (events *EventStore) ReplaceEvent(evt nostr.Event) error {
	filter := nostr.Filter{Kinds: []nostr.Kind{evt.Kind}, Authors: []nostr.PubKey{evt.PubKey}}
	if evt.Kind.IsAddressable() {
		filter.Tags = nostr.TagMap{"d": []string{evt.Tags.GetD()}}
	}

	shouldStore := true
	for previous := range events.QueryEvents(filter, 1) {
		if previous.CreatedAt <= evt.CreatedAt {
			if err := events.DeleteEvent(previous.ID); err != nil {
				return fmt.Errorf("failed to delete event for replacing: %w", err)
			}
		} else {
			shouldStore = false
		}
	}

	if shouldStore {
		if err := events.SaveEvent(evt); err != nil && err != eventstore.ErrDupEvent {
			return fmt.Errorf("failed to save: %w", err)
		}
	}

	return nil
}

func (events *EventStore) CountEvents(filter nostr.Filter) (uint32, error) {
	// Build a count query based on the select query but with COUNT(*) instead
	qb := events.buildSelectQuery(filter, 0)

	// Convert the select query to a count query
	countQb := squirrel.Select("COUNT(*)").FromSelect(qb, "subquery")

	var count uint32
	err := countQb.RunWith(GetDb()).QueryRow().Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count events: %w", err)
	}

	return count, nil
}
