#!/usr/bin/env python3
"""Routing dataset v3.1 -- 40,000 prompts for comparing routers.

Used as the prompt set for comparing the Decision-1 pipeline with Microsoft
Foundry Model Router. No model is trained on it. The `selected_model` column is
a hand-written policy label, kept as a reference point, not as ground truth.

Bias controls:
  1. Exact balance across all 16 task_type x quality_preference cells (2,250 each)
  2. Complexity held UNIFORM within every cell (450 per level 1-5) so prompt
     difficulty is statistically independent of the preference label
  3. Template usage balanced within every (task, complexity) group
  4. Global prompt uniqueness enforced, with capacity checks that fail loudly
  5. A held-out template bank -> a 4,000-row set of unseen phrasings

quality_preference maps onto Model Router's routing modes:
  cheap -> cost, medium and high -> balanced, extra_high -> quality
"""
import json, random, csv, os
from collections import Counter, defaultdict

random.seed(31415)

MODELS = ["gpt-5-nano", "gpt-5-mini", "deepseek-v4-flash", "gpt-5.5", "o4-mini", "gpt-5.6-terra"]
PREFS = ["cheap", "medium", "high", "extra_high"]
TASKS = ["code", "reasoning", "rag", "content"]
PER_CELL = 2250          # 16 cells -> 36,000
PER_CX = PER_CELL // 5   # 500 per complexity level per cell
OOD_PER_CELL = 250       # 16 cells -> 4,000 held-out-template examples

