---
name: code-writer
description: Generate boilerplate that follows an existing pattern — tests, config scaffolding, type stubs — without spending output tokens on it. Use when the output is predictable from a reference file.
---

# code-writer

Generates a file from a spec plus a reference file, using a cheap worker model.
With `--target` the code is written straight to disk and never enters this
session's context.

## Usage

```bash
node .github/hooks/shunt/code-write.mjs \
  --spec "Vitest suite covering GitHubFsProvider.flush() batching and the reserved-prefix skip" \
  --reference tests/event-log.test.ts \
  --target tests/github-fs-flush.test.ts
```

- `--spec` — what to generate. Be specific about behaviour to cover.
- `--reference` — **required.** One or more existing files whose patterns,
  naming, and style the output must match. Without it the worker produces
  code that fits nothing in this project.
- `--target` — where to write. Omit to print to stdout instead.

After it writes, **review the file** — read it with a targeted read, or run the
relevant checks (`npm run typecheck`, `npm test`). The worker is not trusted;
it is cheap.

## When NOT to use this

- **The code needs real reasoning** — concurrency, security, anything where
  being subtly wrong matters. Write it yourself.
- **There is no reference to match.** Find one first, or write it yourself.
- **The change is an edit to an existing file.** This generates whole files;
  it does not patch.
