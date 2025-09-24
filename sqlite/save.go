package sqlite

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
)

func (s *SqliteBackend) SaveEvent(evt nostr.Event) error {
	s.Lock()
	defer s.Unlock()

	// Check if event already exists
	var existingID string
	err := s.db.QueryRow("SELECT id FROM events WHERE id = ?", evt.ID.Hex()).Scan(&existingID)
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
	query := `INSERT INTO events (id, created_at, kind, pubkey, content, tags, sig)
              VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err = s.db.Exec(query,
		evt.ID.Hex(),
		int64(evt.CreatedAt),
		int(evt.Kind),
		evt.PubKey.Hex(),
		evt.Content,
		string(tagsJSON),
		hex.EncodeToString(evt.Sig[:]),
	)

	if err != nil {
		return fmt.Errorf("failed to save event '%s': %w", evt.ID, err)
	}

	// Insert single-letter tags into event_tags table
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && len(tag[0]) == 1 {
			_, err := s.db.Exec("INSERT INTO event_tags (event_id, key, value) VALUES (?, ?, ?)",
				evt.ID.Hex(), tag[0], tag[1])
			if err != nil {
				// Log error but don't fail the entire save operation
				continue
			}
		}
	}

	return nil
}
