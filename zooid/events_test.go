package zooid

import (
	"testing"

	"fiatjaf.com/nostr"
)

func createTestEventStore() *EventStore {
	schema := &Schema{Name: "test_" + RandomString(8)}
	return &EventStore{
		Schema: schema,
	}
}

func createTestEvent(kind nostr.Kind, content string) nostr.Event {
	secret := nostr.Generate()
	event := nostr.Event{
		Kind:      kind,
		CreatedAt: nostr.Now(),
		Content:   content,
		Tags:      nostr.Tags{{"t", "test"}, {"p", "testpubkey"}},
	}
	event.Sign(secret)
	return event
}

func TestEventStore_Init(t *testing.T) {
	store := createTestEventStore()

	err := store.Init()
	if err != nil {
		t.Errorf("EventStore.Init() error = %v", err)
	}
}

func TestEventStore_SaveEvent(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	event := createTestEvent(nostr.KindTextNote, "test content")

	err := store.SaveEvent(event)
	if err != nil {
		t.Errorf("EventStore.SaveEvent() error = %v", err)
	}

	// Try to save the same event again - should return duplicate error
	err = store.SaveEvent(event)
	if err == nil {
		t.Error("EventStore.SaveEvent() should return error for duplicate event")
	}
}

func TestEventStore_QueryEvents_Basic(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	event1 := createTestEvent(nostr.KindTextNote, "first event")
	event2 := createTestEvent(nostr.KindTextNote, "second event")

	store.SaveEvent(event1)
	store.SaveEvent(event2)

	// Query all events
	filter := nostr.Filter{}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 2 {
		t.Errorf("QueryEvents() returned %d events, want 2", len(events))
	}
}

func TestEventStore_QueryEvents_ByKind(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	textEvent := createTestEvent(nostr.KindTextNote, "text note")
	metadataEvent := createTestEvent(nostr.KindProfileMetadata, "metadata")

	store.SaveEvent(textEvent)
	store.SaveEvent(metadataEvent)

	// Query only text notes
	filter := nostr.Filter{Kinds: []nostr.Kind{nostr.KindTextNote}}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 1 {
		t.Errorf("QueryEvents() by kind returned %d events, want 1", len(events))
	}

	if events[0].Kind != nostr.KindTextNote {
		t.Errorf("QueryEvents() by kind returned wrong kind: got %v, want %v", events[0].Kind, nostr.KindTextNote)
	}
}

func TestEventStore_QueryEvents_ByAuthor(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	secret1 := nostr.Generate()
	secret2 := nostr.Generate()

	event1 := nostr.Event{Kind: nostr.KindTextNote, CreatedAt: nostr.Now(), Content: "from author 1"}
	event1.Sign(secret1)

	event2 := nostr.Event{Kind: nostr.KindTextNote, CreatedAt: nostr.Now(), Content: "from author 2"}
	event2.Sign(secret2)

	store.SaveEvent(event1)
	store.SaveEvent(event2)

	// Query by specific author
	filter := nostr.Filter{Authors: []nostr.PubKey{secret1.Public()}}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 1 {
		t.Errorf("QueryEvents() by author returned %d events, want 1", len(events))
	}

	if events[0].PubKey != secret1.Public() {
		t.Error("QueryEvents() by author returned wrong author")
	}
}

func TestEventStore_QueryEvents_ByIDs(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	event1 := createTestEvent(nostr.KindTextNote, "first event")
	event2 := createTestEvent(nostr.KindTextNote, "second event")

	store.SaveEvent(event1)
	store.SaveEvent(event2)

	// Query by specific ID
	filter := nostr.Filter{IDs: []nostr.ID{event1.ID}}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 1 {
		t.Errorf("QueryEvents() by ID returned %d events, want 1", len(events))
	}

	if events[0].ID != event1.ID {
		t.Error("QueryEvents() by ID returned wrong event")
	}
}

