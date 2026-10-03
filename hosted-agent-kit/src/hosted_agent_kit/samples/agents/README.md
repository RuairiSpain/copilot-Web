# Sample hosted agents

Each folder is one Foundry hosted agent plus the scheduler configuration the pool uses for it.

| Folder | Protocol | State | What it shows |
|---|---|---|---|
| `support-bot/` | Responses | stateless | A pool of interchangeable sessions with warm capacity, queueing and streaming |
| `memory-bot/` | Responses | stateful | One sandbox per user and conversation; the agent remembers earlier turns |
| `doc-processor/` | Invocations | stateless | Any request body and content type, relayed unchanged |

Files in each folder:

- `agent.py`: the container's code. It follows the hosted-agent contract: port 8088,
  `GET /readiness`, and `POST /responses` or `POST /invocations`.
- `Dockerfile` and `requirements.txt`: build the container.
- `azure.yaml`: deploys the agent with `azd`. The agent's `name` in Foundry must equal the agent
  name in `scheduler.yaml`.
- `scheduler.yaml`: how this kit's pool schedules sessions for the agent.

Deploy one agent, then point the kit at the project:

```bash
cd support-bot
azd ai agent init     # once
azd up
export FOUNDRY_PROJECT_ENDPOINT="https://<account>.services.ai.azure.com/api/projects/<project>"
```

Without `FOUNDRY_PROJECT_ENDPOINT`, the FastAPI samples run in demo mode against an in-memory
agent, so you can try the API before deploying anything.
