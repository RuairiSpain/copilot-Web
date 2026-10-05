# foundry-doctor report

- Tool: foundry-doctor 0.1.0-test
- Profile: `foundry-prod`

## Effective policy

| Key | Value |
| --- | --- |
| `allowedExternalScopes` | \[rg-shared, rg-hub\] |
| `apiToken` | \[redacted\] |
| `nested.clientSecret` | \[redacted\] |
| `nested.retries` | 3 |
| `resourceScope` | same-resource-group |
| `validation.azure` | optional |
| `validation.bicep` | required |

## Tools

| Tool | Version | State | Required | Detail |
| --- | --- | --- | --- | --- |
| bicep | 0.47.16 | available | true |   |
| psrule |   | missing | false | not installed |

## Findings (5)

### `azure.yaml`

| Severity | Rule | Position | Resource | Details | Status |
| --- | --- | --- | --- | --- | --- |
| error | `FND-SYS-INVALID-AZURE-YAML` |   |   | Evidence: azure.yaml failed to parse<br>Recommendation: Fix the YAML. | certain |

### `infra/main.bicep`

| Severity | Rule | Position | Resource | Details | Status |
| --- | --- | --- | --- | --- | --- |
| error | `FND-SEC-002` | 12:5 | Microsoft.CognitiveServices/accounts aiservices /properties/disableLocalAuth | Evidence: disableLocalAuth is false second line<br>Recommendation: Set disableLocalAuth to true.<br>Fix: disableLocalAuth: true<br>[docs](https://learn.microsoft.com/azure/ai-services/) | certain |
| warning | `FND-NET-001` | 40:3 | Microsoft.CognitiveServices/accounts aiservices | Evidence: Pipe \| in text and &lt;b&gt;html&lt;/b&gt; and \[link\]\(http://evil.example\)<br>Recommendation: Review the setting. | likely |

### `infra/other.bicep`

| Severity | Rule | Position | Resource | Details | Status |
| --- | --- | --- | --- | --- | --- |
| error | `FND-SEC-002` | 3 |   | Evidence: Unicode 日本語 and emoji 😀 and ANSI red<br>Recommendation: Fix it. | suppressed until 2027-01-01<br>reason: accepted risk<br>certain |

### `(no file)`

| Severity | Rule | Position | Resource | Details | Status |
| --- | --- | --- | --- | --- | --- |
| info | `FND-ENV-003` |   | AZURE\_LOCATION | Evidence: Key AZURE\_LOCATION is unset<br>Recommendation: Set it. | baselined<br>uncertain |

## Skipped checks (3)

| Rule | Reason | Missing capability | Detail |
| --- | --- | --- | --- |
| `FND-IDN-004` | profile-key-missing:policy.allowedExternalScopes | profile key policy.allowedExternalScopes |   |
| `FND-IDN-005` | profile-key-missing:policy.allowedExternalScopes |   |   |
| `FND-NET-009` | missing-input | compiled ARM template | no compiled ARM |

## Owner summary

| Errors | Warnings | Info | Suppressed | Baselined | Skipped | Hidden |
| --- | --- | --- | --- | --- | --- | --- |
| 2 | 1 | 0 | 1 | 1 | 3 | 4 |

Top rules:

- `FND-SEC-002`: 1 (error)
- `FND-SYS-INVALID-AZURE-YAML`: 1 (error)
- `FND-NET-001`: 1 (warning)

Rules skipped because a profile key is missing:

- `profile-key-missing:policy.allowedExternalScopes`: `FND-IDN-004`, `FND-IDN-005`

Exit code: 1

**Skipped is not passed: a skipped check was not evaluated and says nothing about the project.**
