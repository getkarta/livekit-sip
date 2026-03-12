# LiveKit SIP — Per-Worker IP Filtering via Affinity Function

## Problem

We run two SIP EC2 instances sharing the same Redis and LiveKit Room Server:

- **Plivo EC2** — public subnet, `nat_1_to_1_ip: 13.233.54.246` (EIP). Plivo connects over public internet and requires the EIP in SDP.
- **PhonePe EC2** — private subnet, `nat_1_to_1_ip: 10.0.0.196`. PhonePe connects over a site-to-site IPsec VPN tunnel and requires the private IP in SDP. Their policy explicitly disallows public IPs.

Because both workers share the same psrpc job queue, an outbound call intended for PhonePe may be picked up by the Plivo worker (and vice versa). The wrong worker advertises the wrong IP in SDP, causing the call to fail.

## Solution

Use `CreateSIPParticipantAffinity` — the psrpc hook called before job assignment — to filter workers based on their `nat_1_to_1_ip`. The caller passes an `allowed_node_ips` attribute when creating the SIP participant. Workers that don't match return affinity `0`, effectively opting out.

No proto changes. No trunk config changes. `allowed_node_ips` is always expected to be passed — workers always check membership and return `0` if not matched.

## Code Change

**File:** `pkg/sip/service.go`

Replace the existing affinity function:

```go
// BEFORE
func (s *Service) CreateSIPParticipantAffinity(ctx context.Context, req *rpc.InternalCreateSIPParticipantRequest) float32 {
    // TODO: scale affinity based on a number or active calls?
    return 0.5
}
```

```go
// AFTER
func (s *Service) CreateSIPParticipantAffinity(ctx context.Context, req *rpc.InternalCreateSIPParticipantRequest) float32 {
    allowed := getAllowedNodeIPs(req.GetParticipantAttributes())
    myIP := s.conf.NAT1To1IP
    for _, ip := range allowed {
        if ip == myIP {
            return 0.5
        }
    }
    return 0
}

func getAllowedNodeIPs(attrs map[string]string) []string {
    val, ok := attrs["allowed_node_ips"]
    if !ok || val == "" {
        return nil
    }
    var ips []string
    if err := json.Unmarshal([]byte(val), &ips); err != nil {
        return nil
    }
    return ips
}
```

Add `"encoding/json"` to the import block if not already present.

## Caller Side (Agent)

When initiating an outbound call to PhonePe, pass `allowed_node_ips` as a participant attribute:

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

For Plivo outbound calls — omit `allowed_node_ips` entirely. Any worker picks it up as before.

## SIP Config per EC2

The yaml field name depends on the version of the SIP binary you build from the fork:

- **Older versions** use `node_ip` (what your current working Plivo deploy script uses)
- **Current master** uses `nat_1_to_1_ip`

Check which yaml tag is in your forked version's `pkg/config/config.go` and use that. The Go struct field is always `s.conf.NAT1To1IP` regardless.

```yaml
# Plivo EC2 (check your version's yaml tag)
nat_1_to_1_ip: 13.233.54.246   # or node_ip: 13.233.54.246

# PhonePe EC2
nat_1_to_1_ip: 10.0.0.196      # or node_ip: 10.0.0.196
```

If you also want RTP to use the correct IP (recommended), set `media_nat_1_to_1_ip` to the same value on each EC2.

## What Does Not Change

- Inbound call routing — unaffected, inbound uses `GetAuthCredentials` + `DispatchCall`, not affinity
- Trunk configuration — no new fields on `SIPOutboundTrunkInfo`
- Protocol/proto — no changes to `livekit/protocol`
- Redis, Room Server, Egress — untouched
- Any existing Plivo calls — no change in behaviour