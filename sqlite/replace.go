package sqlite

import (
	"fmt"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
)

func (s *SqliteBackend) ReplaceEvent(evt nostr.Event) error {
	filter := nostr.Filter{Kinds: []nostr.Kind{evt.Kind}, Authors: []nostr.PubKey{evt.PubKey}}
	if evt.Kind.IsAddressable() {
		filter.Tags = nostr.TagMap{"d": []string{evt.Tags.GetD()}}
	}

	shouldStore := true
	for previous := range s.QueryEvents(filter, 1) {
		if previous.CreatedAt <= evt.CreatedAt {
			if err := s.DeleteEvent(previous.ID); err != nil {
				return fmt.Errorf("failed to delete event for replacing: %w", err)
			}
		} else {
			shouldStore = false
		}
	}

	if shouldStore {
		if err := s.SaveEvent(evt); err != nil && err != eventstore.ErrDupEvent {
			return fmt.Errorf("failed to save: %w", err)
		}
	}

	return nil
}
