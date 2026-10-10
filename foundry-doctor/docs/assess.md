# WAF assessment

`foundry-doctor assess waf` converts Foundry Doctor findings into three WAF-oriented views:

- **owner**: concise decision, top five actions and unassessed scope
- **developer**: detailed evidence, fix guidance and locations
- **evidence**: auditable Markdown or JSON pack that keeps passed, failed, suppressed, baselined and skipped controls visible

## Scope and limitations

- The report maps available Foundry Doctor evidence to selected WAF-aligned controls.
- It does **not** claim complete Azure Well-Architected compliance or certification.
- Controls that need business or operational context remain **QUESTION** or **UNKNOWN**.
- Template-dependent controls remain visible when Bicep or deployed evidence is unavailable.

## Usage

```text
foundry-doctor assess waf [--profile dev|test|prod]
                          [--baseline <file>] [--suppressions <file>] [--strict]
                          [--audience owner|developer|evidence]
                          [--format markdown|html|json]
                          [--evidence-pack]
                          [--out <path>]
```

Examples:

```text
foundry-doctor assess waf --profile prod
foundry-doctor assess waf --audience developer --format html --out assess.html
foundry-doctor assess waf --evidence-pack --format json --out evidence.json
```

## States

| State | Meaning |
|---|---|
| PASS | Deterministic evidence was evaluated and no finding remains |
| FAIL | A mandatory control has an active error finding |
| WARNING | A non-failing finding or accepted exception remains visible |
| UNKNOWN | The control belongs in the WAF view but deterministic evidence was not available |
| QUESTION | The control requires a business or operational answer |
| SKIPPED | The underlying rule did not run |

## Sources

Each control carries the first verified catalogue source URL and verification date. Product-opinion controls are labelled as such.

## Sample fixtures

- `samples/assess-dev/`
- `samples/assess-test/`
- `samples/assess-prod/`
