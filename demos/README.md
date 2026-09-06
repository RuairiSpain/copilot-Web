# Microsoft Foundry demos

Three self-contained folders. Nothing here is wired into the Next.js app — they
share the repo, not the runtime.

| Folder | Shows | Entry point |
| --- | --- | --- |
| [`foundry-iq/`](foundry-iq/) | One knowledge base, two knowledge sources (one generated, one hand-built), agentic retrieval, and a live pipeline trace | `scripts/1_scrape.py` → `4_trace.py` |
| [`foundry-toolbox/`](foundry-toolbox/) | Every configurable tool type in one toolbox, MCP routing through tool search, and a whole-project inventory | `scripts/create_toolbox.py` |
| [`cli/`](cli/) | Standing up a Foundry project with `az` and `azd ai` only, no portal | `README.md` |

## Three things worth knowing before you start

**1. Foundry IQ won't let you design the index it generates.** A blob knowledge
source's only configuration surface is `ingestionParameters` — models, network
mode, extraction mode, schedule. No fields, analyzers, vector profile or
semantic configuration. If you need control over retrieval, bring your own
index and use a `searchIndex` knowledge source. The full flag is in
[`foundry-iq/README.md`](foundry-iq/README.md#-what-you-can-and-cannot-customise--the-flag-you-asked-for).

**2. Toolbox tool search is not automatic.** Add `{"type": "toolbox_search"}`
to the version or every tool definition goes to the model on every turn.

**3. There is no `az foundry`.** Resources are `az`; project objects are
`azd ai …` extensions.

## Running the tests

```bash
python -m unittest discover -s demos/tests -v
```

25 tests over the logic that doesn't need a subscription: index schema
invariants, corpus chunking and classification, the trace renderer, the toolbox
YAML emitter (round-tripped through PyYAML), tool selection, and the inventory's
Markdown rendering. `azure.identity` is stubbed at import, so the tests never
authenticate and never reach Azure.

## What's verified, and how

A review pass checked the SDK and CLI surfaces against their **source**, not
against documentation prose — and doc prose turned out to be wrong in several
places.

**Verified against `azure-ai-projects` 2.6.0** (wheel downloaded and
introspected): the `project.toolboxes` operations group and its real method
names, every `ToolboxToolType` discriminator, the field set of each
`*ToolboxTool` model, the `allow_preview` client flag, and
`AzureAISearchQueryType`. This caught four invalid tool-type names, an invented
one, two payloads that are nested rather than flat, and two fields that don't
exist on the toolbox variant of the MCP tool. `demos/tests` now asserts all of
it so the mistake can't come back.

**Verified against the azure-cli source** (`command_modules/{search,cognitiveservices}`):
`az search service create` takes no `--identity-type`; `--disable-local-auth` is
what makes a search service Entra-only; the deployment-create flags match the
CLI's own example; `--custom-domain` and `--assign-identity` are real.

**Verified against Azure OpenAI docs**: the v1 path `{endpoint}/openai/v1/…`
with the deployment name in the body's `model` field.

**Still unverified.** Nothing here has been run against a live subscription —
this environment has none, and its `cryptography` build is broken so the Azure
SDKs can't even be imported. Specifically outstanding: the Foundry IQ REST
payloads (`queryHints` above all), Foundry project creation via ARM, and every
`azd ai` command line, since neither `az` nor `azd` is installed here. Each is
flagged in the README where it lives.
