"""memory-bot: a stateful Responses-protocol hosted agent.

A stateful agent keeps data inside its session sandbox. This one writes what the user tells it to
a local file, which Foundry keeps for the life of the session. The scheduler sends the same user
(and conversation) back to the same session, so the memory is found again.
"""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any

from fastapi import FastAPI

app = FastAPI(title="memory-bot")
# Point MEMORY_PATH at the session's persistent storage in a real agent.
MEMORY = Path(os.environ.get("MEMORY_PATH", "memory.txt"))


@app.get("/readiness")
async def readiness() -> dict[str, str]:
    return {"status": "ready"}


@app.post("/responses")
def responses(body: dict[str, Any]) -> dict[str, Any]:  # sync: FastAPI runs it in a thread
    text = str(body.get("input", ""))
    lowered = text.lower()
    if lowered.startswith("remember "):
        with MEMORY.open("a", encoding="utf-8") as handle:
            handle.write(text[len("remember ") :] + "\n")
        reply = "Noted."
    elif "what do you remember" in lowered:
        facts = MEMORY.read_text(encoding="utf-8").splitlines() if MEMORY.exists() else []
        reply = "I remember: " + "; ".join(facts) if facts else "Nothing yet."
    else:
        reply = "Say 'remember <fact>' or ask 'what do you remember?'."
    return {
        "object": "response",
        "status": "completed",
        "output": [
            {
                "type": "message",
                "role": "assistant",
                "content": [{"type": "output_text", "text": reply}],
            }
        ],
        "output_text": reply,
    }
