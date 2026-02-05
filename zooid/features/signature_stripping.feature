Feature: Signature Stripping
  As a relay operator
  I want to optionally strip event signatures
  So that users must trust the relay for event authenticity

  Background:
    Given a running relay

  # --- DT: Signature stripping decision table ---

  Scenario: Signatures preserved when strip_signatures is false
    Given strip_signatures is false
    When a non-admin user queries events
    Then all events have their original signatures

  Scenario: Admin retains signatures even when stripping enabled
    Given strip_signatures is true
    And the user is an admin
    When the user queries events
    Then all events have their original signatures

  Scenario: Non-admin gets zeroed signatures when stripping enabled
    Given strip_signatures is true
    And the user is not an admin
    When the user queries events
    Then all event signatures are zeroed out
