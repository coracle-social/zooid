package sqlite

import (
	"fiatjaf.com/nostr"
	"github.com/Masterminds/squirrel"
)

func (s *SqliteBackend) DeleteEvent(id nostr.ID) error {
	_, err := squirrel.Delete("events").Where(squirrel.Eq{"id": id.Hex()}).RunWith(s.db).Exec()

	return err
}
