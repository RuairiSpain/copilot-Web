# dev-team

A lead agent interviews you until the project is unambiguous. Then a Magentic team builds it:

* **CodeProjectPlanner** writes requirements, a task list and guides.
* **CodeWriter** writes typed Pydantic Python.
* **UnitTester** writes unit tests and runs them in the agent host.

Every agent can use an allow-listed command line and local git. Each works in its own git
worktree. See [`docs/architecture.md`](../../docs/architecture.md) for the diagrams.

## The interview

The lead must learn six things: target audience, functional requirements, dependencies
(including other services), input and output samples, what is out of scope, and the
definition of done. `intake/gate.py` checks the draft brief. It rejects missing fields, vague
words such as "etc." or "as needed", duplicate or untestable requirements, undeclared
dependencies, and a definition of done with no test condition. `start_build` runs the gate
again, so the interview cannot be skipped.

## Files

| Path | Purpose |
| --- | --- |
| `main.py` | Entry point (Responses protocol). |
| `lead.py` | The lead agent and the two gate tools: `validate_brief`, `start_build`. |
| `team.py` | Magentic workflow, participants and the `DevTeam` build runner. |
| `intake/` | `ProjectBrief` model and the gate. |
| `tools/shell.py` | Allow-listed command runner. No shell, no network git, scrubbed environment. |
| `tools/files.py` | Path confinement and per-role write ownership. |
| `tools/worktrees.py` | Git worktrees, publish and refresh, conflict-safe merges. |
| `tools/agent_tools.py` | The tools each role receives. |
| `prompts/` | One prompt per role. |

## Limits worth knowing

* One project per session. Start a new session for a new project.
* A build is a long call. Send the Responses request with `background: true` for big projects.
* Lint tools may not exist in the runtime image. The writer is told to check syntax with
  `python -m compileall`. Only `pytest` is guaranteed.
* Whether the hosted sandbox keeps files after the session ends was not verified. Treat the
  workspace as disposable and copy results out.

```bash
uv sync && uv run pytest     # runs real git and pytest in temporary folders
```
