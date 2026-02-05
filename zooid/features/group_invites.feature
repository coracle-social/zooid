Feature: Group Invite Codes
  As an admin
  I want to create invite codes for private/hidden groups
  So that I can control who joins

  Background:
    Given a running relay with groups enabled

  # --- EP: Valid partition ---
  Scenario: User joins private group with valid invite code
    Given a private group "vault" exists
    And an invite code "XYZ789" exists for group "vault"
    And the user is authenticated and a relay member
    When the user sends a join request for "vault" with code "XYZ789"
    Then the join request is accepted

  # --- EP: Invalid partition ---
  Scenario: User cannot join private group without invite code
    Given a private group "vault" exists
    And the user is authenticated and a relay member
    When the user sends a join request for "vault" without a code
    Then the join request is rejected with "valid invite code required"

  Scenario: User cannot join private group with wrong invite code
    Given a private group "vault" exists
    And the user is authenticated and a relay member
    When the user sends a join request for "vault" with code "WRONG"
    Then the join request is rejected with "valid invite code required"

  # --- BVA: Empty code ---
  Scenario: Empty invite code is rejected
    Given a private group "vault" exists
    And the user is authenticated and a relay member
    When the user sends a join request for "vault" with code ""
    Then the join request is rejected

  # --- BC: Hidden group uses "group not found" message ---
  Scenario: Invalid invite for hidden group hides group existence
    Given a hidden group "ghost" exists
    And the user is authenticated and a relay member
    When the user sends a join request for "ghost" with code "WRONG"
    Then the join request is rejected with "group not found"

  # --- EP: Public group does not require invite ---
  Scenario: Public group join does not require invite code
    Given a public group "plaza" exists without private or hidden tag
    And the user is authenticated and a relay member
    When the user sends a join request for "plaza" without a code
    Then the join request is accepted

  # --- UC: Admin generates invite ---
  Scenario: Admin with CanInvite queries for relay invite event
    Given the user has the CanInvite permission
    When the user subscribes with filter kinds=[28935]
    Then the relay generates and returns a RELAY_INVITE event
    And the invite has a "claim" tag with a random code
    And the invite has a "p" tag with the user's pubkey

  # --- ST: Invite code reuse ---
  Scenario: Same invite code can be queried again idempotently
    Given the user previously received an invite event
    When the user subscribes for invite events again
    Then the same invite event is returned without duplication

  # --- BC: Non-inviter cannot get invite ---
  Scenario: User without CanInvite does not receive invite events
    Given the user does NOT have CanInvite permission
    When the user subscribes with filter kinds=[28935]
    Then no invite events are returned

  # --- PW: Invite x Group type combinations ---
  Scenario: Private group requires valid invite
    Given a private group "vault" exists
    When a non-member sends a join request without code
    Then the result is rejected with "invite code required"

  Scenario: Hidden group requires valid invite and hides existence
    Given a hidden group "ghost" exists
    When a non-member sends a join request without code
    Then the result is rejected with "group not found"

  Scenario: Hidden group with valid code allows join
    Given a hidden group "ghost" exists
    And an invite code "SECRET" exists for group "ghost"
    When a non-member sends a join request with code "SECRET"
    Then the result is accepted
