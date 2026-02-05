Feature: Multi-Instance Dispatch & Hot-Reload
  As a relay operator
  I want to host multiple relays from one process
  And have them reload when I edit config files

  # --- UC: Multiple instances ---
  Scenario: Multiple config files create multiple instances
    Given config files for "relay-a.com" and "relay-b.com" exist
    When the relay starts
    Then Dispatch for "relay-a.com" returns the correct instance
    And Dispatch for "relay-b.com" returns the correct instance
    And each instance has its own schema prefix

  # --- EP: Unknown host ---
  Scenario: Dispatch for unknown host returns not found
    When Dispatch for "unknown.com" is called
    Then the result is nil and false

  # --- ST: Hot-reload state machine ---
  Scenario: Modifying a config file reloads the instance
    Given instance "relay-a.com" is running
    When the config file for "relay-a.com" is modified
    Then the old instance is cleaned up
    And a new instance is loaded with updated config
    And Dispatch for "relay-a.com" returns the new instance

  Scenario: Deleting a config file unloads the instance
    Given instance "relay-a.com" is running
    When the config file is deleted
    Then Dispatch for "relay-a.com" returns not found

  Scenario: Creating a new config file loads a new instance
    Given the relay is running with no instances
    When a new config file for "relay-c.com" is created
    Then Dispatch for "relay-c.com" returns the new instance

  # --- EG: Invalid config file ---
  Scenario: Invalid config file does not crash the relay
    Given the relay is running
    When a config file with invalid TOML is created
    Then an error is logged
    And existing instances continue running
