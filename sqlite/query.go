package sqlite

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"iter"
	"strings"

	"fiatjaf.com/nostr"
)

func (s *SqliteBackend) QueryEvents(filter nostr.Filter, maxLimit int) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		s.RLock()
		defer s.RUnlock()

		if filter.LimitZero {
			return
		}

		limit := maxLimit
		if filter.Limit > 0 && filter.Limit < limit {
			limit = filter.Limit
		}

		query, args := s.buildSelectQuery(filter, limit)

		rows, err := s.db.Query(query, args...)
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

func (s *SqliteBackend) buildSelectQuery(filter nostr.Filter, limit int) (string, []interface{}) {
	var conditions []string
	var args []interface{}
	var joins []string

	baseQuery := "SELECT id, created_at, kind, pubkey, content, tags, sig FROM events"

	// Handle search with FTS (if available)
	if filter.Search != "" && s.FTSAvailable {
		joins = append(joins, "JOIN events_fts ON events.rowid = events_fts.rowid")
		conditions = append(conditions, "events_fts MATCH ?")
		args = append(args, filter.Search)
	} else if filter.Search != "" {
		// Fallback to LIKE search if FTS not available
		conditions = append(conditions, "content LIKE ?")
		args = append(args, "%"+filter.Search+"%")
	}

	// Add WHERE clause conditions
	if len(filter.IDs) > 0 {
		placeholders := make([]string, len(filter.IDs))
		for i, id := range filter.IDs {
			placeholders[i] = "?"
			args = append(args, id.Hex())
		}
		conditions = append(conditions, fmt.Sprintf("id IN (%s)", strings.Join(placeholders, ",")))
	}

	if len(filter.Authors) > 0 {
		placeholders := make([]string, len(filter.Authors))
		for i, author := range filter.Authors {
			placeholders[i] = "?"
			args = append(args, author.Hex())
		}
		conditions = append(conditions, fmt.Sprintf("pubkey IN (%s)", strings.Join(placeholders, ",")))
	}

	if len(filter.Kinds) > 0 {
		placeholders := make([]string, len(filter.Kinds))
		for i, kind := range filter.Kinds {
			placeholders[i] = "?"
			args = append(args, int(kind))
		}
		conditions = append(conditions, fmt.Sprintf("kind IN (%s)", strings.Join(placeholders, ",")))
	}

	if filter.Since != 0 {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, filter.Since)
	}

	if filter.Until != 0 {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, filter.Until)
	}

	// Handle tags - only filter single-letter tags using event_tags table
	for tagKey, tagValues := range filter.Tags {
		if len(tagValues) > 0 && len(tagKey) == 1 {
			placeholders := make([]string, len(tagValues))
			for i, tagValue := range tagValues {
				placeholders[i] = "?"
				args = append(args, tagValue)
			}
			conditions = append(conditions, fmt.Sprintf("id IN (SELECT event_id FROM event_tags WHERE key = ? AND value IN (%s))", strings.Join(placeholders, ",")))
			args = append(args, tagKey)
		}
	}

	// Build the complete query
	if len(joins) > 0 {
		baseQuery += " " + strings.Join(joins, " ")
	}

	if len(conditions) > 0 {
		baseQuery += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Order by created_at DESC for most recent first
	baseQuery += " ORDER BY created_at DESC"

	// Add limit
	if limit > 0 {
		baseQuery += " LIMIT ?"
		args = append(args, limit)
	}

	return baseQuery, args
}
