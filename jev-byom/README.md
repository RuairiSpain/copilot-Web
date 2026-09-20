# Jev BYOM decision engine

A frozen base model, a per-scenario calibration layer, and two endpoints that
turn text into a **typed decision with a probability you can act on**:

```http
POST /posthoc_train   → fit temperature + isotonic / numeric calibration for one scenario
POST /decision        → {"value": true, "probability": 0.83}
```

`value` is a `bool`, a class index, or a number, depending on the scenario's
decision type. `probability` is *calibrated*: across the training data, decisions
returned at 0.8 were right about 80% of the time. That is the whole point — a raw
softmax says 0.98 to everything and cannot be thresholded.

Calibration artifacts live in Azure Blob Storage, so they survive restarts and
are shared by every replica. The service runs as a container on Azure Container
Apps and plugs into Azure AI Foundry's Agent Service as a tool or a
bring-your-own-model connection.

## How it fits together

```
        POST /decision                     ┌─────────────────────────────┐
  ─────────────────────────────────────▶   │  FastAPI  (app/server.py)   │
                                           └──────────────┬──────────────┘
                                                          │
                   ┌──────────────────────────────────────┴──────────────────┐
                   ▼                                                         ▼
      ┌─────────────────────────┐                        ┌──────────────────────────────┐
      │ base model (model.py)   │  logits [1, C]         │ registry                     │
      │ DistilBERT / E5, frozen │ ─────────────────────▶ │ (calibration_registry.py)    │
      └─────────────────────────┘                        │  in-memory cache, TTL        │
                                                         │  ▼ miss                      │
                                                         │  Azure Blob Storage          │
                                                         └──────────────┬───────────────┘
                                                                        │ temperature,
                                                                        │ isotonic / bins
                   ┌────────────────────────────────────────────────────┘
                   ▼
      ┌─────────────────────────────────────────┐
      │ calibration (calibration.py)            │   {"value": …, "probability": …}
      │ T-scaling → isotonic / numeric bins      │ ──────────────────────────────────▶
      └─────────────────────────────────────────┘
```

| File | What lives there |
| --- | --- |
| `app/server.py` | FastAPI app, endpoints, auth, error handling |
| `app/engine.py` | the pipeline: labels → artifacts, logits → decision |
| `app/model.py` | base model backends and logit extraction |
| `app/calibration.py` | temperature scaling, isotonic regression, numeric bins, metrics |
| `app/calibration_registry.py` | blob/local object stores, versioning, cache |
| `app/schemas.py` | request/response contracts |
| `config/settings.py` | every tunable, as an environment variable |
| `docker/Dockerfile` | CPU-only two-stage image |
| `scripts/` | provisioning, smoke test, Foundry tool spec |

## Quickstart

```bash
python -m venv .venv && source .venv/bin/activate
pip install -r requirements-dev.txt
cp .env.example .env            # defaults to a local ./.calibration directory

# The `hash` backend needs no weights — good for a first run.
JEV_MODEL_BACKEND=hash JEV_MODEL_NUM_LABELS=3 JEV_LOCAL_REGISTRY_DIR=./.calibration \
  uvicorn app.server:app --reload

python scripts/smoke_test.py --base-url http://localhost:8000
```

Swap `JEV_MODEL_BACKEND=huggingface` and `JEV_MODEL_NAME=<your checkpoint>` once
you have weights. Interactive docs are at `/docs`.

## The API

### `POST /posthoc_train`

```json
{
  "decision_type": "boolean",
  "scenario": "loan-approval",
  "training_data": [
    {"input": {"text": "4 years of history, no arrears"}, "label": true},
    {"input": {"text": "two defaults in 6 months"},       "label": false}
  ]
}
```

Optional: `class_names` (enum), `validation_split`, `numeric_bins`,
`numeric_tolerance`, `notes`.

The response says what was fitted and how well:

