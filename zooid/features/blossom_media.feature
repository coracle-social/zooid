Feature: Blossom Media Storage
  As a relay member
  I want to upload and download media files
  So that I can share images and documents

  Background:
    Given a running relay with blossom enabled

  # --- EP: Valid upload ---
  Scenario: Member uploads a file under 10MB
    Given the user is an authenticated relay member
    When the user uploads a 5MB file
    Then the file is stored successfully
    And the file can be retrieved by its SHA256 hash

  # --- BVA: File size boundaries ---
  Scenario: Member uploads exactly 10MB file at boundary
    Given the user is an authenticated relay member
    When the user uploads a 10485760-byte file exactly 10MB
    Then the file is stored successfully

  Scenario: Upload rejected for file over 10MB
    Given the user is an authenticated relay member
    When the user uploads a 10485761-byte file over 10MB
    Then the upload is rejected with "file too large" and HTTP 413

  # --- BVA: Minimum size ---
  Scenario: Member uploads a 0-byte file
    Given the user is an authenticated relay member
    When the user uploads an empty file
    Then the upload is accepted

  Scenario: Member uploads a 1-byte file
    Given the user is an authenticated relay member
    When the user uploads a 1-byte file
    Then the upload is accepted

  # --- EP: Authentication partitions ---
  Scenario: Unauthenticated user cannot upload
    Given the user is not authenticated with no auth event
    When the user attempts to upload a file
    Then the upload is rejected with "unauthorized" and HTTP 403

  Scenario: Non-member cannot upload
    Given the user is authenticated but not a relay member
    When the user attempts to upload a file
    Then the upload is rejected with "unauthorized" and HTTP 403

  # --- EP: Download access ---
  Scenario: Member can download a file
    Given a file with SHA256 "abc123..." exists
    And the user is an authenticated relay member
    When the user requests the file
    Then the file content is returned

  Scenario: Unauthenticated user cannot download
    When an unauthenticated user requests a file
    Then the request is rejected with "unauthorized" and HTTP 403

  Scenario: Non-member cannot download
    Given the user is authenticated but not a member
    When the user requests a file
    Then the request is rejected with "unauthorized" and HTTP 403

  # --- EP: List files ---
  Scenario: Member can list their files
    Given the user is an authenticated relay member
    When the user requests a file listing
    Then the listing is returned

  Scenario: Non-member cannot list files
    Given the user is authenticated but not a member
    When the user requests a file listing
    Then the request is rejected with "unauthorized"

  # --- EP: Delete files ---
  Scenario: Member can delete their file
    Given a file uploaded by the user exists
    And the user is an authenticated relay member
    When the user deletes the file
    Then the file is removed

  Scenario: Non-member cannot delete files
    Given the user is authenticated but not a member
    When the user attempts to delete a file
    Then the request is rejected with "unauthorized"

  # --- EG: Download non-existent file ---
  Scenario: Requesting a non-existent file returns an error
    Given no file with SHA256 "nonexistent" exists
    When a member requests that file
    Then an error is returned
