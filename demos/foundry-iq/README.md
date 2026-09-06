# Foundry IQ demo — one knowledge base, two very different sources

A Foundry IQ knowledge base over:

| Source | Kind | What it holds | Who designed the index |
| --- | --- | --- | --- |
| `eu-directives-ks` | `azureBlob` | ~60 EU directives bearing on Spanish employment law, from EUR-Lex | **Foundry IQ** — generated |
| `hr-templates-ks` | `searchIndex` | HR samples and templates: contracts, policies, procedures, letters, forms | **You** — hand-built |

That asymmetry is the demo. Agentic retrieval routes each subquery to whichever
source can answer it, and the two sources get there by opposite routes.

---

## ⚠️ What you can and cannot customise — the flag you asked for

**You asked to be told if Foundry IQ won't let you customise the index it
creates. It won't, and here is the precise shape of that.**

When you create an `azureBlob` knowledge source, Azure AI Search generates a
complete indexer pipeline for you, named after the source:

```
eu-directives-ks-datasource
eu-directives-ks-skillset
eu-directives-ks-index        <-- schema chosen by the service
eu-directives-ks-indexer
```

The knowledge source payload has exactly one configuration surface,
`ingestionParameters`, and it accepts:

`networkAccessMode` · `identity` · `disableImageVerbalization` ·
`chatCompletionModel` · `embeddingModel` · `contentExtractionMode` ·
`ingestionSchedule` · `ingestionPermissionOptions` · `assetStore` · `aiServices`

**There is no field for the index schema.** No field definitions, no analyzers,
no vector profile or HNSW parameters, no semantic configuration, no filterable
metadata of your choosing. You pick the models and the extraction mode; the
service picks everything else.

Two honest caveats on top of that:

- The generated objects *are* ordinary search objects, so they're visible
  through the index API. Editing them is undocumented, and the pipeline owns
  them — expect changes to be overwritten on re-ingestion. Treat the generated
  index as managed, not yours.
- The docs don't state a supported customisation path either way. What's above
  is read off the API surface, not from an explicit "you cannot" statement. If
  a future api-version adds a schema hook, this section is what to re-check.

**So: your hybrid + vector + exact-match-metadata requirement is exactly why the
second source is a `searchIndex` knowledge source.** You build the index
(`scripts/index_schema.py`), you own every field, and the knowledge source just
points at it. That is the supported way to keep control of retrieval while
still getting IQ's routing and query planning on top.

If you want *only* generated sources, you get IQ's defaults and no metadata
filters of your own design. If you want your schema, you bring your own index.
There is no third option today.

---

## The four commands

```bash
cp .env.example .env            # or run scripts/provision_infra.sh and paste
set -a && . ./.env && set +a
pip install -r requirements.txt
```

### 1. Build the corpora

```bash
python scripts/1_scrape.py --limit 100
```

Fetches EU directives from EUR-Lex by CELEX id (PDF, DOC and flattened HTML from
the same URI, which is where the format mix comes from) and HR material from an
allowlist of hosts whose terms permit reuse. robots.txt is honoured, requests
are rate-limited, and `corpus/manifest.json` records the real URL, content type
and sniffed format of every file.

Two deliberate choices worth knowing:

- **The CELEX list is a seed, not a crawl.** Deterministic, and a stale id costs
  a log line rather than a broken run.
- **Nothing is fabricated.** If the sources yield fewer documents or a thinner
  format mix than the budget, the run says so. `--synthesize-office` will derive
  `.docx`/`.pptx` from fetched text if you need format coverage for a demo, but
  every derived file is labelled `derived_from` in the manifest.

Adding hosts with `--allow-domain` prints a reminder to check the licence first —
HR template sites commonly forbid redistribution, and a demo corpus is a copy.

### 2. Provision the knowledge base

```bash
python scripts/2_provision.py --project my-foundry-project
```

Six steps, each printing the REST payload it sends:

1. create the HR index — hybrid (BM25 + HNSW), an Azure OpenAI vectorizer on the
   vector profile so queries never have to embed client-side, a semantic
   configuration, and six filterable/facetable metadata fields
2. extract, chunk, embed and push the HR corpus
3. upload the directives to blob storage
4. `PUT /knowledgesources/eu-directives-ks` — kind `azureBlob`
5. `PUT /knowledgesources/hr-templates-ks` — kind `searchIndex`, with
   `baseFilter`, `semanticConfigurationName`, `sourceDataFields` and `queryHints`
6. `PUT /knowledgebases/es-employment-kb` over both

**How metadata filters get applied before the vector search.** The `queryHints`
on the search-index knowledge source tell the planner which fields are worth
filtering on *and what values they take* — `fieldValues` is required, because a
field name alone doesn't tell the planner that "dismissal letters" maps to
`doc_type eq 'letter'`:

```json
"queryHints": {
  "filters": [
    { "field": "doc_type",
      "fieldValues": ["contract", "policy", "procedure", "letter", "form", "guidance"],
      "filterInstructions": "Filter on the kind of document the user is asking for." }
  ]
}
```

