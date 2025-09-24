package sqlite

import (
	"errors"
	"fiatjaf.com/nostr"
)

func (s *SqliteBackend) CountEvents(nostr.Filter) (uint32, error) {
	return 0, errors.New("not supported")
}