# ---------------------------------------------------------------- slot banks
SLOTS = {
    "lang": ["Python", "TypeScript", "Go", "C#", "Rust", "Java", "Kotlin", "Scala", "Ruby", "Swift", "PHP", "Elixir"],
    "lang2": ["Go", "Rust", "TypeScript", "C#", "Python", "Java", "Kotlin", "Elixir", "Scala", "Swift"],
    "unit": ["function", "helper", "class", "module", "utility", "decorator", "generator", "adapter"],
    "service": ["API service", "ingestion pipeline", "worker queue", "batch job", "gRPC service", "web backend",
                 "ETL job", "event consumer", "scheduler", "notification service", "search indexer",
                 "billing processor", "auth gateway", "sync daemon"],
    "simple_op": ["reverses a string", "sums a list of integers", "checks if a number is prime",
                   "formats a date as ISO-8601", "strips whitespace from a list", "converts Celsius to Fahrenheit",
                   "counts vowels in a word", "flattens a nested list", "capitalises each word",
                   "finds the median of a list", "pads a string to fixed width", "removes duplicate characters",
                   "converts bytes to a human-readable size", "checks if two strings are anagrams",
                   "rounds a float to two decimals", "splits a list into chunks", "titles-cases a sentence",
                   "returns the nth Fibonacci number", "validates an email format", "clamps a number to a range"],
    "mid_op": ["parse CSV files", "validate JSON payloads against a schema", "retry HTTP calls with exponential backoff",
                "paginate a REST API", "deduplicate records by key", "stream rows from blob storage",
                "cache expensive lookups in Redis", "normalise addresses from user input", "batch-write to a SQL table",
                "compress and archive old logs", "reconcile two data feeds", "transform XML into JSON",
                "poll a message queue", "enrich records from a lookup service", "chunk documents for embedding",
                "diff two configuration files", "rotate credentials on a schedule", "backfill a partitioned table",
                "detect anomalies in a metrics stream", "export reports to Parquet"],
    "feature": ["idempotent retries", "OAuth token refresh", "rate limiting", "schema evolution", "audit logging",
                 "multi-tenant isolation", "circuit breaking", "incremental checkpointing", "graceful shutdown",
                 "dead-letter handling", "optimistic locking", "back-pressure control", "request tracing",
                 "encryption at rest"],
    "opt_a": ["Azure AI Foundry", "Amazon Bedrock", "a self-hosted vLLM cluster", "Kubernetes", "Snowflake",
               "a managed model router", "Databricks", "Postgres pgvector", "Azure Functions", "Terraform",
               "an event-driven architecture", "a monorepo"],
    "opt_b": ["Amazon Bedrock", "Google Vertex AI", "Azure Container Apps", "BigQuery", "a serverless function stack",
               "Azure AI Search", "Redis vector store", "an in-house inference gateway", "Pulumi",
               "a message-bus architecture", "polyrepo", "a managed Kafka service"],
    "opt_c": ["an open-source stack", "a hybrid deployment", "a third-party SaaS", "the status quo",
               "a phased migration", "a build-versus-buy split"],
    "use_case": ["an enterprise copilot rollout", "a customer support triage system", "a document intelligence platform",
                  "a real-time fraud detection service", "a multi-agent research assistant", "an internal developer portal",
                  "a regulated financial reporting workload", "a global retail recommendation engine",
                  "a clinical documentation assistant", "a supply-chain forecasting system", "a contract review pipeline",
                  "a field-service dispatch platform", "a telemetry analytics backend", "a compliance monitoring service"],
    "concept": ["model routing", "a multi-agent memory architecture", "semantic caching", "cost-aware inference",
                 "retrieval evaluation", "guardrail enforcement", "confidence calibration", "eventual consistency",
                 "a feature store", "prompt versioning", "an evaluation harness", "tiered storage",
                 "zero-trust networking", "a canary release strategy"],
    "horizon": ["6-month", "12-month", "3-year", "two-quarter", "18-month", "five-year"],
    "doc": ["policy document", "support ticket", "contract clause", "research paper", "incident report",
             "product spec", "regulatory filing", "meeting transcript", "knowledge-base article", "audit finding",
             "vendor questionnaire", "design review", "release note", "customer interview"],
    "field": ["effective date", "termination clause", "owner", "SLA target", "root cause", "pricing tier",
               "risk rating", "escalation path", "data retention period", "approval status", "liability cap"],
    "question": ["what the renewal terms are", "who is accountable for the outage", "whether the control is compliant",
                  "what changed since the last revision", "what the reported uptime was", "which vendor obligations apply",
                  "whether the deadline was met", "what the agreed scope covers", "which exceptions were granted",
                  "how the incident was resolved"],
    "n_small": ["2", "3", "4", "5"],
    "n_mid": ["8", "10", "12", "15", "18", "20"],
    "n_large": ["25", "30", "35", "40", "50", "60"],
    "n_xl": ["80", "100", "120", "150", "200", "250"],
    "audience": ["a non-technical stakeholder", "the exec team", "a new starter", "the platform team",
                  "finance", "our security reviewer", "the steering group", "a customer"],
    "domain": ["for the payments team", "in the EMEA region", "for the compliance review", "ahead of the QBR",
                "for the migration workstream", "for the vendor assessment", "for next week's audit",
                "for the onboarding pack"],
    "mech": ["memory", "shared state", "checkpointing", "message passing", "a blackboard", "context hand-off"],
    "ctype": ["blog post", "customer email", "landing page", "product description", "newsletter",
               "release announcement", "internal memo", "FAQ entry", "press release", "onboarding guide",
               "social post", "help-centre article", "case study", "pitch narrative"],
    "subject": ["the new billing dashboard", "our EMEA expansion", "the Q4 platform release",
                 "a pricing change", "the partner programme", "our security certification",
                 "the migration to the new API", "a customer success story", "the developer portal",
                 "our sustainability commitments", "the support SLA update", "a feature deprecation",
                 "the annual user conference", "our data residency options"],
    "tone": ["plain", "formal", "friendly", "technical", "concise", "persuasive", "neutral", "upbeat"],
    "rtype": ["design document", "pull request", "architecture proposal", "test plan", "runbook",
               "incident postmortem", "vendor contract", "marketing brief", "research summary",
               "migration plan", "security assessment", "data-handling policy"],
}

FRAMES = ["{}", "Please {}", "I need you to {}", "Can you {}", "Quick task: {}", "{} Thanks.",
          "For the team: {}", "{} Keep it tight."]
