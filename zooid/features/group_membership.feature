Feature: Group Membership Management
  As an admin
  I want to add and remove members from groups
  So that I can control who participates

  Background:
    Given a running relay with groups enabled and auto_join true

  # --- ST: Group member state machine ---
  # [Non-member] ->(JoinRequest)-> [Member] ->(LeaveRequest)-> [Non-member]
  # [Non-member] ->(PutUser)->    [Member] ->(RemoveUser)->   [Non-member]

  # --- UC: Join public group ---
  Scenario: User joins a public group with auto_join
    Given a public group "cafe" exists
    And the user is authenticated and a relay member
    When the user sends a KindSimpleGroupJoinRequest for "cafe"
    Then the event is accepted
    And the user is added as a member of "cafe"
    And the members list kind=39002 is updated

  # --- ST: Leave group ---
  Scenario: Member leaves a group
    Given the user is a member of group "cafe"
    When the user sends a KindSimpleGroupLeaveRequest for "cafe"
    Then the event is accepted
    And the user is removed from "cafe"
    And the members list is updated

  # --- EG: Leave group you're not in ---
  Scenario: Non-member cannot leave a group
    Given the user is NOT a member of group "cafe"
    When the user sends a KindSimpleGroupLeaveRequest for "cafe"
    Then the event is rejected with "not currently a member"

  # --- ST: Already a member tries to join ---
  Scenario: Already-member join request is rejected
    Given the user is already a member of group "cafe"
    When the user sends a KindSimpleGroupJoinRequest for "cafe"
    Then the event is rejected with "already a member"

  # --- UC: Admin adds member ---
  Scenario: Admin adds a user to a group via PutUser
    Given a group "team" exists
    And the user is an admin
    And "userB" is not a member of "team"
    When the admin publishes a KindSimpleGroupPutUser for "userB" in group "team"
    Then "userB" becomes a member of "team"
    And the members list is updated

  # --- UC: Admin removes member ---
  Scenario: Admin removes a user from a group via RemoveUser
    Given "userB" is a member of group "team"
    And the user is an admin
    When the admin publishes a KindSimpleGroupRemoveUser for "userB" in group "team"
    Then "userB" is no longer a member of "team"
    And the members list is updated

  # --- ST: Kicked user can rejoin ---
  Scenario: Kicked user can rejoin a public group
    Given "userB" was removed from group "team"
    And "team" is a public group
    When "userB" sends a join request for "team"
    Then "userB" is accepted
    And "userB" can read and post in "team" again

  # --- LP: Multiple add/remove cycles ---
  Scenario: Member status reflects latest event after multiple cycles
    Given "userB" is added to group "cycle"
    And "userB" is removed from group "cycle"
    And "userB" is added to group "cycle" again
    Then "userB" is a member of "cycle"

  # --- SC: GetMembers processes events chronologically ---
  Scenario: GetMembers returns correct members after mixed history
    Given group "complex" has the following history:
      | order | kind       | user   |
      | 1     | PutUser    | Alice  |
      | 2     | PutUser    | Bob    |
      | 3     | RemoveUser | Alice  |
      | 4     | PutUser    | Carol  |
    Then the members of "complex" are "Bob" and "Carol"
    And "Alice" is not a member

  # --- UC: Kicked user cannot post ---
  Scenario: Kicked user cannot post to closed group
    Given a closed group "vip" exists
    And "userB" was kicked from "vip"
    When "userB" tries to post a message to "vip"
    Then the event is rejected with "not a member"

  # --- UC: Invited user can post ---
  Scenario: Invited user can post to group after being added
    Given a private group "lab" exists
    And "userB" is not a member
    When admin adds "userB" via PutUser
    And "userB" posts a message to "lab"
    Then the message is stored successfully
    And "userB" can retrieve their own message