func TestEventStore_QueryEvents_ByTags(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	event1 := nostr.Event{
		Kind:      nostr.KindTextNote,
		CreatedAt: nostr.Now(),
		Content:   "tagged event",
		Tags:      nostr.Tags{{"t", "bitcoin"}, {"p", "testuser"}},
	}
	event1.Sign(nostr.Generate())

	event2 := nostr.Event{
		Kind:      nostr.KindTextNote,
		CreatedAt: nostr.Now(),
		Content:   "other event",
		Tags:      nostr.Tags{{"t", "nostr"}},
	}
	event2.Sign(nostr.Generate())

	store.SaveEvent(event1)
	store.SaveEvent(event2)

	// Query by tag
	filter := nostr.Filter{Tags: nostr.TagMap{"t": []string{"bitcoin"}}}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 1 {
		t.Errorf("QueryEvents() by tags returned %d events, want 1", len(events))
	}

	if events[0].ID != event1.ID {
		t.Error("QueryEvents() by tags returned wrong event")
	}

	// Test that non-single-character tags are ignored
	event3 := nostr.Event{
		Kind:      nostr.KindTextNote,
		CreatedAt: nostr.Now(),
		Content:   "event with multi-char tag",
		Tags:      nostr.Tags{{"title", "special"}, {"t", "ignored"}},
	}
	event3.Sign(nostr.Generate())
	store.SaveEvent(event3)

	// Query by multi-character tag key should ignore the tag filter and return all events
	// (because multi-character tags are skipped in buildSelectQuery)
	filter = nostr.Filter{Tags: nostr.TagMap{"title": []string{"special"}}}
	events = make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 3 {
		t.Errorf("QueryEvents() with multi-character tag key should ignore tag filter and return all 3 events, got %d", len(events))
	}
}

func TestEventStore_QueryEvents_TimeRange(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	oldTime := nostr.Timestamp(1000000)
	newTime := nostr.Timestamp(2000000)

	event1 := nostr.Event{Kind: nostr.KindTextNote, CreatedAt: oldTime, Content: "old event"}
	event1.Sign(nostr.Generate())

	event2 := nostr.Event{Kind: nostr.KindTextNote, CreatedAt: newTime, Content: "new event"}
	event2.Sign(nostr.Generate())

	store.SaveEvent(event1)
	store.SaveEvent(event2)

	// Query events since a certain time
	filter := nostr.Filter{Since: oldTime}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 2 {
		t.Errorf("QueryEvents() with Since returned %d events, want 2", len(events))
	}

	// Query events until a certain time
	filter = nostr.Filter{Until: oldTime}
	events = make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 1 {
		t.Errorf("QueryEvents() with Until returned %d events, want 1", len(events))
	}
}

func TestEventStore_QueryEvents_Limit(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	// Save multiple events
	for i := 0; i < 5; i++ {
		event := createTestEvent(nostr.KindTextNote, "event content")
		store.SaveEvent(event)
	}

	// Query with limit
	filter := nostr.Filter{Limit: 3}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 3 {
		t.Errorf("QueryEvents() with limit returned %d events, want 3", len(events))
	}
}

func TestEventStore_QueryEvents_LimitZero(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	event := createTestEvent(nostr.KindTextNote, "test event")
	store.SaveEvent(event)

	// Query with LimitZero true should return no events
	filter := nostr.Filter{LimitZero: true}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 0 {
		t.Errorf("QueryEvents() with LimitZero returned %d events, want 0", len(events))
	}
}

func TestEventStore_QueryEvents_Search(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	event1 := createTestEvent(nostr.KindTextNote, "this contains bitcoin")
	event2 := createTestEvent(nostr.KindTextNote, "this contains nostr")

	store.SaveEvent(event1)
	store.SaveEvent(event2)

	// Query by search term
	filter := nostr.Filter{Search: "bitcoin"}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	// Should find at least one event (exact result depends on FTS availability)
	if len(events) == 0 {
		t.Error("QueryEvents() with search should find at least one event")
	}

	// If we found events, make sure they contain the search term
	if len(events) > 0 {
		found := false
		for _, evt := range events {
			if evt.Content == "this contains bitcoin" {
				found = true
				break
			}
		}
		if !found {
			t.Error("QueryEvents() search did not return the expected event")
		}
	}
}

func TestEventStore_DeleteEvent(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	event := createTestEvent(nostr.KindTextNote, "to be deleted")
	store.SaveEvent(event)

	// Verify event exists
	filter := nostr.Filter{IDs: []nostr.ID{event.ID}}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 1 {
		t.Error("Event should exist before deletion")
	}

	// Delete the event
	err := store.DeleteEvent(event.ID)
	if err != nil {
		t.Errorf("DeleteEvent() error = %v", err)
	}

	// Verify event is deleted
	events = make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 0 {
		t.Error("Event should not exist after deletion")
	}
}