LOWER_FIRST = {"Please {}", "I need you to {}", "Can you {}"}

# ---------------------------------------------------------------- templates
# 6 per (task, complexity); the 6th is HELD OUT of training entirely.
CODE = {
 1: [("Write a {lang} one-liner that {simple_op}.", []),
     ("Add type hints to a {lang} {unit} that {simple_op}.", []),
     ("Fix the off-by-one error in a {lang} loop that {simple_op}.", []),
     ("Rename the variables in a short {lang} {unit} that {simple_op}.", []),
     ("Write a small {lang} {unit} that {simple_op}.", []),
     ("Explain line by line what a {lang} {unit} that {simple_op} does.", [])],
 2: [("Write a {lang} {unit} to {mid_op}.", []),
     ("Write unit tests for a {lang} {unit} that {mid_op}.", ["tests"]),
     ("Convert a {lang} script that {mid_op} into a module with CLI arguments.", []),
     ("Add {feature} to a {lang} {unit} that {mid_op}.", []),
     ("Document a {lang} {unit} that {mid_op}, with usage examples.", []),
     ("Wrap a {lang} {unit} that {mid_op} in a reusable {feature} decorator.", [])],
 3: [("Build a {lang} script to {mid_op} and generate a summary report.", []),
     ("Refactor a {lang} {service} to {mid_op} without changing its public API.", ["refactor"]),
     ("Implement {feature} in a {lang} {service}, with error handling and logging.", []),
     ("Add integration tests to a {lang} {service} that {mid_op}.", ["tests"]),
     ("Optimise a {lang} {service} that {mid_op} for throughput.", ["refactor"]),
     ("Instrument a {lang} {service} that {mid_op} with metrics and tracing.", [])],
 4: [("Debug an intermittent failure in a {lang} {service} that {mid_op} under load.", ["debug"]),
     ("Design and implement {feature} across a {lang} {service} and its {lang2} client.", ["multi_file", "design"]),
     ("Port a {lang} {service} that {mid_op} to {lang2}, preserving concurrency semantics.", ["multi_file"]),
     ("Introduce {feature} into a {lang} {service} with a zero-downtime migration.", ["design", "multi_step"]),
     ("Find and fix a memory leak in a {lang} {service} that {mid_op}.", ["debug"]),
     ("Harden a {lang} {service} that {mid_op} against {feature} failures, with tests.", ["debug", "tests"])],
 5: [("Diagnose and fix a production race condition in a distributed {lang} {service}, then prove correctness with tests.", ["debug", "multi_step", "design"]),
     ("Re-architect a monolithic {lang} {service} into services, covering {feature}, data migration and rollback.", ["design", "multi_step", "multi_file"]),
     ("Design a {lang} and {lang2} system implementing {feature} at scale, with consistency guarantees and a rollout plan.", ["design", "multi_file"]),
     ("Lead a rewrite of a {lang} {service} that {mid_op} into {lang2}, with phased cutover and correctness proofs.", ["multi_file", "multi_step"]),
     ("Root-cause a cascading outage across a {lang} {service} and its {lang2} dependencies, then implement {feature} to prevent recurrence.", ["debug", "multi_step"]),
     ("Design and implement a fault-tolerant {lang} {service} handling {feature}, including formal verification of the critical path.", ["design", "multi_step", "multi_file"])],
}
REASONING = {
 1: [("Which is cheaper to run day to day for {use_case}: {opt_a} or {opt_b}?", []),
     ("Give {audience} a quick pros and cons list for adopting {opt_a} in {use_case}.", []),
     ("Explain to {audience} in simple terms what {concept} means for {use_case}.", []),
     ("In one line, what is the main risk of {opt_a} for {use_case}?", []),
     ("For {use_case}, is {opt_a} or {opt_b} easier for {audience} to pick up?", []),
     ("Summarise the single biggest advantage of {opt_a} over {opt_b}.", [])],
 2: [("Rank {opt_a}, {opt_b} and {opt_c} for a small team on a tight budget.", []),
     ("Draft a short decision memo on whether to use {opt_a} for {use_case}.", []),
     ("Estimate the operational impact of moving {use_case} to {opt_a}.", []),
     ("List the questions we should ask before choosing {opt_a} for {use_case}.", []),
     ("Outline what a proof of concept for {concept} in {use_case} would involve.", []),
     ("Sketch the main trade-off between {opt_a} and {opt_b} for {use_case}.", [])],
 3: [("Compare {opt_a} and {opt_b} for {use_case} and recommend one, with trade-offs.", ["comparison"]),
     ("Work out a {horizon} roadmap for {use_case} {domain}, with milestones and dependencies.", ["multi_step", "planning"]),
     ("Analyse the failure modes of {concept} in {use_case} and propose mitigations.", ["analysis"]),
     ("Assess whether {concept} is worth adopting for {use_case}, with a cost sketch.", ["analysis"]),
     ("Evaluate {opt_a} against our constraints for {use_case} and justify a go or no-go.", ["analysis"]),
     ("Map the dependencies and sequencing for introducing {concept} into {use_case}.", ["multi_step", "planning"])],
 4: [("Design a {concept} for {use_case}, covering trade-offs, cost model and rollout.", ["design", "multi_step"]),
     ("Build a cost and benefit model comparing {opt_a}, {opt_b} and {opt_c} for {use_case} over {horizon}.", ["analysis", "math"]),
     ("Plan a multi-agent workflow for {use_case} {domain}, specifying tool calls, {mech} and hand-offs.", ["tools", "multi_step", "design"]),
     ("Produce a {horizon} migration strategy from {opt_a} to {opt_b} for {use_case}, with risk mitigation.", ["multi_step", "analysis"]),
     ("Model the capacity and cost implications of scaling {use_case} tenfold on {opt_a}.", ["math", "analysis"]),
     ("Specify an orchestration plan for {concept} in {use_case}, including tool selection and fallbacks.", ["tools", "multi_step"])],
 5: [("Compare {opt_a} and {opt_b} and recommend an enterprise strategy for {use_case}, with a risk register and financial model.", ["comparison", "analysis", "design"]),
     ("Design a {concept} for {use_case} at global scale: consistency guarantees, failure recovery, cost ceiling and a {horizon} migration plan.", ["design", "multi_step", "math"]),
     ("Resolve conflicting requirements between {opt_a} and {opt_b} for {use_case}, formally justify the decision and define the tool-orchestration plan.", ["tools", "multi_step", "analysis"]),
     ("Produce a board-ready {horizon} strategy for {use_case} spanning {opt_a} and {opt_b}, with financial modelling and regulatory analysis.", ["analysis", "math", "design"]),
     ("Architect {concept} across {use_case} under regulatory constraint, defining trade-offs, verification and a staged rollout with rollback criteria.", ["design", "multi_step", "analysis"]),
     ("Arbitrate between {opt_a}, {opt_b} and {opt_c} for {use_case} where the requirements conflict, and defend the decision with a quantified model.", ["comparison", "math", "analysis"])],
}
RAG = {
 1: [("Find the {field} in this {doc}.", ["short_ctx"]),
     ("Answer one question from a single {doc} {domain}: {question}.", ["short_ctx"]),
     ("Extract the {field} from {n_small} {doc}s into a list.", ["short_ctx"]),
     ("Look up the {field} in the attached {doc}.", ["short_ctx"]),
     ("From this {doc} {domain}, state {question} in one sentence.", ["short_ctx"]),
     ("Pull the {field} out of this {doc} and nothing else.", ["short_ctx"])],
 2: [("Summarise these {n_small} {doc}s {domain} in a paragraph.", ["short_ctx"]),
     ("Summarise these {n_mid} retrieved {doc}s {domain} and provide citations.", ["citations"]),
     ("Answer {question} using these {n_mid} {doc}s, quoting the source lines.", ["citations"]),
     ("List the {field} across {n_mid} {doc}s with a citation for each.", ["citations"]),
     ("Give a short briefing on {question} from {n_small} {doc}s.", ["short_ctx"]),
     ("Condense {n_mid} {doc}s into bullet points, citing each source.", ["citations"])],
 3: [("Reconcile the {field} across {n_mid} {doc}s and flag any disagreement.", ["conflict", "citations"]),
     ("Synthesise {n_large} {doc}s {domain} into a briefing with per-claim citations.", ["citations", "long_ctx"]),
     ("Answer {question} from {n_large} {doc}s where the relevant passage appears in only two of them.", ["needle", "long_ctx"]),
     ("Cross-reference {n_large} {doc}s to establish {question}, citing sources.", ["long_ctx", "citations"]),
     ("Identify which of {n_mid} {doc}s actually address {question}, and summarise those.", ["citations"]),
     ("Merge {n_large} {doc}s into one account of {question}, noting gaps.", ["long_ctx", "citations"])],
 4: [("Build a comparison table of the {field} across {n_large} {doc}s, citing every cell.", ["long_ctx", "citations", "analysis"]),
     ("Audit {n_large} {doc}s for contradictions on {question}, cite each conflict and state which source is authoritative.", ["conflict", "long_ctx", "analysis"]),
     ("Trace how the {field} changed across {n_large} versioned {doc}s and explain the drivers, with citations.", ["long_ctx", "multi_step"]),
     ("Analyse {n_large} {doc}s to determine {question}, weighing source reliability.", ["long_ctx", "analysis"]),
     ("Reconstruct the timeline of {question} from {n_large} {doc}s, citing each step.", ["long_ctx", "multi_step"]),
     ("Assess the evidence for {question} across {n_large} {doc}s and rate confidence per claim.", ["long_ctx", "analysis"])],
 5: [("Produce a defensible answer to {question} from {n_xl} {doc}s with conflicting evidence, a full citation chain and a confidence statement.", ["conflict", "long_ctx", "citations", "analysis"]),
     ("Synthesise {n_xl} {doc}s into a regulator-ready analysis of {question}, resolving contradictions and citing every claim.", ["long_ctx", "citations", "conflict", "analysis"]),
     ("Adjudicate {question} across {n_xl} contradictory {doc}s, establish authority order and justify every inclusion.", ["conflict", "long_ctx", "analysis"]),
     ("Build a litigation-grade evidence summary on {question} from {n_xl} {doc}s, with per-claim citation and contradiction analysis.", ["conflict", "long_ctx", "citations", "analysis"]),
     ("Derive the authoritative {field} from {n_xl} conflicting {doc}s, documenting the reasoning chain and residual uncertainty.", ["conflict", "long_ctx", "analysis"]),
     ("Compile an audit-ready determination of {question} from {n_xl} {doc}s, reconciling every disagreement with citations.", ["conflict", "long_ctx", "citations", "analysis"])],
}
CONTENT = {
 1: [("Write a one-line {ctype} for {subject}.", []),
     ("Give me three subject lines for a {ctype} about {subject}.", []),
     ("Shorten this {ctype} about {subject} to a single sentence.", []),
     ("Write a {tone} one-paragraph {ctype} for {subject}.", []),
     ("Draft a short {ctype} announcing {subject}.", []),
     ("Turn a bullet list about {subject} into one {tone} sentence.", [])],
 2: [("Write a {tone} {ctype} for {subject}, about 200 words.", []),
     ("Draft a {ctype} for {audience} explaining {subject}.", []),
     ("Rewrite a {ctype} about {subject} in a more {tone} voice.", ["rewrite"]),
     ("Produce a {ctype} for {subject} with a clear call to action.", []),
     ("Write {tone} release notes for {subject} aimed at {audience}.", []),
     ("Adapt a {ctype} about {subject} for {audience}.", ["rewrite"])],
 3: [("Write a {ctype} on {subject} for {audience}, with a structured argument.", ["structure"]),
     ("Produce a {horizon} content plan for {subject} aimed at {audience}, with themes and formats.", ["planning"]),
     ("Draft {tone} documentation for {subject} for {audience}, covering setup, usage and troubleshooting.", ["structure", "long_form"]),
     ("Write a {tone} {ctype} for {subject} that handles the main objections.", ["structure"]),
     ("Create a {ctype} series outline on {subject} for {audience}.", ["planning"]),
     ("Write a case study on {subject} for {audience}, with evidence and outcomes.", ["structure"])],
 4: [("Write a long-form {ctype} on {subject} for {audience}, with research-backed claims and citations.", ["long_form", "citations"]),
     ("Produce a full launch narrative for {subject}: positioning, {ctype}, and {audience} messaging.", ["long_form", "planning"]),
     ("Write technical documentation for {subject} in {lang} for {audience}, spanning architecture, API reference and migration.", ["long_form", "structure"]),
     ("Develop a {tone} thought-leadership {ctype} on {subject}, differentiated from competitors.", ["long_form"]),
     ("Write a {tone} whitepaper section on {subject} for {audience}, balancing depth and accessibility.", ["long_form", "citations"]),
     ("Produce a {horizon} editorial strategy for {subject} with measurement criteria.", ["planning", "long_form"])],
 5: [("Write a board-level {ctype} on {subject} for {audience}, with financial framing, risk language and regulatory care.", ["long_form", "citations", "high_stakes"]),
     ("Produce a crisis communications pack for {subject}: holding statement, {audience} briefing and press {ctype}.", ["high_stakes", "long_form"]),
     ("Write a regulator-facing {ctype} on {subject}, where every claim must be defensible and precisely worded.", ["high_stakes", "citations"]),
     ("Develop the full {tone} brand narrative for {subject} across {audience} and press, with tone rules and rationale.", ["long_form", "high_stakes"]),
     ("Write an investor-facing {ctype} on {subject} with forward-looking statements handled carefully.", ["high_stakes", "citations"]),
     ("Produce a legally reviewed {ctype} on {subject} for {audience}, reconciling marketing and compliance language.", ["high_stakes", "long_form"])],
}
BANK = {"code": CODE, "reasoning": REASONING, "rag": RAG, "content": CONTENT}

