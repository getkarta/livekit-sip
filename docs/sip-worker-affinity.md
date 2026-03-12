# SIP Worker Affinity — IP-Based Call Routing

## The Problem (In Plain English)

Imagine you have two phone-line workers (SIP servers) sitting in the same office (shared Redis + LiveKit Room Server):

- **Worker A (Plivo)** — talks to the outside world using a **public IP** (`13.233.54.246`)
- **Worker B (PhonePe)** — talks over a **private VPN tunnel** using a **private IP** (`10.0.0.196`)

When someone says "make a call to PhonePe", **either worker might pick up the job** — there's no filter. If Worker A picks it up, it tells PhonePe "hey, connect to my public IP", but PhonePe's firewall blocks public IPs. The call fails.

```mermaid
flowchart LR
    Agent["Your App / Agent"]
    Queue["Shared Job Queue\n(psrpc via Redis)"]
    Plivo["Worker A\n(Plivo)\nPublic IP:\n13.233.54.246"]
    PhonePe["Worker B\n(PhonePe)\nPrivate IP:\n10.0.0.196"]
    PlivoGW["Plivo\nGateway"]
    PhonePeGW["PhonePe\nGateway"]

    Agent -->|"Make a call"| Queue
    Queue -->|"Either worker\nmight pick it up!"| Plivo
    Queue -->|"Either worker\nmight pick it up!"| PhonePe
    Plivo -.->|"Public IP in SDP ✅"| PlivoGW
    Plivo -.->|"Public IP in SDP ❌"| PhonePeGW
    PhonePe -.->|"Private IP in SDP ❌"| PlivoGW
    PhonePe -.->|"Private IP in SDP ✅"| PhonePeGW

    style Plivo fill:#4CAF50,color:#fff
    style PhonePe fill:#2196F3,color:#fff
    style PlivoGW fill:#81C784,color:#000
    style PhonePeGW fill:#64B5F6,color:#000
```

**The wrong worker picks up the call → wrong IP in SDP → call fails.**

---

## The Solution

We added a simple filter: when making a call, you say **"only workers with THIS IP should handle it"**. Workers that don't match simply say "not me" (return affinity `0`), and the job goes to the right worker.

```mermaid
flowchart LR
    Agent["Your App / Agent"]
    Queue["Shared Job Queue\n(psrpc via Redis)"]
    Plivo["Worker A\n(Plivo)\nIP: 13.233.54.246"]
    PhonePe["Worker B\n(PhonePe)\nIP: 10.0.0.196"]
    PhonePeGW["PhonePe\nGateway"]

    Agent -->|"Call PhonePe\nallowed_node_ips:\n[10.0.0.196]"| Queue
    Queue -->|"Am I 10.0.0.196?\nNo → skip"| Plivo
    Queue -->|"Am I 10.0.0.196?\nYes → take it!"| PhonePe
    PhonePe -->|"Private IP in SDP ✅"| PhonePeGW

    style Plivo fill:#EF5350,color:#fff
    style PhonePe fill:#4CAF50,color:#fff
    style PhonePeGW fill:#81C784,color:#000
```

---

## How It Works Step by Step

```mermaid
sequenceDiagram
    participant App as Your App / Agent
    participant Redis as Job Queue (Redis)
    participant WA as Worker A (Plivo)<br/>IP: 13.233.54.246
    participant WB as Worker B (PhonePe)<br/>IP: 10.0.0.196
    participant GW as PhonePe Gateway

    App->>Redis: Create SIP Participant<br/>allowed_node_ips: ["10.0.0.196"]

    Redis->>WA: "What's your affinity?"
    Note right of WA: My IP is 13.233.54.246<br/>Not in allowed list<br/>→ return 0 (skip)
    WA-->>Redis: Affinity = 0

    Redis->>WB: "What's your affinity?"
    Note right of WB: My IP is 10.0.0.196<br/>Found in allowed list!<br/>→ return 0.5 (take it)
    WB-->>Redis: Affinity = 0.5

    Redis->>WB: You win! Handle this call.
    WB->>GW: SIP INVITE (private IP in SDP ✅)
    GW-->>WB: 200 OK
```

---

## The Code Change Explained

We modified one function and added one helper in [service.go](../pkg/sip/service.go).

### Before (every worker always says "I can do it")

```mermaid
flowchart TD
    A["CreateSIPParticipantAffinity called"] --> B["return 0.5\n(always willing)"]

    style A fill:#FFF9C4
    style B fill:#EF5350,color:#fff
```

Every worker returned `0.5` no matter what → any worker could pick up any call.

### After (workers check if they're allowed)

