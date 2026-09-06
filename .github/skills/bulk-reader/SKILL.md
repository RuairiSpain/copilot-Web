---
name: bulk-reader
description: Answer questions that span large or multiple files without reading them into context. Use when a read was blocked for being over the line threshold, or before opening several large files just to answer one question.
---

# bulk-reader

Large file reads are delegated to a cheap worker model so their contents never
enter this session's context. If a `read` or `cat` was blocked, this is what to
do instead.

## Usage

```bash
node .github/hooks/shunt/bulk-read.mjs \
  --question "What does this service do and which methods touch the database?" \
  --paths src/server/session-manager.ts src/server/github-fs.ts
```

- `--question` — one specific question. The worker answers only what is asked.
- `--paths` — one or more files. Pass every file the question spans in a single
  call; the worker sees them together and can answer across them.

The answer comes back as structured bullets on stdout. Token usage for the
delegation is reported on stderr.

Follow-up questions over the same files are a fresh call with the same
`--paths`. Re-sending is cheap: the files go to the worker, not into this
context.

## When NOT to use this

- **You are about to edit the code.** The worker's summary has no reliable line
  numbers. Read the specific section directly with an explicit `offset`/`limit`
  — targeted reads are never blocked.
- **You are debugging, making an architectural decision, or working on
  safety-critical code.** The worker finds surface patterns and misses subtle
  bugs. Read it yourself and reason about it.
- **The file is small.** Delegation costs a 10–30 second round trip. Below the
  threshold the overhead exceeds the saving, which is why small reads pass
  straight through.
