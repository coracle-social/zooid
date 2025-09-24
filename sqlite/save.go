package sqlite

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"github.com/Masterminds/squirrel"
)

func (s *SqliteBackend) SaveEvent(evt nostr.Event) error {
	// Check if event already exists
	var existingID string
	qb := squirrel.Select("id").From("events").Where(squirrel.Eq{"id": evt.ID.Hex()})
	err := qb.RunWith(s.db).QueryRow().Scan(&existingID)
	if err == nil {
		// Event already exists
		return eventstore.ErrDupEvent
	}

	// Serialize tags to JSON
	tagsJSON, err := json.Marshal(evt.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	// Insert the event
	insertQb := squirrel.Insert("events").
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

	_, err = insertQb.RunWith(s.db).Exec()

	if err != nil {
		return fmt.Errorf("failed to save event '%s': %w", evt.ID, err)
	}

	// Insert single-letter tags into event_tags table
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && len(tag[0]) == 1 {
			tagQb := squirrel.Insert("event_tags").
				Columns("event_id", "key", "value").
				Values(evt.ID.Hex(), tag[0], tag[1])

			_, err := tagQb.RunWith(s.db).Exec()
			if err != nil {
				// Log error but don't fail the entire save operation
				continue
			}
		}
	}

	return nil
}
