# ADR-024: Phase 6 annotate review-copy decisions and deferrals

Status: Accepted
Date: 2026-10-18
Decided by: the Phase 6 fixer (go-implementer, fix loop 1), from the Phase 6 round 1 reviews (`foundry-lead` and `azure-black-belt`, both REJECT)

## Context

The round 1 reviews of Phase 6 (PRD section 14) asked for code fixes and, where a fix belongs to a later phase or needs facts that
cannot be verified in this loop, a recorded decision. This ADR records the design choices made by fix loop 1 and the items it does not
deliver. It does not waive any PRD section 14 DoD item.

## Decision

1. **The review copy is not an azd project.** azd finds a project by the file name `azure.yaml` (reviewer citation:
   `cli/azd/pkg/environment/azdcontext/azdcontext.go`, `ProjectFileName`; not re-fetched in this loop). Every copied file named
   `azure.yaml` is written as `azure.review.yaml` (`azure.yml` as `azure.review.yml`, defensively; whether azd also discovers `azure.yml`
   is **unverified**). `annotations-manifest.json` `files[]` maps `path` to `reviewPath`. Every copied `azure.yaml` and every copied `.bicep`
   file carries the do-not-deploy banner, with or without annotations. The review root also holds a `.foundry-doctor-review` marker file.
2. **Ownership.** An existing `--out` directory is replaced only when it is empty, holds the `.foundry-doctor-review` marker, or holds an
   `annotations-manifest.json` with `"tool": "foundry-doctor"`. Otherwise the run fails with exit 2 and the directory is untouched.
3. **One infra resolver.** `resolveInfra` (internal/app/annotate.go) gives the infra directory and entry module used by the review copy, the
   annotate compiles, the ARM loader and the preflight (through `resolveBicepEntry`). Precedence: a Foundry Doctor `inputs.infraPath` that differs
   from the default `infra`; then `azure.yaml` `infra.path`; then `infra`. The `azure.yaml` value and the default are resolved against the
   directory that contains `azure.yaml`. A configured `inputs.infraPath: infra` cannot be told apart from the default (the config layer fills
   the default), so it does not override `azure.yaml`. `infra.module` defaults to `main` and gets `.bicep` when it has no extension; the entry must
   stay inside the infra directory. `infra.provider` other than `bicep` (built-in `terraform`, or an extension provider) is never compiled by
   annotate. Source: https://learn.microsoft.com/azure/developer/azure-developer-cli/azd-schema (infra section), verified 2026-10-18.
   The doctor ARM loader (`wire.go`, shared) is not changed by this ADR: for a non-Bicep provider it still falls back to `infra/main.bicep`
   when that file exists, as before Phase 6. Changing that is left to the owner of `wire.go`.
4. **ARM equivalence ignores generator metadata.** Whether Bicep's `metadata._generator.templateHash` is sensitive to comment-only source
   edits is **unverified**. The review-copy check therefore compares the compiled ARM with `metadata._generator` removed at every nesting level;
   every other byte of the template (after JSON canonicalisation) must match.
5. **Deferred test categories (disclosed, not waived).**
   - PRD "Large repository performance tests": not delivered in Phase 6; carried to the Phase 9 audit. Until then there is no size or
     file-count cap on the infra copy (reviewer N11).
   - A Windows junction test: junctions and other reparse points are refused through `fs.ModeIrregular` (Go 1.23+ reports them so,
     https://go.dev/doc/go1.23#os), but the test needs a Windows runner and is not written in this loop.
   - The real-Bicep recompile test (`internal/app/annotate_bicep_real_test.go`, build tag `spike`) must be run by the maintainer with
     `BICEP_PATH` set; it has not been run.
6. **Not changed in this loop (non-blocking findings).** Per-annotation fallback instead of failing the whole run (lead N6 partly, black
   belt N5); YAML-only fallback for `infra.layers` or a missing Bicep CLI (lead N8); a GitHub path prefix (lead N4, black belt N7); the
   out-of-root Bicep diagnostic path flag (lead N5, owned by `internal/bicep`); the duplicate extension `annotateCmd` (lead N11, owned by
   `extension/`); copying intermediate `bicepconfig.json` files (black belt N12).

## Consequences

- `docs/annotate.md` documents the rename, the banner, the ownership marker, the resolver precedence and the limits above.
- The Phase 6 hand-off lists the deferred tests and points to this ADR.
- If azd is found to discover other file names, the rename list in `internal/annotate/review.go` `reviewName` must grow.
