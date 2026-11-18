# Fork invariants — must keep working

Behavioral contracts for getkarta customizations on top of upstream LiveKit SIP.
Use this when backmerging `upstream/main`, cherry-picking, or refactoring shared files.
Docs/RCA history are out of scope; only runtime behavior matters here.

Companion: [`upstream-merge-notes.md`](./upstream-merge-notes.md) — upstream features, agent touchpoints, and merge risks.

---

## 1. Direct-SIP flow_id → To rewrite (inbound)

**Why:** PhonePe (and future direct-SIP clients) send INVITEs with a `flow_id` in a header.
We rewrite `To` to a route key (fake dialed number) so LiveKit trunk + dispatch rule matching works.

### Must keep working

1. **Hot path is in-memory only** — no network call during INVITE handling for the map lookup.
2. **Default extractor** — `User-to-User` with `uuid|flow_id`, encoding `auto` (plain / base64 / hex).
3. **Per-source extractors** — source IP (and CIDR) can override header name / format / encoding.
4. **Lookup order** — `DirectRoutes` store first, then YAML `FlowIDLookup` fallback.
5. **On hit** — replace `callInfo.To.User` with mapped route key before auth / trunk / dispatch.
6. **On miss / unparsed** — leave `To` unchanged; log warn/info; do not fail the INVITE solely for unknown `flow_id`.
7. **Cold start bootstrap** — if `DirectRoutesBootstrapURL` is set, periodically fetch full map into the store (voice-service).
8. **Live replace** — private HTTP `PUT /private/custom-sip/routes` replaces the in-memory map (VPC-only, no auth by design).
9. **GET** — `GET /private/custom-sip/routes` returns current snapshot.
10. **JSON formats** — accept nested `{sources, default}`, legacy wrapped `{routes, extractors_by_source}`, and flat `{flow_id: route_key}`.
11. **YAML seed** — `config/flow_id_lookup.yaml` (or `SIP_FLOW_ID_LOOKUP_FILE`) still seeds the store / fallback when present; image path `/sip/flow_id_lookup.yaml`.
12. **Enablement** — `direct_routes_enabled` or env `SIP_DIRECT_ROUTES_ENABLED`; auto-enable when bootstrap URL or private listen addr is set.

### Config / env knobs

| Knob | Role |
|---|---|
| `flow_id_lookup` / `/sip/flow_id_lookup.yaml` | Seed / fallback map |
| `direct_routes_enabled` / `SIP_DIRECT_ROUTES_ENABLED` | Master switch |
| `direct_routes_bootstrap_url` / `SIP_DIRECT_ROUTES_BOOTSTRAP_URL` | Cold-start + refresh from voice-service |
| `private_listen_addr` / `SIP_PRIVATE_LISTEN_ADDR` (or `SIP_ADMIN_LISTEN_ADDR`) | Private HTTP listener |

### Shared-file hooks to re-apply after merge

- `pkg/sip/inbound.go` — extract + rewrite in `processInvite`
- `pkg/config/config.go` — load / lookup / InitDirectRoutes
- `pkg/service/service.go` — private HTTP server
- `cmd/livekit-sip/main.go` — bootstrap loop
- Entire `pkg/customsip/` (additive)

### Smoke checks

- INVITE with known PhonePe UUI → `To` rewritten; trunk matches.
- INVITE with unknown `flow_id` → call proceeds with original `To`; warn logged.
- Bootstrap URL returns map → store size updates; metric `direct_sip_map_replace_total{source="bootstrap"}`.
- `PUT` valid JSON → map replaced; `GET` reflects it.
- `PUT` invalid JSON → 400; previous map unchanged.

---

## 2. Outbound CreateSIPParticipant routing (SSM IP allowlist)

**Why:** Public and PhonePe/VPN SIP workers share Redis + Room Server. Only public `nat_1_to_1_ip` nodes may win outbound jobs; wrong node → wrong SDP IP / Plivo `403 Domestic Anchored Terms Not Met`.

**Note:** `livekit/protocol` ≥1.45 registers CreateSIPParticipant with `affinityFunc=nil`, so affinity alone is dead. Primary gate is **topic registration**.

### Must keep working

1. **Registration gate (primary)** — `pkg/service/service.go` calls `RegisterCreateSIPParticipantTopic` **only** when `AllowsOutbound()` is true. Disallowed nodes must not advertise for outbound.
2. **Handler reject (belt)** — `CreateSIPParticipant` returns `PermissionDenied` when `!AllowsOutbound()`.
3. **Affinity (if protocol re-wires it)** — return **`-1`** (not `0`) when disallowed / trunk mismatch / at capacity; psrpc only hard-skips `< 0`. Then upstream load scoring.
4. **Allowlist match** — this node’s `nat_1_to_1_ip` must be in `OutboundNodeIPs` when the list is non-empty.
5. **Load order** — yaml `outbound_node_ips` → env `SIP_OUTBOUND_NODE_IPS` (JSON) → SSM (`SIP_OUTBOUND_NODE_IPS_SSM` or `/livekit/{ENV}/sip/outbound_node_ips`).
6. **SSM failure is fatal** when `ENV` / explicit SSM path is set (do not silently open outbound on all nodes).
7. **Inbound unaffected** — INVITE path must not use this allowlist.
8. **Voice agent unchanged** — no `allowed_node_ips` required on the CreateSIPParticipant request.

