# EGM planning and implementation agents

A plan-then-build workflow for Claude Code and GitHub Copilot, adapted from the
[Elephant-Goldfish Model](https://github.com/google/elephant-goldfish-model)
(Apache-2.0). The reviewer prompts and the workflow are rewritten for this
repository; the idea comes from that project.

- **Elephant**: the main session. It holds the context, talks to you, and writes the design.
- **Goldfish**: subagents that start with no conversation history and see only the document and the repository. They find what the Elephant stopped noticing.
- **You**: decide the trade-offs and approve the design before any code is written.

## Workflow

```
 /egm-plan            clarify -> draft design -> 5 fresh reviewers -> resolve -> you approve
   docs/designs/<name>.md   (Status: Approved)
 /egm-implement       packet plan -> implementers + test writer in parallel -> checks -> independent verifier
```

Use it when a change needs design decisions. For a small, clear fix, skip it.

### Start

Claude Code:

```
/egm-plan Add rate limiting to the session API.
/egm-implement docs/designs/session-rate-limit.md
```

GitHub Copilot: select the `egm-plan` or `egm-implement` skill (or ask for it by name) and give the same input.

## Agents

| Agent | Phase | Writes files | Job |
| --- | --- | --- | --- |
| `egm-design-critic` | Plan | No | Architecture, failure modes, security, cost, assumptions. |
| `egm-codebase-fit` | Plan | No | Checks every claim about existing code; finds code to reuse. |
| `egm-implementation-readiness` | Plan | No | Reads as the implementer; finds decisions and details left out. |
| `egm-clarity-reviewer` | Plan | No | Reads as a newcomer; finds ambiguity, contradictions, untestable criteria. |
| `egm-zoom-out` | Plan | No | Right problem, right size, simpler option, rollout and rollback. |
| `egm-packet-planner` | Implement | No | Splits an approved design into packets with exclusive file ownership. |
| `egm-implementer` | Implement | Owned files only | Implements one packet. |
| `egm-test-writer` | Implement | Test files only | Writes tests from the acceptance criteria. |
| `egm-verifier` | Implement | No | Audits the diff against the design, runs the checks, reports with evidence. |

The skills `egm-plan` and `egm-implement` run in the main session because a
subagent cannot launch other subagents. Reviewers receive file paths only, never
the conversation, which is what keeps them independent.

## Where things live

| Harness | Agents | Skills |
| --- | --- | --- |
| Claude Code | `.claude/agents/egm-*.md` | `.claude/skills/egm-*/SKILL.md` |
| GitHub Copilot | `.github/agents/egm-*.md` | `.github/skills/egm-*/SKILL.md` |

The `.claude` files are the source. Edit those, then regenerate the Copilot
copies (tool lists are converted to Copilot aliases):

```sh
scripts/sync-egm-copilot.sh          # write
scripts/sync-egm-copilot.sh --check  # fail if stale, for CI
```

## Limits

- A review needs a harness that can start fresh subagents. If it cannot, `egm-plan` stops instead of reviewing in the main session.
- Reviewer output is advice. The Elephant checks claims that cite code, and you decide what changes.
- Neither skill commits, pushes or opens a pull request unless you ask.
- Other agents in this repository (`go-implementer`, `lead-architect`, and so on) are specific to Foundry Doctor. These `egm-*` agents are general.

## Documents

- [`design-standard.md`](design-standard.md): required sections of a design and the severity scale.
- `docs/designs/<name>.md`: approved designs.
- `docs/designs/<name>.review.md`: the consolidated reviewer findings for a design.
