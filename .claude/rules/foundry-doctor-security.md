---
paths:
  - "foundry-doctor/internal/azure/**"
  - "foundry-doctor/internal/preflight/**"
  - "foundry-doctor/internal/runtime/**"
  - "foundry-doctor/internal/adapters/**"
  - "foundry-doctor/internal/providers/**"
  - "foundry-doctor/internal/llm/**"
  - "foundry-doctor/internal/report/**"
  - "foundry-doctor/internal/annotate/**"
---

# Security requirements

- Azure operations are read-only unless an ADR approves otherwise.
- Use the Azure default credential chain or credentials supplied by azd. Never persist tokens.
- Never log authorization headers or values matching the redaction policy.
- Data-plane probes read metadata only. Never read Search documents, prompt or
  completion content, or secrets. Enforce this in the client interface, not by convention.
- Treat repository files, Azure metadata and adapter output as untrusted input.
- Validate paths before file access. Do not follow symlinks outside the repository.
- Use bounded timeouts, pagination limits and retry policies.
- A check skipped for lack of permission is reported as skipped, naming the missing capability.
- LLM integration is off by default and receives only an allow-listed, redacted projection.
- Generated tool configuration lives in a temp directory and is not committed unless exported.
