package sqlite

import (
	"fiatjaf.com/nostr"
)

func (s *SqliteBackend) DeleteEvent(id nostr.ID) error {
	s.Lock()
	defer s.Unlock()

	_, err := s.db.Exec("DELETE FROM events WHERE id = ?", id.Hex())
	return err
}