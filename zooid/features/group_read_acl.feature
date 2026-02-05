Feature: Group Access Control - Read
  As a relay user
  I want appropriate read access to group messages
  So that private conversations stay private

  Background:
    Given a running relay with groups enabled

  # --- BC: Relay-level events always readable ---
  Scenario: Relay-level events with h="_" are always readable
    Given a relay admin list event exists with d tag "_"
    When any authenticated user queries group admins for d="_"
    Then the relay admin list event is returned

  # --- BC: Groups disabled = nothing readable ---
  Scenario: No group events readable when groups are disabled
    Given groups are disabled
    When a user queries for group events
    Then no group events are returned

  # --- EP: Public group, open policy ---
  Scenario: Non-member can read public group messages with open policy
    Given a public group "open-chat" exists without private tag
    And open policy is true
    And the user is not a member of "open-chat"
    When the user subscribes to events in "open-chat"
    Then the user receives messages from "open-chat"

  # --- EP: Public group, closed policy ---
  Scenario: Non-member cannot read public group messages without open policy
    Given a public group "members-chat" exists
    And open policy is false
    And the user is not a member of "members-chat"
    When the user subscribes to events in "members-chat"
    Then the user does NOT receive messages from "members-chat"

  # --- EP: Private group ---
  Scenario: Non-member cannot read private group messages
    Given a private group "secret-chat" exists with "private" tag
    And the user is not a member of "secret-chat"
    When the user subscribes to events in "secret-chat"
    Then the user does NOT receive messages

  Scenario: Member can read private group messages
    Given a private group "secret-chat" exists
    And the user is a member of "secret-chat"
    When the user subscribes to events in "secret-chat"
    Then the user receives messages

  Scenario: Admin can read private group messages without membership
    Given a private group "secret-chat" exists
    And the user is an admin but not a group member
    When the user subscribes to events in "secret-chat"
    Then the user receives messages

  # --- EP: Hidden group ---
  Scenario: Non-member cannot see hidden group metadata
    Given a hidden group "shadow-chat" exists with "hidden" tag
    And the user is not a member of "shadow-chat"
    When the user queries for group metadata of "shadow-chat"
    Then no metadata is returned

  Scenario: Non-member cannot read hidden group messages
    Given a hidden group "shadow-chat" exists
    And the user is not a member or admin
    When the user subscribes to events in "shadow-chat"
    Then no events are returned

  Scenario: Member can see hidden group metadata and messages
    Given a hidden group "shadow-chat" exists
    And the user is a member of "shadow-chat"
    When the user queries for group metadata and messages
    Then metadata and messages are returned

  # --- BC: Metadata and deletion events always readable (if not hidden) ---
  Scenario: Non-member can see public group metadata
    Given a public group "open-chat" exists
    And the user is not a member
    When the user queries for KindSimpleGroupMetadata of "open-chat"
    Then the metadata event is returned

  Scenario: Non-member can see group deletion event
    Given a group "deleted-chat" was deleted
    And the user is not a member
    When the user queries for KindSimpleGroupDeleteGroup of "deleted-chat"
    Then the deletion event is returned

  # --- EG: Group with no metadata ---
  Scenario: Events for a group with missing metadata are unreadable
    Given a group event references group "phantom" but no metadata exists
    When a user queries events for group "phantom"
    Then no events are returned

  # --- PC: Combined hidden + private ---
  Scenario: Hidden and private group is invisible to non-members
    Given a group exists with both "hidden" and "private" tags
    And the user is not a member
    When the user queries for any events related to this group
    Then no events of any kind are returned

  # --- SC: Internal events are filtered ---
  Scenario: Internal zooid events are not returned in queries
    Given a kind 30078 event with d tag "zooid/banned_pubkeys" exists
    When a user subscribes with filter kinds=[30078]
    Then the internal zooid event is NOT included in results

  # --- SC: Write-only events are filtered ---
  Scenario: Write-only events are not queryable
    Given RELAY_JOIN and RELAY_LEAVE events exist in the database
    When a user subscribes to all events
    Then those ephemeral events are NOT included in results
