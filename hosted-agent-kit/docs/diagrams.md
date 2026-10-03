# Diagrams

Diagrams (seven pictures in five topics) of how the kit is built and how it controls session quota. GitHub renders the
Mermaid blocks, and rendered copies are in `diagrams/`. To get them into Figma, paste a block into FigJam's Mermaid import, or use the
Figma connector's `generate_diagram` tool.

The regional limit of 1,000 sessions is an assumption taken from public documentation summaries.
It is not confirmed (see `quota.md`, "Open questions").

## 1. Components

The kit runs inside your application. Everything outside the kit is behind a port, so each
adapter can be replaced or faked in tests.

```mermaid
flowchart TB
    app["Your application<br/>(FastAPI or any asyncio code)"]

    subgraph kit["Hack (the kit), one per process"]
        facade["Hack facade<br/>responses, ask, invocations,<br/>reporting, admin"]
        own["OwnershipManager<br/>leases, standby, self-fencing"]
        idem["IdempotencyStore"]

        subgraph rt["Runtime"]
            pool["PoolService<br/>queue, per-user fairness,<br/>per-agent lock"]
            sched["Scheduling framework<br/>filters, scores, reserve"]
            gate["QuotaGate<br/>counted sessions, admission,<br/>eviction by stop_session"]
            gov["KitGovernor<br/>budget, adaptive limit,<br/>borrowed permits"]
            remote["RemoteService<br/>create, retry, delete"]
            reg[("Session registry<br/>in memory")]
            ctl["Controllers<br/>observation, session, gc,<br/>pool, status, health"]
            cfg["ConfigHolder<br/>hot reload"]
        end
    end

    subgraph ports["Ports and adapters"]
        fa["FoundryAdapter<br/>azure-ai-projects"]
        os["OwnershipStore<br/>memory or Redis"]
        led["QuotaLedger<br/>memory or Redis"]
        met["Metrics<br/>OpenTelemetry"]
    end

    foundry["Microsoft Foundry<br/>hosted agents, sessions"]
    redis[("Redis<br/>optional")]

    app --> facade
    facade --> idem
    facade --> pool
    facade -. "role check" .-> own
    pool --> sched
    sched --> gate
    gate --> gov
    pool --> remote
    pool --> reg
    gate --> reg
    ctl --> reg
    ctl --> cfg
    pool --> cfg
    remote --> fa
    ctl --> fa
    gate --> fa
    own --> os
    gov --> led
    pool --> met
    fa --> foundry
    os --> redis
    led --> redis
```

## 2. A call that creates a session

A user's first call to a stateful agent. The kit admits the call, checks quota, creates the
session in Foundry, runs the call on it, and returns the session to the pool. The reconciler
works in the background and corrects the kit's view from Foundry's listing.

```mermaid
sequenceDiagram
    autonumber
    actor U as User
    participant A as Your app
    participant K as Hack facade
    participant P as PoolService
    participant S as Scheduler
    participant Q as QuotaGate
    participant R as Registry
    participant F as FoundryAdapter
    participant H as Hosted agent session
    participant C as Reconciler

    U->>A: request
    A->>K: responses(agent, user_id, input)
    K->>K: check role is active, size limit, idempotency key
    K->>P: execute(request)
    P->>P: per-user queue limit, fairness turn
    P->>S: find a session for this user
    S->>R: view sessions of the agent
    R-->>S: none usable for this user
    S->>Q: reserve_create(agent)
    Q->>Q: persisted limit, agent limit, kit limit
    alt a limit is reached and an idle session exists
        Q->>F: stop_session(least recently used idle)
        F-->>Q: stopped, state kept
    end
    Q->>R: reserve a slot
    Q-->>S: reserved
    S-->>P: create
    P->>F: create_session(agent)
    F->>H: provision
    H-->>F: active
    F-->>P: session id
    P->>R: register session, lease it to the user
    P->>F: invoke(session, input, per-user headers)
    F->>H: run the call
    H-->>F: response or event stream
    F-->>P: response
    P->>R: release lease, record last activity
    P-->>K: result
    K-->>A: AgentResult
    A-->>U: answer
    C->>F: list sessions (every sync interval)
    F-->>C: sessions as Foundry sees them
    C->>R: correct the registry, delete failed sessions
```

## 3. Stateful and stateless sessions

A stateless agent shares sessions between users and reuses any free one. A stateful agent gives
each user a session of their own and always sends that user back to it.

```mermaid
flowchart LR
    subgraph sl["Stateless agent: sessions are interchangeable"]
        direction TB
        u1(["User A"]) --> pl1["Pool"]
        u2(["User B"]) --> pl1
        u3(["User C"]) --> pl1
        pl1 -- "any free session" --> s1["Session 1"]
        pl1 --> s2["Session 2"]
        s1 --- n1["No user data kept.<br/>Next call may use any session.<br/>Idle sessions can be reused or removed freely.<br/>Count of sessions follows load."]
    end

    subgraph sf["Stateful agent: one session per user"]
        direction TB
        v1(["User A"]) --> pl2["Pool with affinity"]
        v2(["User B"]) --> pl2
        v3(["User C"]) --> pl2
        pl2 -- "always A's session" --> t1["Session of A<br/>files, memory"]
        pl2 -- "always B's session" --> t2["Session of B"]
        pl2 -- "always C's session" --> t3["Session of C"]
        t1 --- n2["Session holds the user's state.<br/>The user waits if it is busy,<br/>never moved to another session.<br/>Idle sessions are stopped, not deleted,<br/>so state survives and the session resumes."]
    end
```

