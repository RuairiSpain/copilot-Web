# Agent Trace Instrumentation Checklist

**This is a checklist for developers, not a report.** Workflow/agent
bottleneck visibility ("which step in my agent's multi-call workflow was
slow") depends on something the gateway cannot provide on its own —
distributed trace correlation across the calls that make up one logical
agent run. This document explains what's needed and points at the tool
that already exists for it, instead of shipping a Power BI report that
would only ever be an approximation.

## Why this can't just be a Power BI report

The gateway sees every request independently. Two calls from the same
agent run, thirty seconds apart, look identical to two calls from two
unrelated users — **unless the calling application tells the gateway they
belong together**, via a W3C `traceparent` header carrying a shared trace
ID. The accelerator's Application Insights integration already has
everything needed to use that correlation
(`guides/platform-observability-guide.md`, capability 2:
`httpCorrelationProtocol: 'W3C'` is already enabled on every inference
API) — **the missing piece is entirely on the calling application's
side**, not the gateway's.

## Step 1 — check whether your agent already does this

Before instrumenting anything: open **Application Insights → Application
Map** (or **Transaction Search**) for the hub's App Insights resource and
look at one of your agent's real requests. If you see a single connected
graph spanning multiple calls (your agent → gateway → backend, repeated
for each internal LLM call, all under one operation), **you already have
this** — the report you want is the Application Map / End-to-end
transaction details view, not something to build in Power BI. Use it
directly; there is no reason to duplicate it.

If instead every call shows up as its own disconnected node with no
parent-child relationship, your agent isn't propagating trace context yet
— follow the checklist below.

## Step 2 — instrument the agent to propagate trace context

The general requirement, regardless of language or framework: every
outbound call your agent makes to the gateway must carry a `traceparent`
header (and `tracestate` if you use vendor-specific trace state) linking
it back to the parent operation for that agent run.

- **If your agent is already built on an OpenTelemetry SDK** (most modern
  Python/.NET/Node/Java HTTP client libraries support OTel
  auto-instrumentation) — enabling the standard HTTP client
  instrumentation for whatever library issues the gateway calls is
  usually enough; the SDK injects `traceparent` automatically on every
  outbound request within a traced operation. Point the OTel exporter at
  the same Application Insights connection string / OTLP endpoint the
  gateway already uses so traces land in one place.
- **If your agent framework has built-in callback/tracing hooks** (many
  agent orchestration frameworks expose a callback or middleware layer
  for logging each step) — wire that layer to open an OTel span per step
  and let the SDK's HTTP instrumentation propagate context downstream,
  rather than hand-rolling header injection.
- **If neither applies** (a simple script making direct HTTP calls) —
  manually generate/propagate the `traceparent` header per the [W3C Trace
  Context spec](https://www.w3.org/TR/trace-context/): one root trace ID
  per agent run, a fresh span ID per outbound call, forwarded on every
  request that's logically part of the same run.

This document doesn't prescribe exact code for a specific framework —
verify against your framework's actual OpenTelemetry/tracing
documentation rather than treating any specific snippet here as
authoritative, since agent framework tracing APIs change quickly and
vary by language.

## Step 3 — once instrumented, use the Application Map (not Power BI)

With trace context flowing, Application Insights already gives you, per
agent run: total wall-clock time, which individual call in the sequence
took the longest, parallel vs. sequential call structure, and failures
attributed to the specific step that caused them — all without any new
Power BI development. This is a strictly better tool for this question
than an aggregate dashboard could ever be, because it shows the actual
causal structure of one run, not a statistical summary across many.

## If you can't instrument right now: a weaker proxy

The `powerbi-report-kit` deliverable does **not** include a "workflow
bottleneck" report, deliberately — building one against
`llm-usage-container` (which has no trace/span ID) would only ever
approximate a workflow as "this app's requests within a short time
window," indistinguishable from a user quickly asking several unrelated
things. That's a real approximation with a real false-positive rate, not
a substitute for actual tracing. If you want it anyway as a stopgap while
Step 2 is in progress, it's a small addition to the kit (group by `appId`
+ a time-gap threshold, similar to file 12's rapid-fire detection) — ask
and it can be added, clearly labeled as an approximation on the visual
itself so nobody mistakes it for real trace data.