# ---------------------------------------------------------------- label policy (unchanged from v1)
PREF_BASE = {"cheap": 0.0, "medium": 1.6, "high": 3.0, "extra_high": 4.4}


# deepseek-v4-flash is no longer a code-only specialist. It is the craft/production
# workhorse across code, content generation and review -- strong enough to cover the
# mid tier in all three, so it displaces both gpt-5-mini and (at low-to-mid
# complexity) gpt-5.5 in those domains.
DEEPSEEK_DOMAINS = {"code", "content"}


def label(task_type, pref, complexity, tags):
    score = PREF_BASE[pref] + (complexity - 3) * 0.55
    notes = []
    if task_type == "code" and any(t in tags for t in ("multi_file", "refactor", "tests")):
        score += 0.35; notes.append("code-heavy context")
    if task_type == "content" and "long_form" in tags:
        score += 0.35; notes.append("long-form drafting")
    if task_type == "content" and "high_stakes" in tags:
        score += 0.55; notes.append("high-stakes copy, wording must be defensible")
    if task_type == "reasoning" and "multi_step" in tags:
        score += 0.4; notes.append("multi-step planning")
    if task_type == "rag" and "long_ctx" in tags:
        score += 0.35; notes.append("long retrieved context")
    if task_type == "rag" and "conflict" in tags:
        score += 0.3; notes.append("conflicting sources need adjudication")
    if pref == "cheap":
        score = min(score, 2.0)
    if pref == "extra_high":
        score = max(score, 3.0)
    idx = max(0, min(5, int(round(score))))
    model, rule = MODELS[idx], "base"
    # --- broadened deepseek: covers code, content and review at the mid tiers ---
    if task_type in DEEPSEEK_DOMAINS and model == "gpt-5-mini" and complexity <= 4:
        model, rule = "deepseek-v4-flash", "deepseek-domain-specialist"
        notes.append(f"deepseek is tuned for {task_type} at this tier")
    # Not under extra_high: its floor is gpt-5.5, and this override would push the
    # label below it (v3.1 shipped 764 training rows labelled with a forbidden model).
    if (task_type in DEEPSEEK_DOMAINS and model == "gpt-5.5" and complexity <= 3 and pref != "extra_high"
            and not any(t in tags for t in ("security", "compliance", "high_stakes"))):
        model, rule = "deepseek-v4-flash", "deepseek-covers-midtier"
        notes.append(f"deepseek handles mid-complexity {task_type} at lower cost")
    if task_type == "code" and model == "gpt-5.5" and complexity >= 4 and "debug" in tags:
        model, rule = "o4-mini", "code-debug-escalation"; notes.append("deep debugging benefits from reasoning model")
    if task_type == "reasoning" and model == "gpt-5.5" and ("tools" in tags or "multi_step" in tags):
        model, rule = "o4-mini", "reasoning-tool-orchestration"; notes.append("tool orchestration required")
    if task_type == "rag" and model == "gpt-5-nano" and ("citations" in tags or complexity >= 2):
        model, rule = "gpt-5-mini", "rag-citation-floor"; notes.append("citation fidelity needs more than nano")
    if task_type == "rag" and model == "o4-mini" and "analysis" not in tags:
        model, rule = "gpt-5.5", "rag-no-deep-reasoning"; notes.append("retrieval-bound, not reasoning-bound")
    reason = f"{pref} budget, complexity {complexity}/5" + ("; " + "; ".join(notes) if notes else "")
    return model, rule, reason


