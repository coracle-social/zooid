Feature: Key-Value Store
  As the relay
  I want a persistent key-value store
  So that I can store arbitrary configuration data

  # --- EP: Basic operations ---
  Scenario: Set and Get a key-value pair
    Given an initialized key-value store
    When key "theme" is set to "dark"
    Then Get "theme" returns "dark"

  Scenario: Get non-existent key returns error
    Given an initialized key-value store
    When Get "nonexistent" is called
    Then an error "not found" is returned

  # --- ST: Upsert behavior ---
  Scenario: Setting an existing key updates its value
    Given an initialized key-value store
    And key "count" is set to "1"
    When key "count" is set to "2"
    Then Get "count" returns "2"

  # --- EP: Namespaced KV ---
  Scenario: Namespaced keys are isolated
    Given a KV namespace "relay1"
    And a KV namespace "relay2"
    When relay1 sets "port" to "3334"
    And relay2 sets "port" to "3335"
    Then relay1 Get "port" returns "3334"
    And relay2 Get "port" returns "3335"

  # --- SC: Key format ---
  Scenario: Namespaced keys are prefixed with namespace
    Given a KV namespace "myns"
    When myns sets "key1" to "val"
    Then the underlying key is "myns:key1"

  # --- BVA: Empty values ---
  Scenario: Empty string is a valid value
    Given an initialized key-value store
    When key "empty" is set to ""
    Then Get "empty" returns ""

  # --- EG: Special characters in keys ---
  Scenario: Keys with special characters work correctly
    Given an initialized key-value store
    When key "key:with:colons" is set to "value"
    Then Get "key:with:colons" returns "value"
