# ADR-013: Phase 0 evidence and gate contract

- Status: Accepted for remediation
- Date: 2026-10-05
- Supersedes: Phase 0 completion claims where they conflict with this ADR

## Context

The Phase 0 hand-off recorded approvals and historical results, but there is still no successful
current CI run and no repository location for the PRD's 59 XF-origin assets. Current local validation
now includes the vulnerability and transitive dependency/licence checks, while the race test is
unsupported on Windows ARM64 and tagged spike commands were blocked by the repository preTool hook.
Calling the phase complete would obscure those remaining gaps. The product exit-code wording also
inherited an azd example in which codes 1 and 2 had different meanings from the PRD.

Generated rule and overlap documentation is owned by catalogue metadata and its generator. This
remediation does not hand-edit either generated file.

## Decision

1. Phase 0 is **reopened**. Existing artefacts remain useful, but completion requires current evidence
   for every mandatory gate or an explicit open status. An ADR can change a requirement; it cannot turn
   a skipped or failed check into passing evidence.
2. Product exit codes follow PRD section 7: 1 is findings and 2 is requested validation unable to run
   because input, dependency, authentication or permission is unavailable. External tool codes are
   translated. Codes 3 (strict skip) and 4 (internal/protocol failure) retain their PRD meanings.
3. The 108-rule overlap matrix is provisional research metadata, not proof that adapters work. Each
   rule's `overlap` metadata is the source of generated documentation. Adapter mappings require a
   structured-output fixture at a pinned tool version before implementation can claim compatibility.
4. The claimed 59 XF-origin checks are a provisional requirement. No authoritative XF assets are
   present in the current repository. Completion requires an asset manifest with source identity,
   licence, mapping to `FND-*` IDs (including explicit no-match entries), and reusable test provenance.
5. The synthetic-infrastructure spike is conditional evidence. Its schema test is build-tagged, needs
   an absolute `AZURE_DEV_DIR`, Python, PyYAML and jsonschema/referencing, and is not part of
   `verify-phase.sh`. CI is configured to run it against pinned inputs, but no successful run is
   evidenced. A result may be claimed only with the clone commit and command. The schema test proves
   fixture acceptance/rejection, not that provider-generated ARM was compiled or enumerated.
6. The current Go module graph has a fail-closed local audit and inventory consistency check, and
   dependency review is configured for relevant pull requests. These mechanisms do not establish
   successful CI or release-artefact licence clearance. Licence work remains provisional until each
   shipped module and artefact is covered and required licence and NOTICE texts are assembled.
7. Final phase evidence must include the exact command, revision, environment-relevant prerequisites,
   result, and skipped checks. A non-strict run can provide diagnostics but cannot satisfy a strict final gate.

## Consequences

- Prior review approvals remain dated review records; they do not attest to later changes.
- CI installation or intent is not evidence of a successful CI run. A reachable-database local
  `govulncheck ./...` passed in the current validation record, but no successful CI run is evidenced.
- Phase 1 must not consume XF provenance, adapter compatibility, synthetic ARM facts or release licence
  clearance as settled contracts until the corresponding evidence above exists.