func TestEventStore_ReplaceEvent(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	secret := nostr.Generate()

	// Create initial addressable event
	event1 := nostr.Event{
		Kind:      nostr.KindProfileMetadata,
		CreatedAt: nostr.Timestamp(1000000),
		Content:   "initial content",
		Tags:      nostr.Tags{{"d", "profile"}},
	}
	event1.Sign(secret)

	err := store.ReplaceEvent(event1)
	if err != nil {
		t.Errorf("ReplaceEvent() error = %v", err)
	}

	// Create newer event to replace the first
	event2 := nostr.Event{
		Kind:      nostr.KindProfileMetadata,
		CreatedAt: nostr.Timestamp(2000000),
		Content:   "updated content",
		Tags:      nostr.Tags{{"d", "profile"}},
	}
	event2.Sign(secret)

	err = store.ReplaceEvent(event2)
	if err != nil {
		t.Errorf("ReplaceEvent() error = %v", err)
	}

	// Query events - should only have the newer one
	filter := nostr.Filter{Kinds: []nostr.Kind{nostr.KindProfileMetadata}, Authors: []nostr.PubKey{secret.Public()}}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 1 {
		t.Errorf("ReplaceEvent() should result in 1 event, got %d", len(events))
	}

	if events[0].Content != "updated content" {
		t.Errorf("ReplaceEvent() content = %q, want %q", events[0].Content, "updated content")
	}
}

func TestEventStore_ReplaceEvent_OlderEvent(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	secret := nostr.Generate()

	// Create newer event first
	event1 := nostr.Event{
		Kind:      nostr.KindProfileMetadata,
		CreatedAt: nostr.Timestamp(2000000),
		Content:   "newer content",
		Tags:      nostr.Tags{{"d", "profile"}},
	}
	event1.Sign(secret)

	store.ReplaceEvent(event1)

	// Try to replace with older event - should be ignored
	event2 := nostr.Event{
		Kind:      nostr.KindProfileMetadata,
		CreatedAt: nostr.Timestamp(1000000),
		Content:   "older content",
		Tags:      nostr.Tags{{"d", "profile"}},
	}
	event2.Sign(secret)

	err := store.ReplaceEvent(event2)
	if err != nil {
		t.Errorf("ReplaceEvent() with older event error = %v", err)
	}

	// Verify the newer event is still there
	filter := nostr.Filter{Kinds: []nostr.Kind{nostr.KindProfileMetadata}, Authors: []nostr.PubKey{secret.Public()}}
	events := make([]nostr.Event, 0)
	for evt := range store.QueryEvents(filter, 0) {
		events = append(events, evt)
	}

	if len(events) != 1 {
		t.Errorf("ReplaceEvent() with older event should keep newer event, got %d events", len(events))
	}

	if events[0].Content != "newer content" {
		t.Errorf("ReplaceEvent() with older event kept wrong content = %q, want %q", events[0].Content, "newer content")
	}
}

func TestEventStore_CountEvents(t *testing.T) {
	store := createTestEventStore()
	store.Init()

	// Save events with different kinds
	for i := 0; i < 3; i++ {
		event := createTestEvent(nostr.KindTextNote, "text note")
		store.SaveEvent(event)
	}
	for i := 0; i < 2; i++ {
		event := createTestEvent(nostr.KindProfileMetadata, "profile metadata")
		store.SaveEvent(event)
	}

	// Count all events
	filter := nostr.Filter{}
	count, err := store.CountEvents(filter)
	if err != nil {
		t.Errorf("CountEvents() error = %v", err)
	}

	if count != 5 {
		t.Errorf("CountEvents() = %d, want 5", count)
	}

	// Count by specific kind - should return less than 5
	filter = nostr.Filter{Kinds: []nostr.Kind{nostr.KindTextNote}}
	count, err = store.CountEvents(filter)
	if err != nil {
		t.Errorf("CountEvents() by kind error = %v", err)
	}

	if count != 3 {
		t.Errorf("CountEvents() by kind = %d, want 3", count)
	}

	// Count by another specific kind - should return less than 5
	filter = nostr.Filter{Kinds: []nostr.Kind{nostr.KindProfileMetadata}}
	count, err = store.CountEvents(filter)
	if err != nil {
		t.Errorf("CountEvents() by metadata kind error = %v", err)
	}

	if count != 2 {
		t.Errorf("CountEvents() by metadata kind = %d, want 2", count)
	}
}

func TestEventStore_Close(t *testing.T) {
	store := createTestEventStore()

	// Close should not panic or error
	store.Close()
}
