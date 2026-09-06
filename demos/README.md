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

## Honesty about what's verified

The scripts were written against the current Foundry and Azure AI Search
documentation, and the payload shapes come from those docs rather than from
memory. **They have not been run against a live Azure subscription** — this
environment has none, and its Python can't even load `azure-identity`. What
*was* verified here: every module compiles, the pure-logic paths are unit
tested, and the YAML emitter round-trips through a real parser.

The two places most likely to need adjustment on first contact are called out
where they live: `queryHints` on the search-index knowledge source
([`foundry-iq/README.md`](foundry-iq/README.md)), and project creation plus
`azd ai` flags ([`cli/README.md`](cli/README.md)). Both sections say what to
check and what to do if the shape has moved.
