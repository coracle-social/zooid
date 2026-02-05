Feature: Configuration & Roles
  As a relay operator
  I want to configure roles and permissions
  So that different users have appropriate access

  # --- EP: Owner identification ---
  Scenario: Owner pubkey is correctly identified
    Given a config with owner pubkey "owner-hex"
    When checking IsOwner for "owner-hex"
    Then the result is true
    When checking IsOwner for "other-hex"
    Then the result is false

  # --- EP: Self identification ---
  Scenario: Relay's own pubkey is derived from secret
    Given a config with a secret key
    When checking IsSelf for the derived public key
    Then the result is true

  # --- DT: CanManage decision table ---
  Scenario: Owner can always manage
    Given a config with owner pubkey "owner-hex"
    When checking CanManage for "owner-hex"
    Then the result is true

  Scenario: Relay self can always manage
    Given a config with a secret key
    When checking CanManage for the relay self pubkey
    Then the result is true

  Scenario: User with CanManage role can manage
    Given a config with role "admin" having can_manage=true and pubkey "admin-hex"
    When checking CanManage for "admin-hex"
    Then the result is true

  Scenario: User without CanManage role cannot manage
    Given a config with role "editor" having can_manage=false and pubkey "editor-hex"
    When checking CanManage for "editor-hex"
    Then the result is false

  Scenario: User with no roles cannot manage
    Given a config with no roles for "random-hex"
    When checking CanManage for "random-hex"
    Then the result is false

  # --- DT: CanInvite decision table ---
  Scenario: Owner can always invite
    Given a config with owner pubkey "owner-hex"
    When checking CanInvite for "owner-hex"
    Then the result is true

  Scenario: Relay self can always invite
    Given a config with a secret key
    When checking CanInvite for the relay self pubkey
    Then the result is true

  Scenario: User with CanInvite role can invite
    Given a config with role "inviter" having can_invite=true and pubkey "inviter-hex"
    When checking CanInvite for "inviter-hex"
    Then the result is true

  Scenario: Member role with can_invite grants invite to all
    Given a config with member role having can_invite=true
    When checking CanInvite for any pubkey
    Then the result is true

  # --- SC: GetAllRoles includes member role for everyone ---
  Scenario: Any pubkey gets the member role
    Given a config with a "member" role with can_invite=true
    When getting all roles for an arbitrary pubkey
    Then the "member" role is included

  # --- SC: GetAssignedRoles only returns explicitly assigned roles ---
  Scenario: GetAssignedRoles excludes member role
    Given a config with "admin" and "member" roles
    And pubkey "admin-hex" is only in the "admin" role
    When getting assigned roles for "admin-hex"
    Then only the "admin" role is returned
    And the "member" role is not returned

  # --- EP: Multiple roles ---
  Scenario: User with multiple roles gets aggregate permissions
    Given a config with role "admin" having can_manage=true and pubkey "multi-hex"
    And a config with role "editor" having can_invite=true and pubkey "multi-hex"
    When checking CanManage for "multi-hex"
    Then the result is true
    When checking CanInvite for "multi-hex"
    Then the result is true
