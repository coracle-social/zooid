Feature: Event Storage & Queries
  As the relay
  I want to persist and query Nostr events
  So that clients can reliably store and retrieve data

  Background:
    Given an initialized event store

  # --- EP: Save valid event ---
  Scenario: Save a new event successfully
    When a valid signed event is saved
    Then the event is retrievable by its ID

  # --- EP: Duplicate event ---
  Scenario: Saving a duplicate event returns ErrDupEvent
    Given an event "evt1" is already stored
    When the same event "evt1" is saved again
    Then the result is ErrDupEvent

  # --- LP: Query with no results ---
  Scenario: Query with non-matching filter returns empty
    When querying events with kind=99999
    Then no events are returned

  # --- EP: Query by kind ---
  Scenario: Query events filtered by kind
    Given events of kinds 1, 1, 0 are stored
    When querying with kinds=[1]
    Then exactly 2 events are returned
    And all returned events have kind=1

  # --- EP: Query by author ---
  Scenario: Query events filtered by author
    Given events from authors "alice" and "bob" are stored
    When querying with authors=["alice"]
    Then only events from author "alice" are returned

  # --- EP: Query by ID ---
  Scenario: Query events filtered by ID
    Given events with IDs "x", "y", "z" are stored
    When querying with IDs=["y"]
    Then exactly 1 event is returned with ID "y"

  # --- EP: Query by single-letter tags ---
  Scenario: Query events filtered by single-letter tag
    Given events with tags [["t","nostr"]] and [["t","bitcoin"]] are stored
    When querying with tags t=["nostr"]
    Then only the event with tag value "nostr" is returned

  # --- BC: Multi-character tag keys are ignored ---
  Scenario: Multi-character tag keys do not filter
    Given events with tag [["topic","nostr"]] are stored
    When querying with tags topic=["nostr"]
    Then all events are returned because filter is ignored

  # --- BVA: Time range queries ---
  Scenario: Query with Since returns events at or after timestamp
    Given events at timestamps 100, 200, 300 are stored
    When querying with Since=200
    Then events at timestamps 200 and 300 are returned

  Scenario: Query with Until returns events at or before timestamp
    Given events at timestamps 100, 200, 300 are stored
    When querying with Until=200
    Then events at timestamps 100 and 200 are returned

  Scenario: Query with Since=Until returns events at exact timestamp
    Given events at timestamps 100, 200, 300 are stored
    When querying with Since=200 and Until=200
    Then only the event at timestamp 200 is returned

  # --- BVA: Limit ---
  Scenario: Query with limit returns at most N events
    Given 10 events are stored
    When querying with limit=3
    Then exactly 3 events are returned

  Scenario: Query with LimitZero returns no events
    Given 5 events are stored
    When querying with LimitZero=true
    Then 0 events are returned

  Scenario: MaxLimit caps the query limit
    Given 100 events are stored
    When querying with limit=50 and maxLimit=10
    Then at most 10 events are returned

  # --- EP: Full-text search ---
  Scenario: Search by content keyword
    Given events with content "hello world" and "goodbye world" are stored
    When querying with search="hello"
    Then only the "hello world" event is returned

  # --- UC: Delete event ---
  Scenario: Deleted event is no longer queryable
    Given event "evt1" is stored
    When event "evt1" is deleted
    Then querying for "evt1" returns no results

  # --- UC: Replace event (addressable) ---
  Scenario: Newer addressable event replaces older one
    Given an addressable event kind=30000 d="test" at timestamp 100 is stored
    When a newer event kind=30000 d="test" at timestamp 200 is saved
    Then only the newer event is queryable
    And the older event is deleted

  Scenario: Older addressable event does not replace newer one
    Given an addressable event kind=30000 d="test" at timestamp 200 is stored
    When an older event kind=30000 d="test" at timestamp 100 is saved
    Then the newer event remains
    And the older event is not stored

  # --- UC: Replace event (replaceable, non-addressable) ---
  Scenario: Newer replaceable event kind=0 replaces older one
    Given a kind 0 event at timestamp 100 from author "alice" is stored
    When a kind 0 event at timestamp 200 from author "alice" is saved
    Then only the newer kind 0 event exists

  # --- UC: Count events ---
  Scenario: CountEvents returns correct counts with filters
    Given 3 kind-1 events and 2 kind-0 events are stored
    When counting all events
    Then the count is 5
    When counting with kinds=[1]
    Then the count is 3

  # --- LP: Tags indexing with multiple tags ---
  Scenario: Event with multiple single-letter tags is indexed for all
    Given an event with tags [["p","alice"],["e","ref1"],["t","nostr"]] is stored
    When querying with tags p=["alice"]
    Then the event is returned
    When querying with tags e=["ref1"]
    Then the event is returned
    When querying with tags t=["nostr"]
    Then the event is returned
