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
Foundry OpenAI-compatible endpoint. For the LLM call, tokens come only from:

- `azd auth token --scope <scope>`
- `az account get-access-token --resource <scope without /.default>`

`AZURE_ACCESS_TOKEN` is **never** used for the LLM call, even when it is set.
That variable carries a token of unknown audience (normally Azure Resource
Manager, `https://management.azure.com`); a token issued for one audience does
not work with another, and forwarding a management-plane token to an inference
host would disclose it. The LLM provider therefore uses the azd/az chain
without the environment source (`azure.NewCLICredentialForTenant`). Other
commands (`preflight`, `runtime`, `graph`) keep the full chain described in
[configuration.md](configuration.md). In a managed-identity or CI environment
where only `AZURE_ACCESS_TOKEN` is available, `--llm-explain` falls back to
"LLM advisory unavailable".

The token is requested for the CLI's default tenant; there is no tenant
override for the LLM call yet. If you are signed in to several tenants, sign in
to (or set the CLI default to) the tenant that owns the inference resource,
otherwise the provider returns 401 and the advisory falls back.

No API keys or bearer tokens are read from or stored in repository config
files, and no `api-key` header is ever sent.

### Required environment variables

- `FOUNDRY_DOCTOR_LLM_ENDPOINT` or `AZURE_OPENAI_ENDPOINT`
- `FOUNDRY_DOCTOR_LLM_DEPLOYMENT` or `AZURE_OPENAI_DEPLOYMENT`

Optional:

- `FOUNDRY_DOCTOR_LLM_PROVIDER` to override the default provider
- `FOUNDRY_DOCTOR_LLM_SCOPE` to override the default token scope. Only two
  values are accepted; anything else (for example an ARM or Graph scope) makes
  the advisory unavailable and no token is requested:
  - `https://ai.azure.com/.default` (default)
  - `https://cognitiveservices.azure.com/.default`

The default scope is `https://ai.azure.com/.default`; Microsoft Learn states
that tokens for the `/openai/v1/` route "must be issued with scope
https://ai.azure.com/.default" (endpoints page, below). Current Learn REST
samples for the same route still obtain tokens with
`--resource https://cognitiveservices.azure.com` (reasoning-models page,
below), so that audience is also allow-listed. UNVERIFIED: Learn does not
explicitly state that `cognitiveservices.azure.com` tokens remain accepted on
`/openai/v1/`; prefer the default.

**Ambient configuration warning:** `AZURE_OPENAI_ENDPOINT` and
`AZURE_OPENAI_DEPLOYMENT` are read as fallbacks. If another application set
them in your shell, `--llm-explain` sends the projected payload to that
resource. Set the `FOUNDRY_DOCTOR_LLM_*` variables explicitly to avoid this.

Accepted endpoint forms are:

- `https://<resource>.openai.azure.com`
- `https://<resource>.openai.azure.com/openai/v1/`
- `https://<resource>.services.ai.azure.com/openai/v1/`

Explicit ports, query strings, user info, Foundry project endpoints
(`.../api/projects/<p>`) and sovereign-cloud hosts (`*.openai.azure.us`,
`*.openai.azure.cn`) are rejected by design.

### Roles

The signed-in identity needs a data-plane inference role on the resource;
management-plane roles such as Owner or Contributor are not sufficient by
themselves for keyless inference:

- Microsoft Foundry resource: **Foundry User** (previously named *Azure AI
  User*; Learn calls it the least-privilege Foundry role).
- Azure OpenAI resource: **Cognitive Services OpenAI User** (role name taken
  from the Azure OpenAI RBAC page cited below; not re-fetched in this loop).

Do not grant *Cognitive Services Contributor* for this purpose: it is a
control-plane role that also allows key listing. Role assignments can take a
few minutes to propagate; until then the provider returns 401/403 and the
advisory falls back.

### Network

- The client honours `HTTPS_PROXY` / `NO_PROXY` (Go `ProxyFromEnvironment`).
- Redirects are never followed: any 3xx response makes the advisory
  unavailable, so neither the payload nor the token is re-sent to another host.