```mermaid
flowchart TD
    A["CreateSIPParticipantAffinity called"] --> B{"Does the request have\nallowed_node_ips?"}
    B -->|"No (or invalid)"| C["return 0.5\n(default — any worker OK)"]
    B -->|"Yes"| D{"Is my IP in\nthe allowed list?"}
    D -->|"Yes"| E["return 0.5\n(I'm allowed!)"]
    D -->|"No"| F["return 0\n(not me, skip)"]

    style A fill:#FFF9C4
    style C fill:#4CAF50,color:#fff
    style E fill:#4CAF50,color:#fff
    style F fill:#EF5350,color:#fff
```

### Key behaviors

| Scenario | What happens | Affinity |
|---|---|---|
| No `allowed_node_ips` in request | Any worker can pick it up (backwards compatible) | `0.5` |
| `allowed_node_ips` present, worker IP matches | This worker takes the call | `0.5` |
| `allowed_node_ips` present, worker IP doesn't match | This worker opts out | `0` |
| `allowed_node_ips` is malformed JSON | Treated as "no filter" (safe fallback) | `0.5` |

---

## How to Use It (Caller / Agent Side)

When your app creates an outbound SIP call, pass the `allowed_node_ips` attribute to target specific workers.

### Target the PhonePe worker only

```python
await lk_api.sip.create_sip_participant(
    api.CreateSIPParticipantRequest(
        sip_trunk_id="phonepe-trunk-id",
        sip_call_to="+91XXXXXXXXXX",
        room_name=ctx.room.name,
        participant_identity="phonepe-participant",
        participant_attributes={
            "allowed_node_ips": '["10.0.0.196"]'
        }
    )
)
```

### Target the Plivo worker only

```python
await lk_api.sip.create_sip_participant(
    api.CreateSIPParticipantRequest(
        sip_trunk_id="plivo-trunk-id",
        sip_call_to="+1XXXXXXXXXX",
        room_name=ctx.room.name,
        participant_identity="plivo-participant",
        participant_attributes={
            "allowed_node_ips": '["13.233.54.246"]'
        }
    )
)
```

### Allow either worker (backwards compatible)

Simply don't include `allowed_node_ips` — works exactly like before.

```python
await lk_api.sip.create_sip_participant(
    api.CreateSIPParticipantRequest(
        sip_trunk_id="any-trunk-id",
        sip_call_to="+1XXXXXXXXXX",
        room_name=ctx.room.name,
        participant_identity="any-participant"
        # no allowed_node_ips → any worker picks it up
    )
)
```

---

## Worker Configuration

Each SIP worker's config file tells it what IP to use. The affinity function reads this IP to decide "am I the right worker?"

```mermaid
flowchart LR
    subgraph "Plivo EC2"
        ConfigA["config.yaml\nnat_1_to_1_ip: 13.233.54.246"]
        WorkerA["SIP Worker A"]
        ConfigA --> WorkerA
    end
    subgraph "PhonePe EC2"
        ConfigB["config.yaml\nnat_1_to_1_ip: 10.0.0.196"]
        WorkerB["SIP Worker B"]
        ConfigB --> WorkerB
    end

    style ConfigA fill:#FFF9C4
    style ConfigB fill:#FFF9C4
    style WorkerA fill:#4CAF50,color:#fff
    style WorkerB fill:#2196F3,color:#fff
```

```yaml
# Plivo EC2 config
nat_1_to_1_ip: 13.233.54.246

# PhonePe EC2 config
nat_1_to_1_ip: 10.0.0.196
```

---

## What's NOT Affected

```mermaid
flowchart TD
    subgraph "✅ Changed"
        A["Outbound call\nworker selection"]
    end
    subgraph "❌ Not changed"
        B["Inbound calls"]
        C["Trunk configuration"]
        D["Protocol / Protobuf"]
        E["Redis / Room Server"]
        F["Existing Plivo calls"]
    end

    style A fill:#4CAF50,color:#fff
    style B fill:#E0E0E0
    style C fill:#E0E0E0
    style D fill:#E0E0E0
    style E fill:#E0E0E0
    style F fill:#E0E0E0
```

- **Inbound calls** — use a completely different path (`GetAuthCredentials` + `DispatchCall`), untouched
- **Trunk config** — no new fields needed on `SIPOutboundTrunkInfo`
- **Protocol/Protobuf** — no changes to `livekit/protocol`
- **Redis, Room Server, Egress** — completely untouched
- **Existing Plivo calls** — if you don't pass `allowed_node_ips`, behavior is identical to before

---

## Test Coverage

We added 14 unit tests covering both the helper function and the affinity logic:

| Test Group | Cases | What it validates |
|---|---|---|
| `TestGetAllowedNodeIPs` (7 tests) | nil attrs, empty attrs, missing key, empty value, invalid JSON, single IP, multiple IPs | The JSON parsing helper handles every edge case gracefully |
| `TestCreateSIPParticipantAffinity` (7 tests) | no filter, matching IP, non-matching IP, match in list, no match in list, bad JSON fallback, empty value fallback | The affinity function returns the right score in every scenario |