```mermaid
stateDiagram-v2
    [*] --> Creating: first call for the user
    Creating --> Active: Foundry reports active
    Active --> Leased: call starts
    Leased --> Active: call ends
    Active --> Stopped: idle too long, or evicted for quota
    Stopped --> Resuming: next call by the same user
    Resuming --> Active: compute is back
    Active --> Deleting: version drain, failure, or admin delete
    Stopped --> Deleting: admin delete
    Deleting --> [*]
    note right of Stopped
        Foundry keeps the session's state.
        A stateless session in this
        state can be resumed by any user.
        It does not count against
        the active limit.
    end note
```

## 4. A shared regional limit that other projects can use up

The regional session limit belongs to the subscription and region. Every project and every kit
draws on the same number. A kit has no control over the other projects, so a "noisy neighbour"
can use most of it, and then the kit's own `create_session` calls are refused with a 429.

```mermaid
flowchart TB
    subgraph region["Subscription and region: one shared limit (assumed 1,000 sessions)"]
        direction TB
        bar["In use right now: 1,000 of 1,000 sessions"]

        subgraph pa["Project A (noisy neighbour, not under your control)"]
            a1["Many agents<br/>load test or runaway loop<br/>760 sessions"]
        end
        subgraph pb["Project B (other team)"]
            b1["Agents<br/>180 sessions"]
        end
        subgraph pc["Project C (yours)"]
            direction TB
            kit1["Kit 1<br/>budget 60, holding 40"]
            kit2["Kit 2<br/>budget 40, holding 20"]
        end
    end

    bar --- pa
    bar --- pb
    bar --- pc

    kit1 -- "create_session (41st, within its own budget)" --> refuse["429 regional_session_quota_exceeded<br/>the region is full, although<br/>Kit 1 is within its budget"]
    refuse --> gov2["KitGovernor lowers the kit's limit<br/>and the call returns RegionalCapacityError<br/>with Retry-After"]
    gov2 --> note2["The kit protects itself and its callers:<br/>it stops asking, queues or rejects calls,<br/>and keeps what it already holds.<br/>It cannot take sessions back from A."]
```

What the kit can and cannot do:

| The kit can | The kit cannot |
|---|---|
| Keep its own sessions within a budget, so it never uses more than its share | Stop another project from using the limit |
| Stop its own idle sessions to free compute | Reclaim sessions held by other projects |
| Lower its limit after a quota 429 and report `RegionalCapacityError` | Know the regional total without a shared ledger or a platform usage API |
| Show other kits in the same ledger and the regional total (with Redis) | See kits that do not share the ledger |

## 5. Per-agent quota and the controller that raises and lowers it

Each agent has a limit, and the kit has a total limit above them. The governor changes the
kit's limit over time. It lowers the limit after Foundry refuses a session. It raises it slowly
when there has been no refusal. With a shared ledger it can also borrow spare permits and give
them back when it no longer needs them.

```mermaid
flowchart TB
    subgraph limits["Limits the kit applies to every new or resumed session"]
        direction TB
        kitb["Kit limit<br/>quota.budget, adjusted by the governor<br/>plus borrowed permits"]
        a1["Agent 1<br/>max_active_sessions"]
        a2["Agent 2<br/>max_active_sessions"]
        a3["Agent 3<br/>max_active_sessions"]
        kitb --> a1
        kitb --> a2
        kitb --> a3
    end

    subgraph ctl["KitGovernor, checked every quota tick (5 s)"]
        direction TB
        d1{"Did Foundry refuse<br/>a session for quota?"}
        dec["Lower the limit<br/>limit = max(min_limit, limit x 0.7)<br/>then wait out the cooldown (30 s)"]
        d2{"Quiet for probe_seconds (60 s)<br/>and below the ceiling?"}
        inc["Raise the limit by one step<br/>(at least 5% of the ceiling)"]
        d3{"Counted sessions at the limit<br/>and a spare pool is configured?"}
        bor["Borrow a permit from the<br/>shared spare pool (ledger)"]
        d4{"Counted sessions at least one<br/>permit below the limit?"}
        giv["Give borrowed permits back"]
        d1 -- yes --> dec
        d1 -- no --> d2
        d2 -- yes --> inc
        d2 -- no --> d3
        d3 -- yes --> bor
        d3 -- no --> d4
        d4 -- yes --> giv
    end

    ledger[("Ledger (Redis)<br/>spare pool and each kit's count")]
    bor <--> ledger
    giv <--> ledger
    ctl --> kitb
```

The governor runs these checks independently: a refusal is handled when it happens, and the
raise, borrow and give-back checks run on each tick. The chart lists them in order only to
keep it readable.

The limit over time, for one kit with a budget of 100:

```mermaid
sequenceDiagram
    autonumber
    participant K as Kit (budget 100)
    participant G as KitGovernor
    participant L as Ledger
    participant F as Foundry

    Note over K,F: Normal: limit 100, holding 100
    K->>F: create_session
    F-->>K: 429 regional_session_quota_exceeded
    K->>G: on_refused
    G->>G: limit 100 to 70, cooldown starts
    Note over K,G: New sessions wait or are refused above 70.<br/>Idle sessions above 70 are stopped when needed.
    G->>G: 60 s without a refusal: 70 to 75
    G->>G: 60 s without a refusal: 75 to 80
    Note over G,L: Spare pool configured: demand is high
    G->>L: acquire spare permit
    L-->>G: granted (limit 81)
    K->>F: create_session
    F-->>K: ok
    Note over G,L: Demand falls, counted sessions drop
    G->>L: release spare permit
    L-->>G: released (limit 80)
    G->>G: later probes raise the limit back toward 100,<br/>never above the budget
```
