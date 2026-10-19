# Annotate

`foundry-doctor annotate` turns findings into source-linked review artefacts.
It never edits deployable originals.

## Modes

- `--format review` copies `azure.yaml` and the resolved infra directory (see
  below) into a repo-relative output directory whose name must end in
  `.review`, inserts tool-owned comments, writes `annotations-manifest.json`,
  and optionally writes `annotations.diff` with `--diff`.
- `--format github` writes GitHub Actions workflow-command annotations to
  stdout or `--out`.
- `--format sarif` writes SARIF 2.1.0 annotations to stdout or `--out`.

## Examples

```text
foundry-doctor annotate --profile test --out review\sample.review
foundry-doctor annotate --profile prod --out review\sample.review --diff
foundry-doctor annotate --format github --min-severity warning
foundry-doctor annotate --format sarif --out annotate.sarif
```

## Which infra directory is copied and compiled

One resolver decides the infra directory and the Bicep entry for the review
copy, the annotate compiles and the Bicep/ARM loader (ADR-024):

1. `inputs.infraPath` in `.foundry-doctor/config.yaml`, when it is not the
   default `infra`;
2. otherwise `infra.path` in `azure.yaml`;
3. otherwise `infra`.

Values from `azure.yaml` and the default are relative to the directory that
contains `azure.yaml`, which azd treats as the project directory. The entry is
`infra.module` (default `main`; `.bicep` is added when there is no extension)
inside that directory. A project whose `infra.provider` is not `bicep` (for
example `terraform`) is copied but never compiled with Bicep. An infra path of
`.` (the project root) cannot be used with `--format review`. Source:
<https://learn.microsoft.com/azure/developer/azure-developer-cli/azd-schema>
(infra section), verified 2026-10-18.

## The review copy

- Every copied `azure.yaml` is written as `azure.review.yaml` (and `azure.yml`
  as `azure.review.yml`), so azd, which looks for a file named `azure.yaml`,
  does not treat the review directory as a project. `annotations-manifest.json`
  `files[]` maps each original `path` to its `reviewPath`.
- Every copied `azure.yaml` and every copied `.bicep` file starts with a
  "Do not deploy this file" banner, even when it has no annotations. A UTF-8
  byte order mark stays first.
- The review root holds a `.foundry-doctor-review` marker. A rerun replaces an
  existing `--out` directory only when it is empty, holds that marker, or holds
  a manifest written by Foundry Doctor. Any other existing directory is left
  untouched and the run exits 2.
- Copies are never more permissive than their source: group and other bits are
  kept, and only owner read/write is added. The review root is created `0750`,
  the manifest `0640` and the diff `0600`.
- Comments are inserted above the target line. A Bicep comment for a line that
  follows `#disable-next-line` directives goes above the directives.
- After writing, the copy is checked: every bannered or annotated YAML file must
  decode to the same documents, and the Bicep entry in the copy must give the
  same compile status, the same compiled ARM (the generator metadata
  `metadata._generator` aside, ADR-024) and the same diagnostics. Both the
  original and the copy pass the ADR-018 file-load guard before they are
  compiled. Any failure removes the copy and fails the run.
- Line endings of the originals are kept (a CRLF file stays CRLF), because
  rewriting them changes the value of a multi-line Bicep string. `annotations.diff`
  is a full-file replacement per annotated file, with CRLF shown as LF. It contains the full source of those files; handle it like
  source.
- The infra copy leaves out credential-like files (`.env*`, `*.pem`, `*.pfx`,
  `*.p12`, `*.key`, `*.jks`, `*.keystore`, `*.tfstate*`) and files over 8 MiB, and
  stops at 256 MiB in total. Each skipped file is named in a warning. A module
  that loads a skipped file fails the recompile of the copy and the run stops.
- The review directory contains its own `.gitignore` (`*`). A YAML file that does
  not decode (for example a duplicate key) gets the banner only; its findings stay
  in the manifest with the reason `source-not-parseable`.
- Manifest `line` values refer to the original files; the copy is shifted by the
  banner and the inserted comment lines.
- Add `*.review/` to `.gitignore`, `.dockerignore` and `.funcignore` so a
  review copy is never committed or packaged with a service.

## Output safety

- `--out` must stay inside the project directory. An absolute `--out` inside
  `--dir` is accepted and made relative.
- `--out` must not overlap `azure.yaml` or the infra directory. The check is by
  file identity as well as by name, so a case variant (Windows, macOS), a hard
  link or a symbolic link cannot alias an input. An output path through a
  symbolic link or a Windows junction is refused, an existing github or sarif
  `--out` that is a symbolic link or not a regular file is refused, and a new
  file is created exclusively.
- Inputs that are symbolic links or reparse points (a linked `azure.yaml`, a
  linked infra directory, or a link inside it) are refused.

## Text and locations

- Annotation text is redacted, bounded to 240 bytes, kept on one line, and
  stripped of format characters (including bidirectional overrides) and line
  or paragraph separators. The manifest, SARIF and review comments carry this
  plain text.
- GitHub workflow-command escaping is applied once, only in `--format github`:
  `%`, CR and LF in the message; also `:` and `,` in properties.
- Findings without precise source locations stay in `annotations-manifest.json`
  and are not inlined. In SARIF they carry no line or column.
- Bicep inline comments are limited to exact compiler diagnostics. ARM-derived
  Bicep findings remain manifest-only until source mapping is proven. When a
  compiler diagnostic is also reported as a `bicep/<code>` adapter finding, only
  the diagnostic entry is kept.

## Limitations

- Review-mode Bicep validation requires a single Bicep entry module; projects
  using `infra.layers` return unavailable. A missing Bicep CLI also makes
  review mode unavailable for a Bicep project.
- GitHub `file=` values and SARIF URIs are relative to `--dir`, not to the
  repository root. Run with `--dir` at the repository root, or annotations may
  not attach. GitHub limits the number of annotations shown per step and job;
  the tool does not cap or prioritise them.
- One comment that changes the meaning of the copy (for example inside a YAML
  block scalar or a Bicep multi-line string) fails the whole run; there is no
  per-annotation fallback yet.
- A `bicepconfig.json` between the project root and a nested infra directory is
  not copied, so the copy may compile differently and the run fails.
- There is no size or file-count cap on the infra copy, and no large-repository
  performance test yet (ADR-024).