```json
{
  "status": "ok",
  "scenario": "loan-approval",
  "decision_type": "boolean",
  "num_classes": 2,
  "num_samples": 400,
  "num_train_samples": 320,
  "num_validation_samples": 80,
  "temperature": 1.87,
  "temperature_clamped": false,
  "calibration_version": "20260919T143012Z-1a2b3c4d",
  "artifacts": ["isotonic.json", "temperature.json"],
  "metrics_before": {"split": "validation", "expected_calibration_error": 0.21, "...": null},
  "metrics_after":  {"split": "validation", "expected_calibration_error": 0.04, "...": null},
  "duration_ms": 812.4
}
```

Compare `metrics_before` with `metrics_after`: that difference is what the
calibration bought you. If ECE barely moves, the base model was already
calibrated for this scenario — or there is not enough data to tell.

`temperature_clamped` is the other thing to read. It means the fit ran to the
edge of `[JEV_TEMPERATURE_MIN, JEV_TEMPERATURE_MAX]` instead of settling
somewhere inside it, which is what a training set the base model cannot see
anything in looks like from in here: the optimiser kept flattening the
distribution until it ran out of room. The calibration is still stored and
still served — but treat it as suspect, and check whether the labels really do
relate to what the model reads.

### `POST /decision`

```json
{"decision_type": "boolean", "scenario": "loan-approval", "data": {"text": "..."}}
```

`data` may hold the model input directly or wrap it as `{"input": {...}}`; both
work. Per-call `threshold` (boolean) and `include_probabilities` (enum) are
accepted.

| decision_type | `value` | `probability` |
| --- | --- | --- |
| `boolean` | `true` / `false` | confidence in the value returned (see `JEV_BOOLEAN_PROBABILITY`) |
| `enum` | class index, with `label` naming it | calibrated probability of that class |
| `numeric` | the calibrated value | share of training samples in that bin that landed within the tolerance band |

### `GET /scenarios`, `GET /health`

`/scenarios` lists every trained `(scenario, decision_type)` pair with its
version, class names and sample count — the quickest way to see what a
deployment actually knows. `/health` reports the model and registry state and
backs the container's health probe.

### Errors

Every failure returns `{"error": "...", "detail": "..."}` with a status that
says what to do about it:

| Status | `error` | Means |
| --- | --- | --- |
| 401 | `unauthorized` | `JEV_API_KEY` is set and the request did not present it |
| 409 | `model_mismatch` | the stored calibration does not fit the running model — retrain |
| 422 | `validation_error` / `invalid_input` / `insufficient_data` | the request cannot be used as sent |
| 424 | `calibration_not_found` | the scenario has never been trained |
| 503 | `registry_unavailable` | blob storage is unreachable or inconsistent |

## How the calibration works

**Temperature scaling** divides the logits by one fitted scalar `T`, chosen by
LBFGS to minimise cross-entropy on the training split. `T > 1` spreads an
over-confident distribution out; `T < 1` sharpens an under-confident one. It
never changes which class wins.

**Isotonic regression** (boolean and enum) then maps each class's probability
onto the frequency actually observed, one-vs-rest, and the row is renormalised.
Each map is fitted as a *monotone increasing* function, so within one class the
ordering of inputs is preserved: if the model scored A above B for that class,
calibration keeps A above B.

Across classes it is a different story, and worth being precise about, because
it is the part that surprises people. The per-class curves differ, and the row
is renormalised afterwards, so **the argmax can move**: a class that
systematically overclaims gets pulled down past a class that underclaims, and
the returned `value` changes. That is deliberate — correcting a class that is
wrong about itself is what per-class calibration is for, and it is how the
engine handles class imbalance — but it does mean a calibrated decision is not
always the base model's raw top class.

What calibration cannot do is invent a signal that is not there. Train a
scenario on labels that contradict the base model and the probabilities
collapse toward the base rate rather than flipping: with nothing to rank on,
every monotone map that fits is close to constant. The engine tells you the
scenario is unlearnable instead of pretending otherwise.

