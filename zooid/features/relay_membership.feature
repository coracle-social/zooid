Feature: Relay Membership
  As a Nostr user
  I want to join and leave the relay
  So that I can participate in the community

  Background:
    Given a running relay instance

  # --- ST: Membership state machine ---
  # States: [Anonymous] -> [Member] -> [Banned] -> [Allowed/Member]
  #                       [Member] -> [Left]   -> [Member]

  # --- UC: Join with public_join ---
  Scenario: User joins relay when public_join is enabled
    Given public_join is true
    And the user is authenticated
    When the user sends a RELAY_JOIN event
    Then the join request is accepted
    And the user becomes a relay member
    And a RELAY_ADD_MEMBER event is published

  # --- UC: Join with invite code ---
  Scenario: User joins relay with valid invite code when public_join is disabled
    Given public_join is false
    And the user is authenticated
    And an admin has generated an invite with claim code "ABC123"
    When the user sends a RELAY_JOIN event with claim tag "ABC123"
    Then the join request is accepted
    And the user becomes a relay member

  # --- EP: Invalid partition for join ---
  Scenario: User cannot join relay without invite when public_join is disabled
    Given public_join is false
    And the user is authenticated
    When the user sends a RELAY_JOIN event without a claim tag
    Then the join request is rejected with "no claim tag"

  Scenario: User cannot join relay with invalid invite code
    Given public_join is false
    And the user is authenticated
    When the user sends a RELAY_JOIN event with claim tag "INVALID"
    Then the join request is rejected with "failed to validate invite code"

  # --- ST: Already member tries to join again ---
  Scenario: Already-member user sends join request
    Given the user is already a relay member
    When the user sends a RELAY_JOIN event
    Then the join request is accepted idempotently

  # --- ST: Banned user tries to join ---
  Scenario: Banned user cannot join relay
    Given the user has been banned from the relay
    When the user sends a RELAY_JOIN event
    Then the join request is rejected with "banned"

  # --- UC: Leave relay ---
  Scenario: Member leaves relay
    Given the user is a relay member
    When the user sends a RELAY_LEAVE event
    Then the user is removed from the relay members list
    And a RELAY_REMOVE_MEMBER event is published

  # --- BC: Read-only events rejected on publish ---
  Scenario: Client cannot publish RELAY_ADD_MEMBER directly
    Given the user is authenticated and a member
    When the user publishes a kind 8000 event
    Then the event is rejected with "not accepted"

  Scenario: Client cannot publish RELAY_MEMBERS directly
    Given the user is authenticated and a member
    When the user publishes a kind 13534 event
    Then the event is rejected with "not accepted"

  # --- BC: Write-only events not queryable ---
  Scenario: Client cannot query RELAY_JOIN events
    Given the user is authenticated and a relay member
    When the user subscribes with filter kinds=[28934]
    Then RELAY_JOIN events are filtered out of results

  Scenario: Client cannot query RELAY_LEAVE events
    Given the user is authenticated and a relay member
    When the user subscribes with filter kinds=[28936]
    Then RELAY_LEAVE events are filtered out of results
