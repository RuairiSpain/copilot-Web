# IQ / gateway sample

This sample is a Phase 8 shape reference only. All values are fake.

- `azure.yaml` shows a Foundry IQ MCP connection targeting a Search
  knowledge-base endpoint.
- The forwarded user-token header shape in the sample is **UNVERIFIED** and is
  included only to mirror the currently documented
  `x-ms-query-source-authorization` form.
- The sample is intentionally small and does not include live service
  metadata. Rule-package tests provide deterministic IQ snapshot fixtures.