A class with too few examples on *either* side (`JEV_ISOTONIC_MIN_SAMPLES`
positives and negatives both required) keeps its temperature-scaled probability
rather than a two-point step function: a curve fitted from 200 rows of which 3
are positive is exactly the shape the guard exists to prevent, and so is its
mirror image.

**Numeric calibration** collapses the distribution to a scalar score
(`Σ p_c · c / (C-1)`, the normalised expected class index), bins the training
scores by quantile, and stores each bin's mean label and its *coverage* — the
fraction of that bin's samples landing within `JEV_NUMERIC_TOLERANCE` of that
mean. At inference the value is interpolated between bin centres and the
probability is the bin's coverage. So for numeric scenarios, `probability`
answers "how often is an answer from this bin within tolerance?". Numeric
decisions **require** calibration: the label scale lives in the bins, not in the
model.

Artifacts are plain JSON — isotonic regressors are stored as their `(x, y)`
breakpoints, which reproduce scikit-learn's `predict` exactly for the clipped,
increasing case. Nothing is pickled, so loading a blob cannot execute code and a
scikit-learn upgrade cannot invalidate a stored fit.

## Adding a scenario

A scenario is created by training it — there is nothing to register first.

1. **Pick a name and a type.** Names are `[A-Za-z0-9][A-Za-z0-9_.-]*` (they
   become blob path segments). The same name can hold one calibration per
   decision type: `risk` as `boolean` and `risk` as `enum` are separate.
2. **Collect labelled examples.** At least `JEV_TRAIN_MIN_SAMPLES` (default 10,
   and 10 is a floor, not a recommendation — a few hundred is where isotonic
   starts earning its place), covering every class.
3. **POST them to `/posthoc_train`.** For enum, pass `class_names` so decisions
   come back with readable labels.
4. **Check `metrics_after` against `metrics_before`**, then call `/decision`.

Retraining is the same call again: it writes a new version and flips the
pointer, and the in-memory cache of every replica is invalidated on the replica
that served the fit — others pick it up within `JEV_REGISTRY_CACHE_TTL_SECONDS`
(default 5 minutes). Set the TTL lower if you retrain often and need every
replica to switch immediately.

**Rolling back** is a pointer write: every version stays under
`calibration/<scenario>/<type>/versions/<version>/`, so overwriting
`latest.json` with `{"version": "<older>"}` restores it.

```bash
az storage blob download --account-name "$STORAGE" -c jev-calibration \
  -n calibration/loan-approval/boolean/latest.json -f latest.json --auth-mode login
# edit latest.json, then upload with --overwrite
```

## Configuration

Everything is an environment variable; `config/settings.py` holds the defaults
and the validation. The engine refuses to start if no calibration store is
configured, or if the temperature bounds contradict each other — a container
that cannot work fails at startup rather than at the first request.

### Base model

| Variable | Default | Purpose |
| --- | --- | --- |
| `JEV_MODEL_BACKEND` | `huggingface` | `huggingface` or `hash` (no weights, deterministic) |
| `JEV_MODEL_NAME` | `distilbert-base-uncased` | checkpoint id or local path |
| `JEV_MODEL_REVISION` | — | pin the checkpoint to a commit |
| `JEV_MODEL_NUM_LABELS` | `2` | width of the logit vector |
| `JEV_MODEL_MAX_LENGTH` | `256` | tokenizer truncation |
| `JEV_MODEL_BATCH_SIZE` | `16` | batch size while scoring training data |
| `JEV_MODEL_DEVICE` | `cpu` | torch device |
| `JEV_MODEL_TEXT_FIELDS` | `["text","input","prompt","query","content"]` | keys tried when pulling text out of `input` |
| `JEV_MODEL_CACHE_DIR` | — | Hugging Face cache location |
| `JEV_MODEL_EAGER_LOAD` | `true` | load at startup, so a bad checkpoint fails fast |

