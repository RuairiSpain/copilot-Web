"""Renderer for the `activity` array that agentic retrieval returns.

Shared by `3_search.py` (which prints it inline) and `4_trace.py` (which
replays, watches, or races it live). Kept separate so the two commands can
never drift into showing different things.

Activity record types, per the retrieve API:
  modelQueryPlanning    the planner decomposing the question into subqueries
  searchIndex           one subquery against one knowledge source
  modelAnswerSynthesis  the synthesis pass over the retrieved documents
  agenticReasoning      resolved reasoning effort and reasoning tokens
"""

from __future__ import annotations

from typing import Any

GLYPHS = {
    "modelQueryPlanning": ("\033[35m", "plan"),
    "searchIndex": ("\033[36m", "search"),
    "modelAnswerSynthesis": ("\033[33m", "synth"),
    "agenticReasoning": ("\033[34m", "reason"),
}
RESET = "\033[0m"
DIM = "\033[2m"


def _label(record: dict[str, Any]) -> tuple[str, str]:
    return GLYPHS.get(str(record.get("type")), ("\033[37m", str(record.get("type", "?"))[:6]))


def format_record(record: dict[str, Any], *, indent: str = "  ") -> list[str]:
    colour, name = _label(record)
    elapsed = record.get("elapsedMs")
    elapsed_text = f"{elapsed:>6} ms" if isinstance(elapsed, int) else " " * 9
    detail = ""

    if record.get("type") == "searchIndex":
        detail = f"{record.get('knowledgeSourceName', '?')}  docs={record.get('count', 0)}"
    elif "inputTokens" in record or "outputTokens" in record:
        model = (record.get("model") or {}).get("modelName", "")
        detail = f"in={record.get('inputTokens', 0):,} out={record.get('outputTokens', 0):,}  {model}"
    elif record.get("type") == "agenticReasoning":
        effort = (record.get("retrievalReasoningEffort") or {}).get("kind", "?")
        detail = f"effort={effort}  reasoningTokens={record.get('reasoningTokens', 0):,}"

    lines = [f"{indent}{colour}▸ {name:<7}{RESET}{elapsed_text}  {detail}"]

    arguments = record.get("searchIndexArguments") or {}
    if arguments.get("search"):
        lines.append(f'{indent}    {DIM}search:{RESET} "{arguments["search"]}"')
    if arguments.get("filter"):
        # The filter is the interesting half: it shows metadata narrowing the
        # candidate set before vectors are compared.
        lines.append(f'{indent}    {DIM}filter:{RESET} {arguments["filter"]}')
    if arguments.get("semanticConfigurationName"):
        lines.append(f'{indent}    {DIM}semantic:{RESET} {arguments["semanticConfigurationName"]}')
    if arguments.get("searchFields"):
        lines.append(f'{indent}    {DIM}fields:{RESET} {arguments["searchFields"]}')
    return lines


def render_activity(activity: list[dict[str, Any]], *, wall_ms: int | None = None, title: str = "pipeline") -> None:
    if not activity:
        print(f"\n{DIM}(no activity returned — pass includeActivity in the request){RESET}")
        return
    print(f"\n\033[1m{title}\033[0m")
    for record in activity:
        for line in format_record(record):
            print(line)


def totals(activity: list[dict[str, Any]]) -> dict[str, int]:
    result = {"in": 0, "out": 0, "reasoning": 0, "docs": 0, "subqueries": 0, "elapsed": 0}
    for record in activity:
        result["in"] += int(record.get("inputTokens") or 0)
        result["out"] += int(record.get("outputTokens") or 0)
        result["reasoning"] += int(record.get("reasoningTokens") or 0)
        result["elapsed"] += int(record.get("elapsedMs") or 0)
        if record.get("type") == "searchIndex":
            result["subqueries"] += 1
            result["docs"] += int(record.get("count") or 0)
    return result


def summarize(activity: list[dict[str, Any]], wall_ms: int | None = None) -> str:
    counts = totals(activity)
    parts = [
        f"{counts['subqueries']} subqueries",
        f"{counts['docs']} documents",
        f"{counts['in']:,} in / {counts['out']:,} out tokens",
    ]
    if counts["reasoning"]:
        parts.append(f"{counts['reasoning']:,} reasoning tokens")
    if wall_ms is not None:
        parts.append(f"{wall_ms:,} ms wall")
    return f"{DIM}" + "  |  ".join(parts) + RESET