# ---------------------------------------------------------------- rendering
def fill(tpl):
    out = tpl
    for k, vals in SLOTS.items():
        tok = "{" + k + "}"
        while tok in out:
            out = out.replace(tok, random.choice(vals), 1)
    return out


def render(tpl):
    body = fill(tpl)
    fr = random.choice(FRAMES)
    if fr in LOWER_FIRST and body:
        body = body[0].lower() + body[1:]
        if body.endswith("."):
            body = body[:-1] + "?"
    return fr.format(body)


def capacity(tpl):
    c = len(FRAMES)
    for k, v in SLOTS.items():
        c *= len(v) ** tpl.count("{" + k + "}")
    return c


def preflight():
    """Every template must have >=3x the unique fills it will be asked for."""
    bad = []
    for task, d in BANK.items():
        for cx, tpls in d.items():
            for i, (t, _) in enumerate(tpls):
                need = (OOD_PER_CELL // 5) * len(PREFS) if i == 5 else (PER_CX // 5) * len(PREFS)
                if capacity(t) < need * 3:
                    bad.append(f"{task} c{cx} t{i} cap={capacity(t)} need={need}: {t[:60]}")
    if bad:
        raise SystemExit("PREFLIGHT FAILED — thin templates:\n  " + "\n  ".join(bad))
    print("preflight OK — all 90 templates have >=3x required variation capacity")


preflight()
seen = set()


def make_unique(tpl, budget=4000):
    for _ in range(budget):
        p = render(tpl)
        if p not in seen:
            seen.add(p)
            return p
    raise RuntimeError(f"capacity exhausted for template: {tpl[:60]}")


# ---------------------------------------------------------------- generation
def build(cell_n, template_slice, split_tag):
    rows = []
    for task in TASKS:
        for pref in PREFS:
            per_cx = cell_n // 5
            for cx in range(1, 6):
                tpls = template_slice(BANK[task][cx])
                for i in range(per_cx):
                    tpl, tags = tpls[i % len(tpls)]      # balanced template usage
                    prompt = make_unique(tpl)
                    model, rule, reason = label(task, pref, cx, tags)
                    rows.append({"prompt": prompt, "task_type": task, "quality_preference": pref,
                                 "selected_model": model, "complexity": cx, "features": tags,
                                 "policy_rule": rule, "reason": reason, "template_id": f"{task}-c{cx}-t{tpls.index((tpl, tags))}",
                                 "source": split_tag})
    random.shuffle(rows)
    for n, r in enumerate(rows, 1):
        r["id"] = f"{split_tag}-{n:05d}"
    return rows


main = build(PER_CELL, lambda t: t[:5], "rt")          # 5 templates per group
ood = build(OOD_PER_CELL, lambda t: t[5:], "ood")      # held-out 6th template

# stratified split by (task, pref, complexity): 85 / 5 / 10
strata = defaultdict(list)
for r in main:
    strata[(r["task_type"], r["quality_preference"], r["complexity"])].append(r)
train, val, test = [], [], []
for g in strata.values():
    random.shuffle(g)
    n = len(g); n_tr = round(n * 0.85); n_va = round(n * 0.05)
    train += g[:n_tr]; val += g[n_tr:n_tr + n_va]; test += g[n_tr + n_va:]
for s in (train, val, test, ood):
    random.shuffle(s)

OUT = os.environ.get("DATASET_OUT", os.path.dirname(os.path.abspath(__file__)))
os.makedirs(OUT, exist_ok=True)


def w(path, data):
    with open(f"{OUT}/{path}", "w", encoding="utf-8") as f:
        for r in data:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")


w("routing_v31_full.jsonl", main)
w("routing_v31_train.jsonl", train)
w("routing_v31_val.jsonl", val)
w("routing_v31_test.jsonl", test)
w("routing_v31_test_ood_templates.jsonl", ood)

# ---------------------------------------------------------------- balance audit
def audit(rows, name):
    cells = Counter((r["task_type"], r["quality_preference"]) for r in rows)
    cx_by_cell = {f"{t}/{q}": dict(sorted(Counter(r["complexity"] for r in rows
                  if r["task_type"] == t and r["quality_preference"] == q).items()))
                  for t in TASKS for q in PREFS}
    return {"name": name, "n": len(rows),
            "cell_counts_min_max": [min(cells.values()), max(cells.values())],
            "by_task": dict(Counter(r["task_type"] for r in rows)),
            "by_preference": dict(Counter(r["quality_preference"] for r in rows)),
            "by_complexity": dict(sorted(Counter(r["complexity"] for r in rows).items())),
            "by_model": dict(Counter(r["selected_model"] for r in rows)),
            "complexity_uniform_within_cells": all(len(set(v.values())) == 1 for v in cx_by_cell.values())}


stats = {"total_generated": len(main) + len(ood), "unique_prompts": len(seen),
         "duplicates": (len(main) + len(ood)) - len(seen),
         "splits": {"train": len(train), "val": len(val), "test": len(test), "ood_test": len(ood)},
         "audits": [audit(main, "full"), audit(train, "train"), audit(val, "val"),
                     audit(test, "test"), audit(ood, "ood_test")],
         "model_by_preference": {p: dict(Counter(r["selected_model"] for r in main if r["quality_preference"] == p)) for p in PREFS},
         "model_by_task": {t: dict(Counter(r["selected_model"] for r in main if r["task_type"] == t)) for t in TASKS},
         "train_test_prompt_overlap": len(set(r["prompt"] for r in train) & set(r["prompt"] for r in test)),
         "train_ood_prompt_overlap": len(set(r["prompt"] for r in train) & set(r["prompt"] for r in ood))}
with open(f"{OUT}/v31_stats.json", "w") as f:
    json.dump(stats, f, indent=2)

review = random.sample(main, 300)
with open(f"{OUT}/review_sample_300.csv", "w", newline="", encoding="utf-8") as f:
    wr = csv.writer(f)
    wr.writerow(["id", "prompt", "task_type", "quality_preference", "selected_model", "complexity",
                  "reason", "agree_y_n", "corrected_model", "notes"])
    for r in review:
        wr.writerow([r["id"], r["prompt"], r["task_type"], r["quality_preference"],
                      r["selected_model"], r["complexity"], r["reason"], "", "", ""])

print(json.dumps(stats, indent=2))
