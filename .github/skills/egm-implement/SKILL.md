---
name: egm-implement
description: Implement an approved design from docs/designs/<name>.md - plans work packets, runs implementers and a test writer in parallel on disjoint files, then verifies the result with an independent agent. Use after egm-plan has produced an Approved design.
---

# egm-implement

You are the coordinator. Subagents write the code and tests. An independent verifier checks the result. You report what actually happened.

## 0. Preconditions

Read the design the user names. Stop and say so if:

- its Status is not `Approved`,
- it has open questions that block implementation, or
- the working tree has unrelated uncommitted changes (ask the user how to proceed).

Work on a feature branch, not the default branch.

## 1. Plan the packets

Run `egm-packet-planner` with the design path. It returns verification commands, packets with exclusive file ownership, an order, and a criterion-to-test table. Check that:

- no file is owned by two packets,
- every acceptance criterion maps to a packet and a test,
- the verification commands exist in the repository.

If it lists design gaps, ask the user to resolve them (and update the design) before continuing. Show the user the packet table and wait for agreement when it has more than three packets.

## 2. Implement

Launch packets that have no dependency on each other **in parallel**, in one step: one `egm-implementer` per packet, plus one `egm-test-writer` for the test packet. Each prompt gives: the design path, its packet (goal, owned files, criteria), and the verification commands. Packets that depend on earlier ones start after those finish.

How to launch: Claude Code uses the `Agent` tool with `subagent_type` set to the agent name. GitHub Copilot uses the agent/subagent tool with the custom agent name. If the harness cannot run subagents, work through the packets yourself in order, keeping to each packet's file ownership.

Collect the reports. A `DEVIATION:` line is a design question: show it to the user and update the design if they agree. If an implementer needed a file it did not own, assign the change to the owning packet or a follow-up packet.

## 3. Check

Run the verification commands yourself and read the output. Fix failures by sending the failing packet back to its implementer with the exact error, not by editing across packet boundaries. Allow two repair rounds, then report what is still failing.

## 4. Verify independently

Run `egm-verifier` with the design path, the base branch and the verification commands. Do not pass your own summary of what was done. If its verdict is `not ready`, fix the Blockers and Major findings through step 2 and verify again. Allow two rounds.

## 5. Report

Tell the user, plainly:

- files changed,
- each acceptance criterion with its status and evidence,
- the verifier's verdict and any unresolved findings,
- commands that failed or could not run,
- deviations from the design and how each was settled.

Set the design Status to `Implemented` only if the verdict is `ready`.

## Ground rules

- Do not commit, push, or open a pull request unless the user asks.
- Never skip, weaken or delete a test to get a passing run.
- Do not read or print secrets or `.env` files.
- Report failures as failures. A step that did not run is reported as not run.