### Shared-file hooks

- `pkg/service/service.go` — conditional `RegisterCreateSIPParticipantTopic`
- `pkg/sip/service.go` — `CreateSIPParticipant` reject + `CreateSIPParticipantAffinity`
- `pkg/config/outbound_node_ips.go` + fields in `pkg/config/config.go`

### Smoke checks

- Node IP in SSM list → logs `registered CreateSIPParticipant topic`; handles Plivo outbound.
- Node IP not in list → logs `skipping CreateSIPParticipant registration`; **zero** `Creating SIP participant` for public trunks.
- No `ENV` / empty list → all nodes may register (dev).

---

## 3. Metrics (ops / capacity)

### Must keep working

| Metric | Contract |
|---|---|
| `livekit_sip_calls_inprogress{dir,node_id}` | Gauge of currently tracked inbound/outbound calls (setup + connected until cleanup). Registered at SIP service start; unregistered on monitor stop. |
| `livekit_sip_direct_sip_flow_rewrite_total{result,node_id}` | Incremented on every flow_id lookup attempt; `result=hit\|miss`. |
| `livekit_sip_direct_sip_map_size{node_id}` | Current in-memory route entry count after bootstrap / PUT / seed. |
| `livekit_sip_direct_sip_map_replace_total{source,result,node_id}` | Replace attempts; `source=bootstrap\|private_put`, `result=ok\|error`. |

Also: if SIP `Start()` fails after monitor start, monitor must be stopped (no leaked Prometheus collectors).

### Shared-file hooks

- `pkg/stats/active_calls.go`, `pkg/stats/monitor.go`
- `pkg/sip/service.go` — `RegisterActiveCalls` + startup defer

---

## 4. Inbound agent-wait diagnostics

**Why:** Distinguish “caller cancelled / timed out because no agent joined” from other failures.

### Must keep working

1. Room tracks `agentJoined` (non-SIP remote participant) and `remoteCount`.
2. Wait / cancel / timeout / subscribe / close logs include outcome fields (at least `agentJoined`, remote count, and related timing/outcome keys already used in `outcomeLogValues`).
3. Logging must not change SIP answer/reject semantics — diagnostics only.

### Shared-file hooks

- `pkg/sip/room.go`
- `pkg/sip/inbound.go` (outcome helpers + Infow sites)

---

## 5. Build & deploy (getkarta)

### Must keep working

1. CI builds and tests on `main`.
2. On `main`, multi-arch image (`linux/amd64`, `linux/arm64`) pushes to our ECR tag `sip-server-prod:custom` via OIDC (do not revert to upstream DockerHub release workflow as the primary path).
3. Image includes `/sip/flow_id_lookup.yaml` when that seed file exists in the repo.
4. Dockerfile `go mod download` retries remain acceptable (flaky proxy).

Upstream `build.yaml` / `docker.yaml` may return after merge; our ECR deploy path must still exist and remain the one we ship from.

---

## 6. Merge conflict hotspots (checklist)

When resolving `upstream/main` into our fork, re-verify each item:

- [ ] `inbound.go` — flow extract + To rewrite still runs before trunk/dispatch
- [ ] `inbound.go` — outcome / agent-wait logs still present
- [ ] `service.go` (sip) — `AllowsOutbound()` gate **then** upstream load/`sip_trunk_ids` affinity
- [ ] `service.go` (sip) — active-calls collector registered; monitor cleanup on failed Start
- [ ] `room.go` — `agentJoined` / `remoteCount`
- [ ] `config.go` — FlowID / DirectRoutes / OutboundNodeIPs load in `Init`
- [ ] `pkg/service/service.go` — private routes HTTP server
- [ ] `cmd/livekit-sip/main.go` — bootstrap loop
- [ ] `pkg/stats/monitor.go` — DirectSIP metrics + helpers
- [ ] `pkg/customsip/` intact and compiling
- [ ] `outbound_node_ips.go` intact
- [ ] CI/ECR deploy still pushes our image
- [ ] Dockerfile still copies flow_id lookup seed
- [ ] `go.mod` still has AWS SSM SDK if affinity SSM load remains

---

## 7. Explicit non-goals

- RCA / incident markdown under `docs/`
- Restoring upstream DockerHub release as our ship path
- Changing voice-agent CreateSIPParticipant request shape for affinity
