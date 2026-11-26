# Design document standard

Every design written by `egm-plan` follows this structure and is saved as
`docs/designs/<name>.md`. Reviewers check a design against it, and
`egm-implement` builds from it.

## Required sections

1. **Status**: `Draft`, `In review`, `Approved` or `Superseded`. Include the approval date and approver once approved.
2. **Problem**: What is wrong or missing today, and for whom. Cite the files, tickets or errors that show it.
3. **Goals**: In-scope outcomes. Each is observable.
4. **Non-goals**: Out-of-scope items that a reader might otherwise assume are included.
5. **Current state**: How the relevant code works now, with `path:line` references. Only facts verified in the repository.
6. **Proposed design**: The approach, the data structures and interfaces (with signatures), the control flow, and error handling. State every design decision; leave none for the implementer.
7. **Alternatives considered**: At least one real alternative and why it lost.
8. **Change list**: Every file to create, modify or delete, and what changes in it. This is the input to work-packet planning.
9. **Acceptance criteria**: Numbered (`AC-1`, `AC-2`, ...). Each is testable and names the test that proves it (or the manual check, if no automated test is possible).
10. **Risks and mitigations**: Security, performance, compatibility, data migration, rollout and rollback.
11. **Open questions**: Unresolved items with an owner. An approved design has none that block implementation.

## Rules

- Do not describe code that does not exist as if it does. Mark proposed names as proposed.
- Do not leave a choice to the implementer ("use an appropriate library"). Name it, or list it under open questions.
- Keep each acceptance criterion independent of the others so a packet can be verified alone.
- Record decisions made during review in the document, with the reason.

## Severity scale used by reviewers

| Severity | Meaning |
| --- | --- |
| Blocker | The design cannot be implemented correctly or safely as written. |
| Major | The design can be implemented, but a likely defect, rework or ambiguity remains. |
| Nit | Wording, ordering or style. Fix if cheap. |