- Private endpoints work through the public FQDN when your DNS resolves it to
  the private IP (`privatelink.openai.azure.com`). With
  `publicNetworkAccess=Disabled` and no private route, the service answers 403
  and the advisory falls back.

### Model and deployment

- The deployment must support structured outputs (`response_format` of type
  `json_schema` with `strict: true`); see the supported-models list on the
  structured-outputs page below.
- `temperature` is not sent. Reasoning deployments (GPT-5 family, o-series) do
  not accept the same parameter set as other chat models, and determinism is
  enforced by response validation, not sampling.
- `max_completion_tokens` is set to 4096. On reasoning models this cap covers
  reasoning and visible tokens; if it is reached the response ends with
  `finish_reason: "length"` and the advisory is rejected as truncated. A
  non-reasoning, structured-output-capable deployment is recommended.
- Data residency: prompts sent to a *Global* deployment type may be processed
  in any Azure region. If you have EU/US residency obligations, point
  `FOUNDRY_DOCTOR_LLM_DEPLOYMENT` at a Data Zone or regional Standard
  deployment (deployment-types page below).

## Data minimization and redaction

Before any provider call, Foundry Doctor projects deterministic results into an
allow-listed DTO:

- rule ID
- approved rule description / explanation
- redacted evidence summary
- approved recommendation text
- the audience (`owner` or `developer`)

No other run metadata (profile, environment, subscription, scope note) is sent.

Foundry Doctor **does not send**:

- raw `azure.yaml`, Bicep, ARM, or other source files
- prompt or completion content from workloads
- document contents from Search or data-plane sources
- full Azure resource IDs
- bearer tokens, secrets, or connection strings
- payload logs

Assessment evidence is reduced to sanitized deterministic presence/location
summaries; raw finding evidence text is not sent. Free-text skip reasons (which
come from adapters and the Bicep CLI) are included only after secret
redaction plus replacement of ARM resource IDs (`<azure-resource-id>`), GUIDs
(`<id>`) and DNS host names (`<host>`). ARM resource IDs and GUIDs are also
removed from all other projected text. Approved rule explanation and
recommendation text are redacted and truncated before inclusion.
Prompt text treats all projected finding content as **untrusted data** and
never as instructions.

## Validation and safety checks

Provider responses must be structured JSON matching the repository-owned
response schema. The whole response is rejected (never partially kept) if it:

- lists a rule ID in `ruleIds` that was not in the projected input
- mentions anything shaped like a rule ID (`FND-<AREA>-<NNN>`) in the summary,
  a heading, a section summary or an action that was not in the projected input
- omits any rule ID that was in the projected input
- includes an action that is not verbatim deterministic recommendation text
- is a model refusal, was truncated (`finish_reason: "length"`) or was blocked
  by the content filter
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
- output tokens: `max_completion_tokens` 4096 (one provider call per command;
  the prompt is at most 16 KiB, roughly 4k input tokens)
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

## Samples

See [`samples/llm/`](../samples/llm/README.md) for a projected request, the
provider request shape, an accepted response and two rejected responses.

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
- Reasoning models (parameter support, `max_completion_tokens`, cognitiveservices
  audience in REST samples):
  <https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/reasoning>
  (verified 2026-10-18)
- Role-based access control for Microsoft Foundry (Foundry User, formerly
  Azure AI User):
  <https://learn.microsoft.com/en-us/azure/foundry/concepts/rbac-foundry>
  (verified 2026-10-18)
- Azure OpenAI role-based access control (Cognitive Services OpenAI User),
  cited by the Phase 9 Azure black belt review:
  <https://learn.microsoft.com/en-us/azure/foundry-classic/openai/how-to/role-based-access-control>
  (not re-fetched 2026-10-18)
- Deployment types (Global vs Data Zone vs Standard):
  <https://learn.microsoft.com/en-us/azure/foundry/foundry-models/concepts/deployment-types>
  (cited by review, not re-fetched 2026-10-18)
- Quotas and limits for Azure OpenAI in Microsoft Foundry Models:
  <https://learn.microsoft.com/en-us/azure/foundry/openai/quotas-limits>
  (verified 2026-10-06)
