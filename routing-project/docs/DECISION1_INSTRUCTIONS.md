# What Decision-1 is told: instruction templates

Stage 1 is rule-based and calls no model. Decision-1 receives three things for each request:

- `state`: the conversation rendered as text (`decision1.build_state`).
- `instructions`: what to optimise for, built from fixed fragments.
- `criteria`: one entry per candidate, built from a fixed template.

The fragments and templates are in `decision-router/config/decision1_instructions.json`.
Stage 1's results decide which fragments apply and fill their placeholders
(`decision-router/decision_router/instructions.py`). The same request and catalog always give
the same text, and every sentence Decision-1 can see is in that one file.

## How the instructions are built

1. Stage 1 runs and records facts:
   - the routing mode;
   - the profile, if `model` named one;
   - `routing_constraints.actions`;
   - the cost band and how it was given: a named band, a dollar range, or an action price group;
   - what the request needs (tools, JSON schema, images, a reasoning mode);
   - the estimated prompt and output tokens;
   - whether a pinned conversation had to be re-routed;
   - whether the body is a native Anthropic request.
2. Each fragment whose `when` conditions all hold, and whose placeholders all have values, is
   used. Used fragments are joined in file order.
3. If the text is longer than `max_chars` (1,500), fragments are dropped from the end. `base`
   and the mode fragment are never dropped. Dropped ids are logged.

A request with none of these extras gets exactly the catalog's routing-mode instructions, so
the change adds sentences only when stage 1 knows something more.

| Fragment | Used when | Fills in |
|---|---|---|
| `base` | always | – |
| `mode.cost` / `mode.balanced` / `mode.quality` | that routing mode | – |
| `profile.coding` | `model` is `decision-router-coding` | – |
| `action.code`, `action.evaluate`, `action.extract`, `action.orchestrate`, `action.plan`, `action.reason`, `action.summarize`, `action.translate`, `action.write` | that action is in `routing_constraints.actions` | – |
| `band.named` | a band name without actions (`"routing_cost_band": "high"`) | band name, option count, lowest and highest output price |
| `band.range` | a dollar range (`"2.5-33.33"`) | the range, option count, prices |
| `band.group` | a band name with actions (an action price group) | label, action names, option count, prices |
| `req.tools` | the request has tools, or `capabilities` lists `tools` | tool names |
| `req.structured_output` | `response_format` is `json_schema`, or `capabilities` lists it | – |
| `req.image_input` | the request has images, or `capabilities` lists `image_input` | image count |
| `req.reasoning` | `capabilities` lists `reasoning` | – |
| `size.long_prompt` | estimated prompt ≥ 50,000 tokens | prompt tokens |
| `size.long_output` | requested output ≥ 8,000 tokens | output tokens |
| `pin.rerouted` | a pinned conversation's model no longer passes stage 1 | previous model, reason |
| `passthrough` | `claude_translation: false` | – |

Every fragment is used by at least one example on `STAGE1_EXAMPLES.md` or by a test. That page
shows the rendered instructions for each example that reaches Decision-1.

## Criteria

Each candidate's entry is its catalog description, followed by
`Output price $<price> per million tokens; <rank> of <count> by price among these options (1 is cheapest).`
The rank is among the options Decision-1 is choosing from, not the whole catalog. "3 of 3" is
more useful to it than "rank 19 of 23".

## Design decisions

- **Band and action are stated as facts.** The text says which band or price group the options
  came from and their price span, and what the task is. It does not tell Decision-1 that price
  differences are small or that it should prefer cheaper options; only the mode fragment speaks
  about cost.
- **Profiles can have their own fragment.** It is selected by profile name in the template
  file (`profile.coding`), with no new request field.
- **The wording is not generated.** Changing it means editing the JSON file, which changes
  `template_sha256` in the decision log, so routing results can be compared by template version.
- **The cap is 1,500 characters.** Microsoft's Decision-1 documentation states no limit for
  `instructions`. The longest rendered example is about 600 characters.

## Seeing it

- `/v1/route` returns `decision1_input`: the rendered `instructions`, the fragment ids, and the
  `criteria`.
- The decision log records `instructions.template_sha256`, `instructions.fragments`,
  `instructions.dropped` and `instructions.text`.
- `scripts/export_decision1_requests.py` and the Model Router comparison use the same renderer.

## Changing the wording

Edit `config/decision1_instructions.json`, then run:

```
python scripts/stage1_examples.py    # regenerate STAGE1_EXAMPLES.md with the new text
pytest -q                            # the file is validated when it loads
```

The loader rejects unknown placeholders, conditions and flags, duplicate ids, and an
`action_names` map that does not cover every action. The router refuses to start rather than
send broken text.
