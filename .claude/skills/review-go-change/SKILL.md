---
name: review-go-change
description: Run the independent review set on a Foundry Doctor change - Go principal engineer, Foundry lead, Azure black belt and, where relevant, security reviewer - and consolidate the findings. Use before opening a PR or after substantial Go changes.
---

# Review a Go change

1. Determine the diff (`git diff origin/main...HEAD -- foundry-doctor/`) and the affected phase DoD.
2. Run `foundry-doctor/scripts/claude/verify-phase.sh` first; do not request review of a failing tree.
3. Launch in parallel, each given only the diff and the DoD:
   - `go-principal-engineer` (any Go change)
   - `foundry-lead` (any rule, schema or command-semantics change)
   - `azure-black-belt` (any Azure, identity, network or WAF content)
   - `security-reviewer` (anything touching internal/azure, runtime, adapters, report, annotate, llm)
4. Consolidate: merge duplicates, keep disagreements side by side, order by severity.
5. Report blocking findings first with file:line. Do not fix anything in this skill; fixing is a separate step.