### Calibration

| Variable | Default | Purpose |
| --- | --- | --- |
| `JEV_TEMPERATURE_INIT` / `_MIN` / `_MAX` | `1.0` / `0.05` / `20.0` | starting point and clamps for `T` |
| `JEV_TEMPERATURE_MAX_ITER` / `_LR` | `100` / `0.05` | LBFGS budget and step size |
| `JEV_ISOTONIC_MIN_SAMPLES` | `8` | a class with fewer positives *or* fewer negatives than this keeps the temperature-scaled probability |
| `JEV_ISOTONIC_OUT_OF_BOUNDS` | `clip` | how inputs outside the fitted range are handled |
| `JEV_NUMERIC_BINS` | `10` | quantile bins |
| `JEV_NUMERIC_MIN_SAMPLES_PER_BIN` | `3` | thinner bins are merged |
| `JEV_NUMERIC_TOLERANCE` | `0.1` | band defining "right", as a fraction of the label range |
| `JEV_NUMERIC_TOLERANCE_ABSOLUTE` | `false` | treat the tolerance as an absolute width |
| `JEV_BOOLEAN_THRESHOLD` | `0.5` | probability at which a boolean decision becomes `true` |
| `JEV_BOOLEAN_PROBABILITY` | `confidence` | `confidence` (of the returned value) or `true` (always P(true)) |
| `JEV_VALIDATION_SPLIT` | `0.2` | holdout used for the reported metrics |
| `JEV_TRAIN_MIN_SAMPLES` / `_MAX_SAMPLES` | `10` / `50000` | accepted training-set sizes |
| `JEV_RANDOM_SEED` | `42` | split seed, so a refit on the same data is reproducible |

### Registry

| Variable | Default | Purpose |
| --- | --- | --- |
| `BLOB_CONN_STR` | — | storage connection string |
| `BLOB_ACCOUNT_URL` | — | `https://<account>.blob.core.windows.net`, with managed identity |
| `BLOB_CONTAINER_NAME` | `jev-calibration` | container holding artifacts |
| `JEV_BLOB_PREFIX` | `calibration` | key prefix, so one container can host several engines |
| `JEV_LOCAL_REGISTRY_DIR` | — | use the filesystem instead of Azure (local dev, tests) |
| `JEV_REGISTRY_CACHE_TTL_SECONDS` | `300` | in-memory cache TTL; `0` disables it |
| `JEV_REGISTRY_CACHE_MAX_ENTRIES` | `256` | cache bound |
| `JEV_REGISTRY_CREATE_CONTAINER` | `true` | create the container when missing |

### Service

| Variable | Default | Purpose |
| --- | --- | --- |
| `JEV_API_KEY` | — | when set, required as `x-api-key` or `Authorization: Bearer` |
| `JEV_REQUIRE_CALIBRATION` | `false` | `true` rejects uncalibrated scenarios instead of falling back to the raw softmax |
| `JEV_LOG_LEVEL` / `JEV_LOG_JSON` | `INFO` / `true` | logging |
| `PORT` | `8000` | listen port |

## Deploying to Azure

```bash
./scripts/provision_azure.sh          # resource group, storage, ACR, env, app
PREFIX=myteam LOCATION=northeurope ./scripts/provision_azure.sh
SKIP_INFRA=1 ./scripts/provision_azure.sh   # rebuild and redeploy only
```

The script creates the resource group, storage account and container, the ACR,
the Container Apps environment and the app; builds the image with `az acr build`
(so the ~1 GB of layers never crosses your uplink); and assigns the app's
system-assigned identity `Storage Blob Data Contributor` and `AcrPull`. Blob
auth defaults to that identity — no account key in an environment variable. Set
`USE_STORAGE_KEY=1` if you cannot create role assignments and need the
connection-string path.

