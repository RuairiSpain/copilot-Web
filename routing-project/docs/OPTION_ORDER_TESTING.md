# Option-order testing (`--shuffle-options`)

This guide is for developers running the comparison harness. It explains what the
option-order test measures, how to run it, how to read the results, and what to do about them.

## Why test option order

Every request to Decision-1 lists the stage-1 models as the keys of `criteria`. The service
always sends them in the same order: cheapest first. If Decision-1 gave extra weight to an
option because of where it sits in that list, routing would carry a hidden bias towards
the cheapest (or most expensive) model that no change to the descriptions would explain.

Microsoft's documentation warns that "scores can change based on how you phrase or order
questions and options" and recommends testing "whether changing the order affects results".
Microsoft's launch post reports no flips when options are reversed or shuffled on its own
benchmarks. This test checks whether that holds for our state, instructions and model
descriptions.

## What the test does

For each prompt, after the normal Decision-1 call, the harness asks the same question again
N more times. Each repeat is identical to the original request (same `state`, same
`instructions`, same option keys and descriptions) except for the order of the keys in
`criteria`.

- The reorderings are random, but reproducible: each is seeded from `--seed` and the dataset
  row ID, so rerunning with the same seed sends the same orders.
- A reordering is never the original order. When a shuffle happens to reproduce it, the list
  is rotated by one.
- The reshuffled calls are measurement calls only. They do not change which model serves the
  prompt, are not written to the decision log, and do not count towards the latency figures.
- When stage 1 leaves a single model, Decision-1 is not called, so there is nothing to test.

The code is `shuffled_orders()` and `option_order_test()` in
`decision-router/decision_router/comparison.py`.

## Running it

```bash
cd routing-project/decision-router

# One reshuffled call per prompt
python scripts/compare_with_model_router.py --sample 400 --shuffle-options --output-dir eval/order-001

# Three reshuffled calls per prompt: a tighter estimate, three times the Decision-1 calls
python scripts/compare_with_model_router.py --sample 400 --shuffle-options 3 --output-dir eval/order-002

# Check the decision side only, without generating with our models
# (Model Router still generates: it has no decide-only call)
python scripts/compare_with_model_router.py --sample 400 --shuffle-options 3 --decide-only --output-dir eval/order-003

# Without credentials, against the in-memory fake (checks the harness, not Decision-1)
python scripts/compare_with_model_router.py --dry-run --sample 64 --shuffle-options 2 --output-dir eval/dry-run
```

**Cost.** Each repeat is one extra Decision-1 call per prompt. Decision-1 bills input tokens
only, at $0.042 per million (list price, October 2026). For example, a typical routing
request here is around 400 input tokens, so 400 prompts × 3 repeats ≈ 480,000 tokens ≈
$0.02. The model calls and Model Router calls cost far more than the test does.

## Reading the results

`report.md` gets an **Option-order test** section, and `report.json` has the same figures
under `summary.option_order`:

| Measure | Meaning |
|---|---|
| `calls` | Reshuffled Decision-1 calls that succeeded |
| `top1_flip_rate` | Share of calls where the highest-probability model changed |
| `served_first_flip_rate` | Share of calls where the model we would call first changed, after the low-confidence rule. This is the number that affects users. |
| `ranking_change_rate` | Share of calls where any position in the ranking changed. Swaps between near-equal low-probability models count here. |
| `mean_max_probability_shift` | Average, over calls, of the largest change in any one model's probability |
| `by_mode` | The same figures for `cost`, `balanced` and `quality` |
| `errors` | Reshuffled calls that failed, by error code |

Each row of `results.jsonl` has an `option_order` list with one entry per repeat: the order
sent, the ranking and probabilities returned, and the three flags. Use it to find the
prompts that flipped:

```bash
jq -c 'select(any(.option_order[]?; .served_first_changed))
       | {id, routing_mode, base: .decision1.ranking, shuffled: [.option_order[].ranking]}' \
   eval/order-001/results.jsonl
```

## What to do with the results

These are starting points, not validated limits; set your own based on the cost of a wrong
route.

- **First-call flips near 0, and a probability shift of a few hundredths or less.** Order does
  not matter for this workload, so no action is needed. Rerun the test whenever the catalog
  descriptions, the instructions or the Decision-1 deployment version change.
- **First-call flips of a few percent, concentrated where the top probability is low.** These
  are near-ties, where any perturbation can change the result. Check them against the
  low-confidence threshold sweep in the same report; the threshold may already route these
  cases.
- **First-call flips spread across confident decisions, or a consistent lean towards
  whichever model is listed first.** That is position bias. Options, in order of preference:
  1. Make the descriptions more distinct, so the decision depends on their content. This is
     Microsoft's first recommendation for wording sensitivity.
  2. Randomise the option order per request in production, so any bias averages out rather
     than always favouring the same model. This is not implemented; it would be a small
     change in `RouterPipeline.route`, at the cost of routing that is no longer
     deterministic for a given input.
  3. Average the probabilities over two or more orders. This doubles the Decision-1 calls
     and adds latency.

To check for a lean towards a particular position, compare each flipped call's `order[0]`
with its new top model in `results.jsonl`.

## How the test is tested

`decision-router/tests/test_comparison.py` has:

- a check that reorderings are reproducible and never the original order;
- a run against the fake Decision-1 with no position effect, which must report zero changes;
- a run against the fake with `position_bias` set (the first-listed option's score is
  multiplied), which must report flips, and must show that the criteria order sent to
  Decision-1 really changed;
- an end-to-end dry run with `--shuffle-options 2`, which checks the report section and the
  call count.
