# LLM advisory explanations

Foundry Doctor can append an **advisory** LLM-generated narrative to selected
human-facing outputs. This feature is **off by default** and runs only when you
 pass `--llm-explain` on the command line.

Deterministic findings remain authoritative:

- the LLM never creates, removes, suppresses, or baselines findings
- the LLM never changes severity, rule state, readiness, exit code, or WAF state
- if the provider is unavailable or the response is rejected, Foundry Doctor
  falls back to the deterministic report and labels the advisory as unavailable

## Supported commands

- `foundry-doctor explain <rule-id> [--llm-explain]`
- `foundry-doctor assess waf [--llm-explain]` for `owner` and `developer`
  audiences only

The evidence-pack and machine-readable report contracts remain deterministic and
do not call the provider.

## Provider and authentication

Phase 9 MVP supports one allow-listed provider:

- `azure-openai`

The provider uses Microsoft Entra ID authentication against the Azure OpenAI /
Foundry OpenAI-compatible endpoint. Credentials come only from:

- `azd auth token`
- `az account get-access-token`

No API keys or bearer tokens are stored in repository config files.

### Required environment variables

- `FOUNDRY_DOCTOR_LLM_ENDPOINT` or `AZURE_OPENAI_ENDPOINT`
- `FOUNDRY_DOCTOR_LLM_DEPLOYMENT` or `AZURE_OPENAI_DEPLOYMENT`

Optional:

- `FOUNDRY_DOCTOR_LLM_PROVIDER` to override the default provider
- `FOUNDRY_DOCTOR_LLM_SCOPE` to override the default token scope

The default scope is `https://ai.azure.com/.default`, matching current
Microsoft Learn examples for OpenAI-compatible `/openai/v1/` inference with
Microsoft Entra ID on Microsoft Foundry / Azure OpenAI-compatible endpoints.

Accepted endpoint forms are:

- `https://<resource>.openai.azure.com`
- `https://<resource>.openai.azure.com/openai/v1/`
- `https://<resource>.services.ai.azure.com/openai/v1/`

The identity also needs an inference-capable resource role. Management-plane
roles such as Owner or Contributor are not sufficient by themselves for
keyless inference.

## Data minimization and redaction

Before any provider call, Foundry Doctor projects deterministic results into an
allow-listed DTO:

- rule ID
- approved rule description / explanation
- redacted evidence summary
- approved recommendation text
- audience and narrow run metadata for assessment narratives

Foundry Doctor **does not send**:

- raw `azure.yaml`, Bicep, ARM, or other source files
- prompt or completion content from workloads
- document contents from Search or data-plane sources
- full Azure resource IDs
- bearer tokens, secrets, or connection strings
- payload logs

Assessment evidence is reduced to sanitized deterministic presence/location
summaries; raw finding evidence text is not sent. Approved rule explanation and
recommendation text are redacted and truncated before inclusion.
Prompt text treats all projected finding content as **untrusted data** and
never as instructions.

## Validation and safety checks

Provider responses must be structured JSON matching the repository-owned
response schema. Responses are rejected if they:

- reference unknown rule IDs
- claim that findings disappeared or all controls pass
- mention severity, exit codes, suppressions, or baselines as if they changed
- exceed size limits
- fail JSON parsing or schema checks

Rendered output is escaped before Markdown or HTML presentation.

## Bounded limits

- provider timeout: default 10s, bounded to 1s–30s; advisory timeout falls
  back to deterministic output
- projected prompt size: 16 KiB max
- provider response size: 12 KiB max
- assessment projection: top 10 non-pass controls only

## Provenance markers

Generated sections are labeled **Advisory LLM narrative** and include:

- provider name
- model name when returned
- deployment name
- prompt version

## Data flow

```text
deterministic rule / assessment output
  -> allow-list projection
  -> redaction + truncation
  -> versioned repository prompt
  -> Azure OpenAI structured response
  -> response validation / prohibited-claim checks
  -> escaped advisory appendix
```

## References

- Azure OpenAI / Foundry OpenAI-compatible endpoints:
  <https://learn.microsoft.com/en-us/azure/foundry/foundry-models/concepts/endpoints>
  (verified 2026-10-06)
- Structured outputs with Azure OpenAI in Microsoft Foundry Models:
  <https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/structured-outputs>
  (verified 2026-10-06)
- Microsoft Entra ID authentication for Azure OpenAI (classic guidance for the
  same endpoint family):
  <https://learn.microsoft.com/en-us/azure/foundry-classic/openai/how-to/managed-identity>
  (verified 2026-10-06)
- Quotas and limits for Azure OpenAI in Microsoft Foundry Models:
  <https://learn.microsoft.com/en-us/azure/foundry/openai/quotas-limits>
  (verified 2026-10-06)
