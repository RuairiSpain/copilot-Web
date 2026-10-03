# 0003. A lead agent with a code gate, in front of a Magentic team

Status: accepted, 2026-10-03

## Context

The requirement: the main agent interrogates the user until the brief is unambiguous, then a
planner, writer and tester build the project. Hosting `workflow.as_agent()` is deprecated, and
the Magentic manager has no tools and cannot hold a user interview.

## Decision

* A normal Agent Framework agent (the lead) hosts the conversation over the Responses protocol.
* `start_build` validates the brief in code and refuses incomplete or vague briefs.
* The Magentic workflow runs inside `start_build`, built fresh per run.
* Each role works in its own git worktree and owns distinct paths.

## Consequences

* The interview cannot be skipped by a prompt mistake.
* A build is one long tool call. For long builds, call the Responses endpoint with
  `background: true` and poll.
* One project per session. A new project needs a new session.
