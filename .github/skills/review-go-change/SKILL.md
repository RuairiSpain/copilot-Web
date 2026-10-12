---
name: review-go-change
description: Run the independent Foundry Doctor review set for a Go change and consolidate findings. Use before a pull request or after substantial Go production changes.
---

# Review a Foundry Doctor Go change

1. Determine the Foundry Doctor diff and affected phase Definition of Done.
2. Run `foundry-doctor/scripts/claude/verify-phase.sh` before specialist review.
3. Request independent reviews:
   - `go-principal-engineer` for every Go production change;
   - `foundry-lead` for rules, schemas, or command semantics;
   - `azure-black-belt` for Azure, identity, network, or WAF content;
   - `security-reviewer` for Azure, runtime, adapters, reports, annotation, or LLM code.
4. Give reviewers the relevant diff and Definition of Done without other reviewers' conclusions.
5. Merge duplicate findings, preserve disagreements, and rank by severity.

Report blocking findings first with exact file and line references. This skill reviews only; remediation is a separate task.
