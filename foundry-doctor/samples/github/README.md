# GitHub workflow samples

`foundry-doctor-starter.yml` is a copy-paste sample that calls the reusable workflow
`.github/workflows/foundry-doctor-offline.yml`.

It is not a registered GitHub "starter workflow". A starter workflow appears in the Actions tab only when the file and a
`.properties.json` live in the `workflow-templates/` folder of an organisation-level `.github` repository. This monorepo is not
that repository, so copy the file by hand.

## Use

1. Copy `foundry-doctor-starter.yml` to `.github/workflows/foundry-doctor.yml` in your repository.
2. Replace `<tag-or-sha>` with a release tag or a full commit SHA. Pin a SHA if you want an immutable reference.
3. Set `path`, `profile`, `fail-on`, and a `version` that exists as a release (`foundry-doctor/v<version>`), or use `install-mode: source`.
4. Keep the three `permissions` entries. A called workflow cannot ask for more than its caller grants, and GitHub rejects the run at start-up if it does.

## What it does

- Installs the `foundry-doctor` binary (from a release, checksum and attestation verified, or built from source) and runs
  `foundry-doctor doctor --format sarif --out ...` in `path`.
- Attaches the SARIF to the run as an artifact and uploads it with `github/codeql-action/upload-sarif`, so findings show in
  Security > Code scanning. Private repositories need GitHub Code Security for the upload.
- Fails the job when the doctor exits non-zero (1 findings at or above `fail-on`; 2 could not run; 3 skipped check under `--strict`; 4 internal error).

## Fork pull requests

A workflow triggered by `pull_request` from a fork has a read-only token, so `security-events: write` is not available. The upload
job is skipped (and so is it for Dependabot) with a notice. The SARIF is still an artifact and the exit code is still enforced,
so a fork cannot pass a failing check by skipping the upload. Do not switch to `pull_request_target` to get the upload: that would
run with a write token and secrets on code from the fork.

## Status

The workflows have not been run on GitHub yet. Fork behaviour, the upload, and the release download path are unproven until a real run.
