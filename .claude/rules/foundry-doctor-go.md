---
paths:
  - "foundry-doctor/**/*.go"
  - "foundry-doctor/go.mod"
---

# Go implementation rules

- Use the Go version declared in `go.mod`.
- `context.Context` is the first parameter of every I/O-bound function. Do not store contexts in structs.
- Wrap errors with operation and resource context using `%w`. Test with `errors.Is` / `errors.As`.
- Prefer table-driven tests.
- No package-level mutable state. Package globals are for immutable metadata only.
- Define interfaces where they are consumed. Accept interfaces, return concrete types.
- Cobra handlers parse flags and call `internal/app`; they contain no business logic.
- Bound concurrency with `errgroup` or an explicit semaphore. Honour cancellation and deadlines.
- Output must be deterministic: sort map iteration, avoid wall-clock time and absolute paths in reports.
- Redact secrets before logging or building findings.
- A new dependency needs an entry in `docs/licence-inventory.md`: purpose, licence,
  maintenance status, alternatives, security implications.
- Run `gofmt`, `go vet`, `go test -race ./...`, `staticcheck` and `govulncheck`.
