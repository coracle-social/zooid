Feature: Pubkey & Event Banning
  As a relay admin
  I want to ban malicious users and events
  So that the community is protected

  Background:
    Given a running relay instance

  # --- ST: Pubkey ban state machine ---
  # [Allowed/Member] ->(BanPubkey)-> [Banned + Removed + Events Deleted]
  # [Banned] ->(AllowPubkey)-> [Allowed/Member + Unbanned]

  # --- UC: Ban pubkey ---
  Scenario: Banning a pubkey removes membership and deletes their events
    Given "userB" is a relay member
    And "userB" has published 5 events
    When an admin bans "userB" with reason "spam"
    Then "userB" is no longer a member
    And "userB" is on the banned list with reason "spam"
    And all 5 of "userB" events are deleted

  # --- UC: Allow pubkey ---
  Scenario: Allowing a pubkey adds membership and removes ban
    Given "userB" is banned
    When an admin allows "userB"
    Then "userB" is a member again
    And "userB" is no longer on the banned list

  # --- EG: Double ban ---
  Scenario: Banning an already-banned pubkey is idempotent
    Given "userB" is already banned
    When an admin bans "userB" again with reason "repeat offender"
    Then "userB" remains banned without duplicate entries

  # --- EG: Allow non-banned pubkey ---
  Scenario: Allowing a non-banned pubkey just adds membership
    Given "userB" is not banned and not a member
    When an admin allows "userB"
    Then "userB" is a member
    And no error occurs

  # --- SC: Ban list querying ---
  Scenario: Banned pubkeys list shows all banned users with reasons
    Given users "userA", "userB", "userC" are banned with reasons "spam", "abuse", "bot"
    When an admin queries the banned pubkeys list
    Then the list contains "userA", "userB", "userC" with their respective reasons

  # --- ST: Event ban state machine ---
  # [Normal] ->(BanEvent)-> [Banned + Deleted]
  # [Banned] ->(AllowEvent)-> [Unbanned]

  # --- UC: Ban event ---
  Scenario: Banning an event deletes it and records the ban
    Given event "abc123" exists in the relay
    When an admin bans event "abc123" with reason "illegal content"
    Then the event is deleted from storage
    And the event ID is on the banned events list

  # --- UC: Allow event ---
  Scenario: Allowing a banned event removes it from the ban list
    Given event "abc123" is banned
    When an admin allows event "abc123"
    Then the event is no longer on the banned list

  # --- BC: Check ban status ---
  Scenario: Unbanned pubkey returns false for PubkeyIsBanned
    Given "userX" has never been banned
    When checking if "userX" is banned
    Then the result is false

  Scenario: Unbanned event returns false for EventIsBanned
    Given event "xyz" has never been banned
    When checking if event "xyz" is banned
    Then the result is false