Ask "show me dismissal letter templates" and the planner emits a subquery with a
`$filter` *plus* a vector query — narrowing the candidate set first, then
ranking within it. The trace shows both halves: `searchIndexArguments.filter`
for the filter that ran, and **`queryHintProcessing.generatedFilter`** for the
one the planner derived from these hints specifically.

### 3. Query it

```bash
python scripts/3_search.py "What must a written statement of employment contain?"

# restrict to one source, with your own filter on top of baseFilter
python scripts/3_search.py "dismissal letter wording" \
  --source hr-templates-ks --filter "doc_type eq 'letter'"

# turn the planner down and watch the subqueries disappear
python scripts/3_search.py "..." --effort minimal

# raw grounding documents instead of a synthesised answer
python scripts/3_search.py "..." --output-mode extractiveData

# two turns, so you can see context reuse
python scripts/3_search.py "What is the working time limit?" --follow-up "And the exceptions?"
```

Every run writes `.trace/latest.json`.

### 4. Show the pipeline

```bash
# demo-friendly: run this in a second pane before command 3
python scripts/4_trace.py --watch

python scripts/4_trace.py --live "What must a written statement contain?"
python scripts/4_trace.py --replay .trace/latest.json
python scripts/4_trace.py --log-analytics --workspace "$LOG_ANALYTICS_WORKSPACE"
```

Output looks like:

```
pipeline
  ▸ plan       420 ms  in=1,204 out=88  gpt-5.4-mini
  ▸ search     310 ms  eu-directives-ks [azureBlob]  docs=6
      search: "written statement of employment particulars"
  ▸ search     290 ms  hr-templates-ks [searchIndex]  docs=4
      search: "written statement template"
      filter: language eq 'en' and doc_type eq 'contract'
      queryType: semantic
      semantic: hr-semantic
      hint filter: doc_type eq 'contract'
  ▸ synth      980 ms  in=8,102 out=412  gpt-5.4-mini
  3 subqueries | 10 documents | 9,306 in / 500 out tokens | 2,041 ms wall
```

Both knowledge-source kinds render as sources: `KnowledgeBaseActivityRecordType`
has one member per source kind (`searchIndex`, `azureBlob`, `web`, `file`, …),
each with its own `<kind>Arguments` object, so a blob source is not an
unrecognised record.

**One thing to be straight about.** The `activity` array arrives *with* the
response — agentic retrieval is a single HTTP call and there is no event stream
of subqueries as the planner issues them. So `--watch` (tail the trace file from
a second terminal) is the honest version of "live during command 3", and
`--live`'s ticker is a progress indicator, not streamed internals. The
server-side view that *is* independent of the response is
`--log-analytics`, which needs diagnostic settings on the search service
(`provision_infra.sh --with-diagnostics`).

---

## Notes and limits

- **Reasoning effort caps what the planner may do.** `minimal` = up to 10
  sources, no subqueries, no LLM synthesis. `low` = 3 sources, 3 subqueries,
  5,000-token synthesis budget. `medium` = 5 and 5, 10,000 tokens. `auto` lets
  the service choose. Command 3's `--effort` is the fastest way to show what the
  planner is actually buying you.
- **API version.** Agentic retrieval is GA on `2026-04-01`, but answer
  synthesis, the preview knowledge-source kinds and the token counters in
  `activity` need `2026-08-01-preview`, which is what the scripts pin. Every
  payload in this folder was checked against that version's `search.json` in
  `Azure/azure-rest-api-specs`.
- **Entra-only auth.** No keys anywhere. `provision_infra.sh` sets
  `--disable-local-auth true` on the search service, which is what actually
  turns keys off — `--auth-options aadOrApiKey` *permits* them and is mutually
  exclusive with it. It also assigns the roles, including the search service's
  own managed identity for blob read and for the query-time vectorizer.
- **`az search service create` has no `--identity-type`.** That argument exists
  only on `update` (see `search/custom.py::update_search_service` in azure-cli),
  so the script creates the service and then assigns the identity in a second
  call. Passing it to `create` fails with "unrecognized arguments".
- **The data plane is OData-addressed.** Every resource path is
  `/collection('name')`, not `/collection/name` — `/knowledgebases('kb')`,
  `/knowledgesources('ks')`, `/indexes('idx')`, and document upload at
  `/indexes('idx')/docs/search.index`. `_common.odata()` does the quoting,
  including doubling a quote inside a name.
- **Enum values that don't read the way you'd guess.** `outputMode` is
  `extractiveData` (not `extractedData`) or `answerSynthesis`. Every entry in
  `knowledgeSourceParams` needs a `kind` discriminator (`searchIndex`,
  `azureBlob`, …) alongside `knowledgeSourceName`, and `filterAddOn` exists only
  on the `searchIndex` variant — `3_search.py` fills the kind in automatically
  and `--source-kind NAME=KIND` overrides it.
- **There is no streaming retrieve in this api-version.** The spec defines
  `KnowledgeBaseRetrievalStreamEvents` and friends, but the definition is
  empty and `retrieve`'s `Accept` header accepts only
  `application/json;odata.metadata=minimal`. So the activity array really does
  arrive with the response, and `--watch` remains the honest "live" view.
