# ADR 0005: Quality gates

Status: accepted

## Context

The PRD asked for 100 percent test coverage and for mutation testing or fault injection on the
scheduler, queue, affinity and reconciliation logic.

## Decision

- Coverage gate: **90 percent, statement and branch**, set by the project owner at the start of the
  build. The suite measures higher. Nothing is excluded with `pragma: no cover`.
- Strict mypy over `src`, `tests` and `scripts`. Tests may replace methods on fakes, so mypy ignores
  `method-assign` and `assignment` in `tests.*` only.
- `scripts/mutation_check.py` applies one AST mutation at a time (operator, constant, condition,
  return value, statement deletion) to a copy of the scheduler, queue, affinity, registry and
  reconciler. A hang counts as killed. Survivors are fixed with tests or documented as equivalent.
- CI runs Ruff, mypy, Bandit, `pip-audit`, the tests, an SBOM and a Trivy image scan. Mutation runs
  weekly and on demand.

## Consequences

- A mutation survivor is a signal, not a failure. The accepted equivalent survivor is the default
  `grace_seconds` value in `Reconciler.stop`, which cannot be observed without waiting five seconds.
- The script is a small purpose-built tool, not a replacement for a full mutation framework.

## Results at 1.0.0

209 mutants. 207 killed by a failing test, 1 killed by a timeout (a hang) and 1 survived (the
equivalent `grace_seconds` default).

| Module | Mutants | Killed | Timeout | Survived |
|---|---|---|---|---|
| scheduler | 23 | 23 | 0 | 0 |
| queue | 8 | 8 | 0 | 0 |
| affinity | 22 | 22 | 0 | 0 |
| registry | 54 | 54 | 0 | 0 |
| reconciler | 102 | 100 | 1 | 1 |

The scheduler and queue figures come from the first full run. Their source is unchanged since.

## Results at 1.1.0

The reconciler changed (derived-id adoption, first-sync tracking) and the circuit breaker is new.
Both were mutated again. The scheduler, queue, affinity and registry sources did not change, so their
1.0.0 results stand.

| Module | Mutants | Killed | Timeout | Survived |
|---|---|---|---|---|
| circuit | 53 | 51 | 0 | 2 |
| reconciler | 109 | 107 | 1 | 1 |

Survivors, all equivalent mutants:

- `circuit_breaker.py`: the initial values of `_opened_at` and `_probes`. Neither is read before
  `_open()` sets it.
- `reconciler.py`: the `grace_seconds` default, as at 1.0.0.

The first run of this release left 14 survivors. Four were reconciler mutants that the new restart
tests did not yet cover, because the mutation target did not list them. Seven were probe-counter
updates in the breaker that could never change behaviour, because the counter is reset whenever the
circuit opens. They were removed from the code. One needed a test with two probes in flight.

The pool service, the adapters and the API are not mutation targets. They are covered by the unit,
integration and contract tests.
