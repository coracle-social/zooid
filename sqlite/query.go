package sqlite

import (
	"encoding/hex"
	"encoding/json"
	"iter"

	"fiatjaf.com/nostr"
	"github.com/Masterminds/squirrel"
)

func (s *SqliteBackend) QueryEvents(filter nostr.Filter, maxLimit int) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		if filter.LimitZero {
			return
		}

		limit := maxLimit
		if filter.Limit > 0 && filter.Limit < limit {
			limit = filter.Limit
		}

		rows, err := s.buildSelectQuery(filter, limit).RunWith(s.db).Query()
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

func (s *SqliteBackend) buildSelectQuery(filter nostr.Filter, limit int) squirrel.SelectBuilder {
	qb := squirrel.Select("id", "created_at", "kind", "pubkey", "content", "tags", "sig").
		From("events").
		OrderBy("created_at DESC")

	// Handle search with FTS (if available)
	if filter.Search != "" && s.FTSAvailable {
		qb = qb.Join("events_fts ON events.rowid = events_fts.rowid").
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
				From("event_tags").
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
