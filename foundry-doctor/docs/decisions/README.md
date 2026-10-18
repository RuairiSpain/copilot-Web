# Architecture decision records

Foundry Doctor keeps its ADRs in two directories. Together they form one numbered series.

| Directory | Numbers | Content |
|---|---|---|
| `docs/decisions/` | ADR-001 to ADR-013, ADR-018 to ADR-028 | Phase 0 architecture set, Phase 1 engineering decisions, parameter files (ADR-018), the deferral of the Go and security reviews (ADR-019), PSRule deferral and rule packs (ADR-020), the Phase 2 deviations (ADR-021) and the phase allocation for scope-policy owner rules (ADR-022) and the move of the FND-IDN-002 and FND-NET-002 live checks to Phase 8 (ADR-023), and the Phase 6 annotate review-copy decisions and deferrals (ADR-024), and the Phase 7 cost decisions and deferrals (ADR-025), and the Phase 4 WAF assessment scope, evidence states and deferrals (ADR-026), and the Phase 5 graph decisions and deferrals (ADR-027), and the Phase 8 Foundry IQ and gateway decisions and the ADR-022/023 allocations Phase 8 does not deliver (ADR-028). |
| `docs/adr/` | ADR-014 to ADR-017 | V1 product addenda: command, naming and config surface (ADR-014), verified compatibility floors (ADR-015), CLI and environment authentication (ADR-016), release verification (ADR-017). |

Rules:

- Numbers are unique across both directories. A number appears once.
- A new ADR takes the next free number across both directories (the highest number in use is currently ADR-028). Check both before choosing.
- ADR-014 always means `docs/adr/ADR-014-v1-command-and-config-surface.md`. The deferral of the Go principal and security
  reviews was first numbered ADR-014 in `docs/decisions/` by mistake and was renumbered ADR-019 to remove the collision.
- Put an architecture or engineering decision in `docs/decisions/`; put a closed V1 product-surface decision in `docs/adr/`.
