Feature: Group Access Control - Write
  As a relay user
  I want appropriate write restrictions on groups
  So that unauthorized users cannot post or moderate

  Background:
    Given a running relay with groups enabled

  # --- BC: Metadata cannot be set directly ---
  Scenario: Users cannot publish group metadata events directly
    Given the user is an admin
    When the user publishes a KindSimpleGroupMetadata kind=39000 event
    Then the event is rejected with "metadata cannot be set directly"

  # --- EP: Valid moderation by admin ---
  Scenario: Admin can publish moderation events
    Given a group "mod-group" exists
    And the user is an admin
    When the user publishes a moderation event kind=9000 PutUser for "mod-group"
    Then the event is accepted

  # --- EP: Invalid moderation by non-admin ---
  Scenario: Non-admin cannot publish moderation events
    Given a group "mod-group" exists
    And the user is a member but not an admin
    When the user publishes a moderation event for "mod-group"
    Then the event is rejected with "not authorized to manage groups"

  # --- EP: Posting to non-existent group ---
  Scenario: Cannot post to non-existent group
    Given no group "ghost" exists
    When the user publishes a chat message with h="ghost"
    Then the event is rejected with "group not found"

  # --- BC: Closed group restrictions ---
  Scenario: Non-member cannot post to closed group
    Given a closed group "exclusive" exists with "closed" tag
    And the user is not a member of "exclusive"
    When the user publishes a chat message to "exclusive"
    Then the event is rejected with "you are not a member"

  Scenario: Member can post to closed group
    Given a closed group "exclusive" exists
    And the user is a member of "exclusive"
    When the user publishes a chat message to "exclusive"
    Then the event is accepted

  # --- BC: Hidden group write restrictions ---
  Scenario: Non-member posting to hidden group gets "group not found"
    Given a hidden group "stealth" exists
    And the user is not a member or admin
    When the user publishes a chat message to "stealth"
    Then the event is rejected with "group not found"

  # --- BC: Open non-closed group ---
  Scenario: Member can post to open group
    Given a public non-closed group "lounge" exists
    And the user is a member
    When the user publishes a chat message to "lounge"
    Then the event is accepted

  # --- CC: OnEvent conditions ---
  Scenario: Unauthenticated user cannot publish any event
    Given the user is not authenticated
    When the user publishes any event
    Then the event is rejected with "auth-required"

  Scenario: User cannot publish event as another user
    Given the user is authenticated as pubkey A
    When the user publishes an event with a different pubkey B
    Then the event is rejected with "cannot publish events on behalf of others"

  # --- BC: Banned event check ---
  Scenario: Banned event ID cannot be re-published
    Given event with ID "deadbeef..." has been banned
    When a user publishes an event with that ID
    Then the event is rejected with "event has been banned"
