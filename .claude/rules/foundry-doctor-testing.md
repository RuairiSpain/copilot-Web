---
paths:
  - "foundry-doctor/**/*_test.go"
  - "foundry-doctor/test/**"
  - "foundry-doctor/samples/**"
---

# Testing requirements

Each rule has: a fixture that must produce a finding, one that must not, profile
severity tests, missing-input behaviour, skipped/uncertain behaviour where applicable,
redaction tests for evidence, and rule-version and fingerprint stability tests.

Use table-driven unit tests, golden tests for every reporter, recorded HTTP fixtures
(secrets scrubbed) for Azure APIs, fake clients for failure injection, fuzz tests for
YAML parsing and redaction, and `-race` for concurrent rule execution. Live Azure
tests run only in designated test subscriptions and are skipped by default.

Tests must not depend on a developer's Azure login, environment variables or network.
Never weaken or delete a test to make a gate pass. Fixtures that contain secret-shaped
strings must be fake and listed in `scripts/claude/secret-allowlist.txt`.
