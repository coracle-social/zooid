Feature: NIP-86 Management API
  As a relay admin
  I want to manage the relay via the management API
  So that I can administer it remotely

  Background:
    Given a running relay with management enabled

  # --- DT: API access decision table ---
  Scenario: Unauthenticated user cannot access management API
    When an unauthenticated user calls any management API method
    Then the call is rejected with "please authenticate"

  Scenario: Non-admin authenticated user cannot access management API
    Given the user is authenticated but not an admin
    When the user calls any management API method
    Then the call is rejected with "only relay admins"

  Scenario: Admin can access management API
    Given the user is authenticated and is an admin
    When the user calls a management API method
    Then the call is accepted

  # --- UC: Change relay info ---
  Scenario: Admin changes relay name via management API
    Given the user is an admin
    When the user calls ChangeRelayName with "New Name"
    Then the relay name is updated to "New Name"
    And the config file is saved

  Scenario: Admin changes relay description
    Given the user is an admin
    When the user calls ChangeRelayDescription with "New desc"
    Then the relay description is updated

  Scenario: Admin changes relay icon
    Given the user is an admin
    When the user calls ChangeRelayIcon with "https://new-icon.png"
    Then the relay icon is updated

  # --- UC: Ban/Allow via API ---
  Scenario: Admin bans pubkey via management API
    Given the user is an admin
    When the user calls BanPubKey with a pubkey and reason
    Then the pubkey is banned

  Scenario: Admin allows pubkey via management API
    Given the user is an admin
    When the user calls AllowPubKey with a pubkey
    Then the pubkey is allowed

  Scenario: Admin bans event via management API
    Given the user is an admin
    When the user calls BanEvent with an event ID and reason
    Then the event is banned

  Scenario: Admin allows event via management API
    Given the user is an admin
    When the user calls AllowEvent with an event ID
    Then the event is unbanned

  # --- UC: List via API ---
  Scenario: Admin lists banned pubkeys
    Given the user is an admin
    When the user calls ListBannedPubKeys
    Then a list of banned pubkeys with reasons is returned

  Scenario: Admin lists allowed pubkeys (members)
    Given the user is an admin
    When the user calls ListAllowedPubKeys
    Then a list of relay members is returned
