Feature: Group Lifecycle
  As a relay user
  I want to create, edit, and delete groups
  So that I can organize conversations

  Background:
    Given a running relay with groups enabled
    And auto_join is true

  # --- ST: Group state machine ---
  # [Non-existent] -> CreateGroup -> [Active]
  # [Active] -> EditMetadata -> [Active (updated)]
  # [Active] -> DeleteGroup -> [Deleted]

  # --- UC: Admin creates public group ---
  Scenario: Admin creates a public group
    Given the user is an admin
    When the user publishes a KindSimpleGroupCreateGroup event with:
      | h_tag   | test-group                       |
      | content | {"name":"Test","private":false}  |
    Then the event is accepted
    And a KindSimpleGroupMetadata event is created for "test-group"
    And the metadata does NOT have a "private" tag
    And the creator is added as a member
    And a KindSimpleGroupMembers event is published
    And a KindSimpleGroupAdmins event is published

  # --- DT: Group creation permission decision table ---
  Scenario: Non-admin can create group when admin_create_only is false
    Given admin_create_only is false
    And private_admin_only is false
    And the user is not an admin
    When the user creates a public group
    Then the group creation is accepted

  Scenario: Non-admin cannot create group when admin_create_only is true
    Given admin_create_only is true
    And the user is not an admin
    When the user creates a public group
    Then the group creation is rejected with "only admins can create"

  Scenario: Admin can create group when admin_create_only is true
    Given admin_create_only is true
    And the user is an admin
    When the user creates a public group
    Then the group creation is accepted

  Scenario: Non-admin can create public group when private_admin_only is true
    Given admin_create_only is false
    And private_admin_only is true
    And the user is not an admin
    When the user creates a public group
    Then the group creation is accepted

  Scenario: Non-admin cannot create private group when private_admin_only is true
    Given admin_create_only is false
    And private_admin_only is true
    And the user is not an admin
    When the user creates a private group
    Then the group creation is rejected with "only admins can create private"

  Scenario: Admin can create private group when private_admin_only is true
    Given private_admin_only is true
    And the user is an admin
    When the user creates a private group
    Then the group creation is accepted

  # --- EG: Duplicate group creation ---
  Scenario: Creating a group that already exists is rejected
    Given a group "existing-group" already exists
    When the user creates a group with h="existing-group"
    Then the event is rejected with "group already exists"

  # --- UC: Edit group metadata ---
  Scenario: Admin edits group metadata
    Given a group "my-group" exists
    And the user is an admin
    When the user publishes a KindSimpleGroupEditMetadata event for "my-group"
    Then the group metadata is updated
    And the admins list is refreshed

  # --- UC: Delete group ---
  Scenario: Admin deletes a group
    Given a group "doomed-group" exists with messages
    And the user is an admin
    When the user publishes a KindSimpleGroupDeleteGroup event for "doomed-group"
    Then all events for "doomed-group" are deleted except the deletion event
    And the group metadata is no longer queryable

  # --- EG: Delete non-existent group ---
  Scenario: Deleting a non-existent group is rejected
    Given no group "ghost-group" exists
    When the user publishes a KindSimpleGroupDeleteGroup event for "ghost-group"
    Then the event is rejected with "group not found"

  # --- SC: Metadata visibility tags ---
  Scenario: Creating a private group adds "private" tag to metadata
    Given the user is an admin
    When the user creates a group with content {"name":"Secret","private":true}
    Then the metadata event has a "private" tag

  Scenario: Creating a closed group adds "closed" tag to metadata
    Given the user is an admin
    When the user creates a group with content {"name":"Closed","closed":true}
    Then the metadata event has a "closed" tag

  Scenario: Creating a hidden group adds "hidden" tag to metadata
    Given the user is an admin
    When the user creates a group with content {"name":"Hidden","hidden":true}
    Then the metadata event has a "hidden" tag

  Scenario: Creating a group with all visibility flags
    Given the user is an admin
    When the user creates a group with content {"name":"All","private":true,"closed":true,"hidden":true}
    Then the metadata event has "private", "closed", and "hidden" tags

  # --- EG: Malformed content ---
  Scenario: Creating a group with invalid JSON content
    Given the user is an admin
    When the user creates a group with content "not-json"
    Then the group is created successfully
    And the metadata event has no visibility tags

  Scenario: Creating a group with private as string instead of bool
    Given the user is an admin
    When the user creates a group with content {"name":"Test","private":"true"}
    Then the group is created
    And the metadata event does NOT have a "private" tag
