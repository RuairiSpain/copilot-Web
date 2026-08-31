# tools/

Repo-wide, non-Azure, read-only tooling. Nothing here deploys or calls
anything — safe to run anywhere with just Python 3.

## `validate_policy_xml.py`

An APIM-dialect-aware structural validator for every policy fragment and
product/API policy XML file in this repo — not just this fork's own
additions. See its own module docstring for the full "why" and "how";
short version: it masks C# `@(...)`/`@{...}` expression blocks before
strict-XML-parsing what's left, so it catches genuine structural bugs
(unclosed tags, mismatched elements) without false-positiving on this
repo's own accepted authoring convention (C# generics and nested quotes
inside XML attribute values, which a naive strict parser rejects but
APIM itself accepts).

```bash
python3 tools/validate_policy_xml.py          # validates the whole repo
python3 tools/validate_policy_xml.py some/dir  # or just a subdirectory
```

Exit code 0 = every file structurally sound; 1 = at least one real
problem, printed with file and line number. No dependencies beyond the
Python 3 standard library.

**Verified in this session** (see
`guides/enterprise-hardening-checklist.md` §8 for the full account):
passes clean against all 76 policy XML files that exist in this repo
today, and correctly catches a deliberately broken test fixture with the
right line number.

**Recommended use**: wire this into whatever CI pipeline runs
`az deployment` for this accelerator, as a gate before any policy change
ships — this repo had no automated check for policy XML well-formedness
before this tool existed, for any file, old or new.