Sizing defaults to 1 vCPU / 2 GiB and `min-replicas 1`: the base model is held
in-process, so scaling to zero means paying the model load on the next request.
Scale with replicas rather than workers — a second uvicorn worker would double
the memory for no gain on a CPU-bound forward pass.

To build and push by hand instead:

```bash
docker build -f docker/Dockerfile -t "$ACR.azurecr.io/jev-byom:latest" .   # context is jev-byom/
az acr login -n "$ACR" && docker push "$ACR.azurecr.io/jev-byom:latest"
```

## Connecting it to Foundry

**As an agent tool (the path this repo scripts).** Generate an OpenAPI 3.0
document for `/decision` and attach it to an agent as an OpenAPI tool:

```bash
python scripts/foundry_tool_spec.py \
  --base-url "https://<app>.azurecontainerapps.io" \
  --verify --api-key "$JEV_API_KEY" \
  --out foundry-decision-tool.json
```

`--verify` sends the document's own example to the live endpoint and checks the
response against the declared schema, so a drift between the spec and the
service fails here rather than inside an agent run. The document deliberately
excludes `/posthoc_train`: an agent that can retrain its own calibration is not
something you want in production. Auth is declared as an `apiKey` header, which
maps onto a Foundry connection holding `JEV_API_KEY`.

Agent instructions that work well with it:

> When a decision needs a yes/no, a category, or a number *with a confidence*,
> call `make_decision` with the matching `scenario`. Treat `probability` as
> calibrated: below 0.7, say the call is uncertain and explain what would settle
> it.

**As a bring-your-own-model connection.** Agent Service can also hold this as a
custom model connection pointing at `https://<app>` with the same key. The
connection payload differs between Foundry API versions, so check it against
the version your project is on before scripting it — the endpoint side needs
nothing beyond what is already here (public HTTPS, a key header, a JSON
request/response, and `/openapi.json` for discovery).

## Testing

```bash
pip install -r requirements-dev.txt
pytest                      # 119 tests, no network, no Azure, no weights
pytest tests/test_calibration.py -v

# Optional: exercise the real transformers backend against a tiny checkpoint.
JEV_TEST_HF_MODEL=hf-internal-testing/tiny-random-DistilBertForSequenceClassification \
  pytest tests/test_model_and_settings.py -k huggingface
```

The suite runs against the `hash` backend and a filesystem store, so it
exercises the real calibration math, the real artifact serialisation and the
real FastAPI wiring without downloading anything. What it covers:

- **Calibration math** — temperature fitting against a dense grid of
  alternatives, isotonic output matching scikit-learn's own `predict` after a
  JSON round-trip, monotonicity, bin merging, tolerance semantics, and each
  metric against a hand-computed value.
- **Registry** — version pointers, rollback, dangling pointers, cache hits,
  scoped invalidation, TTL and size bounds, class-count mismatches, path
  traversal, and the blob store's call shape against a stub container client
  (including the `content_settings` that decides a stored artifact's
  Content-Type).
- **Endpoints** — all three decision types trained and queried end to end,
  scenario switching, retraining, survival across a restart, auth, and every
  error path.

## Known limitations

- **Calibration cannot manufacture a signal.** A scenario the base model gets
  backwards needs a different model or a fine-tune; the calibrated
  probabilities will sit near the base rate to say so. It *can* move the argmax
  between classes (see above), so a calibrated decision is not always the raw
  model's top class.
- **Numeric decisions are bin-resolution.** The value is interpolated between
  bin means, so it cannot be more precise than the training labels support.
  More data lets you raise `JEV_NUMERIC_BINS`.
- **One base model per deployment.** Scenarios share the checkpoint; a scenario
  needing different weights needs its own deployment.
- **`/posthoc_train` is synchronous.** Scoring the training set happens in the
  request, so a very large set can outlast an ingress timeout. Batch it, or run
  training against a dedicated replica.
