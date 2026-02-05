Feature: Special Event Kinds - Zaps & Gift Wraps
  As a Nostr user
  I want zap receipts and gift wraps to be deliverable
  Even when the sender is not a member

  Background:
    Given a running relay

  # --- DT: AllowRecipientEvent decision table ---

  Scenario: Zap receipt with member recipient is accepted
    Given "userB" is a relay member
    When an external user publishes a KindZap event with p="userB"
    Then the event is accepted bypassing auth check

  Scenario: Zap receipt with non-member recipient is rejected
    Given "userC" is not a relay member
    When an external user publishes a KindZap event with p="userC"
    Then the event goes through normal auth flow and is rejected

  Scenario: Gift wrap with member recipient is accepted
    Given "userB" is a relay member
    When an external user publishes a KindGiftWrap event with p="userB"
    Then the event is accepted

  Scenario: Zap receipt without "p" tag is rejected
    When an event of KindZap without a "p" tag is published
    Then the event goes through normal auth flow

  Scenario: Non-zap non-giftwrap event is not recipient-authorized
    When a KindTextNote event with a "p" tag is published
    Then normal auth rules apply with no recipient bypass
