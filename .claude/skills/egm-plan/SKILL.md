---
name: egm-plan
description: Plan a feature or change before writing code. Clarifies requirements, drafts a design at docs/designs/<name>.md, has five fresh-context reviewers challenge it, resolves findings with the user, and stops at an approved design. Use for work that needs design decisions; skip for small, clear fixes.
---

# egm-plan

You are the **Elephant**: you hold the project context and the conversation, and you write the design. The reviewers are **Goldfish**: fresh subagents that see only the document and the repository. The **user** makes the trade-offs and approves the design.

Do not write production code in this skill. The output is an approved design file. Implementation is the `egm-implement` skill.

Read `docs/egm/design-standard.md` before drafting. It defines the required sections and the severity scale.

## 1. Clarify

Ask the user only what you cannot learn from the repository: the problem, who it affects, constraints, and what is out of scope. Read the relevant code first. Ask at most a few questions at a time, and propose a default for each.

Agree on a short kebab-case `<name>` for the design.

## 2. Draft

If the design depends on facts about a library, API, standard or service that you are not certain of, run the `researcher` agent first (skip this if the agent is not installed) and cite its sources in the design. Do not write such facts from memory.

Write `docs/designs/<name>.md` following the design standard, with Status `Draft`. Verify every statement about current code by reading it, and cite `path:line`. Put every design decision in the document. Show the user the Proposed design and Change list sections and take feedback before reviewing.

## 3. Review with fresh reviewers

Set Status to `In review`. Launch these subagents **in parallel, in one step**, each as a new subagent:

- `egm-design-critic`
- `egm-codebase-fit`
- `egm-implementation-readiness`
- `egm-clarity-reviewer`
- `egm-zoom-out`

Also launch `egm-security-reviewer` with `Mode: DESIGN` when the design touches authentication, authorization, user input, secrets, stored data or network calls.

Each prompt contains only: the design path, and the instruction to review it against `docs/egm/design-standard.md`. **Do not include conversation history, your reasoning, or your summary of the design.** The point of a Goldfish is that it does not share your assumptions.

How to launch: Claude Code uses the `Agent` tool with `subagent_type` set to the agent name. GitHub Copilot uses the agent/subagent tool with the custom agent name. If the harness cannot create a fresh subagent, stop and tell the user the review cannot run. Do not review the design yourself and call it a Goldfish review.

A reviewer that fails or returns nothing did not review. Retry it once. If it fails again, record it as `not run`. Never count it as zero findings.

## 4. Consolidate

Merge the reports. Deduplicate overlapping findings, keep the highest severity, and note which reviewers raised each. Group as Blocker, Major, Nit. Write the result to `docs/designs/<name>.review.md` with a table of reviewers and their status (`completed` or `not run`) above the findings.

Reviewer claims are leads, not facts. Check any finding that cites code before you act on it, and drop the ones that are wrong, with a one-line reason.

## 5. Resolve with the user

Present findings from Blocker down. For each, give your recommendation and let the user accept, reject or modify it. Do not apply a change that is a design trade-off without the user's decision. Apply agreed changes to the design and record each decision, with its reason, in the document.

If changes were substantial, repeat step 3 once on the revised design. Stop after two rounds and report what remains.

## 6. Approve and hand off

When no Blocker remains and open questions do not block implementation, ask the user to approve. On approval set Status to `Approved` with the date. Report the design path, the review path, and that `egm-implement` is the next step. Do not start implementing.

## Ground rules

- Only the user approves a design. Never mark it `Approved` on your own.
- Do not open a pull request, push, or commit unless the user asks. If the user asks for a commit, include the design and review files only.
- Skip this skill for a small, clear fix. Say so and proceed without it.
