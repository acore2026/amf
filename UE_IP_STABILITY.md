# UE IP Stability Troubleshooting Guide

This document covers diagnosing and resolving UE IP address instability in free5GC deployments where a UE's IP changes periodically, preventing reliable network-side connectivity.

---

## Quick Start: Configure Static IP + Keepalive

This section provides step-by-step instructions for the recommended solution.

### Prerequisites

- free5GC core running (AMF, SMF, UPF, UDM, UDR, MongoDB)
- UE registered or able to register
- Host has route to UE IP subnets (check: `ip route | grep 10.60`)

### Step 1: Configure Static IP in Subscriber Data

Choose static IPs within the UPF pool ranges (see table below). Avoid previously-leaked addresses — if a static IP was used before and the SMF leaked it, pick a different one.

```bash
# Set static IP for a UE (replace <SUPI>, <INET_IP>, <IMS_IP>)
docker exec mongodb mongo -quiet free5gc --eval '
db.getCollection("subscriptionData.provisionedData.smData").updateOne(
  {ueId: "<SUPI>"},
  {$set: {
    "dnnConfigurations.internet.staticIpAddress": [{"ipv4Addr": "<INET_IP>"}],
    "dnnConfigurations.ims.staticIpAddress": [{"ipv4Addr": "<IMS_IP>"}]
  }}
)'

# Verify
docker exec mongodb mongo -quiet free5gc --eval '
var d = db.getCollection("subscriptionData.provisionedData.smData").findOne({ueId: "<SUPI>"});
print("internet: " + JSON.stringify(d.dnnConfigurations.internet.staticIpAddress));
print("ims: " + JSON.stringify(d.dnnConfigurations.ims.staticIpAddress));'
```

**UPF pool ranges** (from `upfcfg.yaml`):

| DNN | CIDR | Example Static IP |
|-----|------|-------------------|
| internet | 10.60.0.0/16 | 10.60.0.200 |
| ims | 10.64.0.0/16 | 10.64.0.200 |

### Step 2: Trigger One Re-Registration

The static IP takes effect only on the next session creation. If a keepalive is running, stop it temporarily and let one re-registration cycle happen (within ~200s):

```bash
# Stop existing keepalive (if running)
kill <keepalive_pid>

# Wait for the next re-registration and verify static IP allocation
timeout 250 docker logs --follow --since 30s smf 2>&1 \
  | grep "<SUPI>" | grep -E "Allocated PDUAdress|DNN:"
# Expect: Allocated PDUAdress[<INET_IP>] and Allocated PDUAdress[<IMS_IP>]
# If you see <nil> or "fail to allocate", the IP was leaked — restart SMF:
#   docker restart smf
# Then use different IP addresses and repeat from Step 1
```

If the UE is not re-registering automatically, trigger it manually:
- Toggle airplane mode on the phone, or
- Restart the UE

### Step 3: Verify and Start Keepalive

```bash
# Verify static IPs are reachable
ping -c 3 <INET_IP>
ping -c 3 <IMS_IP>

# Start keepalive script
# (run from the AMF repository root)
chmod +x scripts/ue-keepalive.sh
nohup ./scripts/ue-keepalive.sh --inet <INET_IP> --ims <IMS_IP> \
  --log /var/log/ue-keepalive.log &
```

### Step 4: Verify Stability

Wait 5+ minutes and confirm no re-registration and stable IP:

```bash
# Should show 0 re-registrations
docker logs --since 5m amf 2>&1 | grep "<SUPI>" | grep "Registration Complete" | wc -l

# Should show 0% packet loss in keepalive log
tail -10 /var/log/ue-keepalive.log
```

### Optional: Set Up as systemd Service (Recommended for Production)

```bash
# Create systemd service (replace paths and IPs as needed)
cat > /etc/systemd/system/ue-keepalive.service << EOF
[Unit]
Description=UE Keepalive (prevents gNB RRC inactivity release)
After=docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/path/to/amf/scripts/ue-keepalive.sh --inet 10.60.0.200 --ims 10.64.0.200
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now ue-keepalive
```

### Recovery: If Static IP Leaks (SMF Bug)

If the UE re-registers (phone restart, gNB restart) while keepalive was down, the static IP may leak in the SMF. Symptoms: SMF logs show `Allocated PDUAdress[<nil>]` and `fail to allocate PDU address`.

```bash
# 1. Restart SMF to clear leaked IP pool state
docker restart smf

# 2. Trigger UE re-registration (toggle airplane mode)

# 3. Verify static IP allocation succeeds
docker logs --since 2m smf 2>&1 | grep "<SUPI>" | grep "PDUAdress"
# Expect: Allocated PDUAdress[<INET_IP>] (not <nil>)

# 4. Restart keepalive
nohup ./scripts/ue-keepalive.sh --inet <INET_IP> --ims <IMS_IP> &
```

---

## Problem Description

A UE (e.g., `imsi-001012345678908`) obtains a PDU session IP address that changes every ~200 seconds. Each change is accompanied by a full Initial Registration, causing all PDU sessions to be torn down and rebuilt with new dynamically-allocated IPs.

**Symptoms:**
- Phone shows a different IP every few minutes
- Ping to the UE works briefly, then fails (stale IP)
- SMF logs show repeated `Allocated PDUAdress[10.60.0.x]` with incrementing IPs
- AMF logs show `Registration Complete` every ~200 seconds
- No errors in AMF/SMF/UDM (all procedures succeed)

---

## Root Cause Analysis

### The ~200-Second Re-Registration Cycle

```
① gNB RRC inactivity timer fires (~200s, no user-plane data)
   └─ gNB sends UEContextReleaseRequest (Cause: RadioNetwork[20] = user-inactivity)
      └─ AMF releases NGAP context; AmfUe context lost (AmfUe is nil → AmfUe[] is removed)
         └─ UE reconnects with full Initial Registration (not lightweight Service Request)
            └─ All existing PDU sessions torn down; new sessions created
               └─ SMF allocates new dynamic IP from UPF pool → IP changes
```

### Key Diagnostic Commands

```bash
# Check re-registration frequency
docker logs --since 1h amf 2>&1 | grep "<SUPI>" | grep "Registration Complete" | wc -l

# Check gNB context release cause
docker logs amf 2>&1 | grep "UEContextReleaseRequest" | grep -A1 "Cause"
# Look for: Cause RadioNetwork[20]  (= user-inactivity)

# Check if AmfUe context is lost between cycles
docker logs --since 30m amf 2>&1 | grep -c "AmfUe is nil"

# Check PDU session rebuild pattern
docker logs smf 2>&1 | grep "<SUPI>" | grep -E "Release IP|Allocated PDUAdress"
```

### Why Some UEs Are Stable

UEs with active SIP/IMS keepalive traffic (e.g., port 5060 on the IMS DNN) keep the gNB data plane active, preventing the RRC inactivity timer from firing. These UEs never get released and their sessions/IPs persist indefinitely.

```bash
# Verify: capture GTP-U traffic to check for SIP keepalive
tcpdump -i br-free5gc -nn -c 20 -XX udp port 2152
# Look for inner IP with port 5060 (SIP) - indicates IMS keepalive
```

---

## Solutions

### Solution 1: Static IP Alone (Does NOT Work)

Adding `staticIpAddress` to the subscriber's smData causes the SMF to allocate a fixed IP. However, this **fails** due to a free5GC SMF bug: when the UE re-registers (triggered by the 200s inactivity cycle), the old session's static IP is not properly released (IP leak), causing the next session creation to fail with `fail to allocate PDU address`.

```bash
# This approach FAILS - documented for reference
docker exec mongodb mongo -quiet free5gc --eval '
db.getCollection("subscriptionData.provisionedData.smData").updateOne(
  {ueId: "imsi-001012345678908"},
  {$set: {
    "dnnConfigurations.internet.staticIpAddress": [{"ipv4Addr": "10.60.0.200"}],
    "dnnConfigurations.ims.staticIpAddress": [{"ipv4Addr": "10.64.0.200"}]
  }}
)'

# SMF error after re-registration:
# [ERRO] PDUSessionSMContextCreate err: fail to allocate PDU address
# Allocated PDUAdress[<nil>]
```

### Solution 2: Keepalive Ping Only (Works, But IP Not Fixed)

Sending periodic pings to the UE keeps the gNB data plane active, preventing the inactivity timer. The IP stays stable (session never rebuilds), but it remains a dynamic IP — if the phone restarts, the IP changes.

### Solution 3: Static IP + Keepalive (Recommended)

**Combine both**: static IP provides a fixed address, while keepalive prevents the re-registration cycle that would trigger the IP leak bug.

#### Step 1: Configure Static IP

```bash
# Choose IP addresses within the UPF pool ranges but unlikely to conflict
# with dynamic allocation. Avoid previously-leaked addresses.
docker exec mongodb mongo -quiet free5gc --eval '
db.getCollection("subscriptionData.provisionedData.smData").updateOne(
  {ueId: "imsi-001012345678908"},
  {$set: {
    "dnnConfigurations.internet.staticIpAddress": [{"ipv4Addr": "10.60.0.200"}],
    "dnnConfigurations.ims.staticIpAddress": [{"ipv4Addr": "10.64.0.200"}]
  }}
)'
```

**UPF pool ranges** (from `upfcfg.yaml`):

| DNN | CIDR | Example Static IP |
|-----|------|-------------------|
| internet | 10.60.0.0/16 | 10.60.0.200 |
| ims | 10.64.0.0/16 | 10.64.0.200 |

#### Step 2: Stop Keepalive (Temporarily)

The static IP only takes effect on the next session creation (re-registration). Temporarily stop keepalive to allow one re-registration cycle:

```bash
# Stop existing keepalive process
kill <keepalive_pid>
```

#### Step 3: Wait for Static IP Allocation

Monitor the SMF for the next session creation with the static IP:

```bash
timeout 250 docker logs --follow --since 30s smf 2>&1 \
  | grep "<SUPI>" | grep -E "Allocated PDUAdress|DNN:"
# Expect: Allocated PDUAdress[10.60.0.200] and [10.64.0.200]
```

#### Step 4: Verify and Start Keepalive Immediately

```bash
# Verify static IPs are reachable
ping -c 2 10.60.0.200
ping -c 2 10.64.0.200

# Start keepalive (see script below)
nohup /path/to/keepalive-908.sh >/dev/null 2>&1 &
```

#### Step 5: Verify Stability

Wait 5+ minutes and confirm:
- No re-registration in AMF logs
- No new PDUAdress allocation in SMF logs
- All keepalive pings show 0% loss

```bash
docker logs --since 5m amf 2>&1 | grep "<SUPI>" | grep "Registration Complete" | wc -l
# Expect: 0
```

---

## Keepalive Script

The keepalive script is included in this repository at `scripts/ue-keepalive.sh`.

### Usage

```bash
# Ping both internet and IMS IPs every 30s (default)
./scripts/ue-keepalive.sh --inet 10.60.0.200 --ims 10.64.0.200

# Ping internet only with custom interval
./scripts/ue-keepalive.sh --inet 10.60.0.200 --interval 20

# Custom log path
./scripts/ue-keepalive.sh --inet 10.60.0.200 --ims 10.64.0.200 --log /var/log/ue-keepalive.log

# Run in background
nohup ./scripts/ue-keepalive.sh --inet 10.60.0.200 --ims 10.64.0.200 &

# View help
./scripts/ue-keepalive.sh --help
```

### Options

| Option | Required | Default | Description |
|--------|----------|---------|-------------|
| `--inet <IP>` | Yes (at least one) | — | UE internet PDU session IP |
| `--ims <IP>` | No | — | UE IMS PDU session IP |
| `--interval <sec>` | No | 30 | Ping interval in seconds |
| `--log <path>` | No | /var/log/ue-keepalive.log | Log file path |

### How the gNB Inactivity Timer Works

The gNB monitors the Data Radio Bearer (DRB) for user-plane activity. If no data is sent/received for ~200 seconds, the gNB sends `UEContextReleaseRequest` with `Cause RadioNetwork[20]` (user-inactivity), releasing the UE's NGAP context.

The keepalive script sends ICMP pings every 30 seconds. The packet path:

```
Host → UPF (br-free5gc) → GTP-U tunnel → gNB → DRB → UE
                                                              ↓ (UE replies)
Host ← UPF ← gNB ← DRB ← UE
```

Each ping resets the gNB's inactivity timer. Since 30s << 200s, the timer never fires.

---

## How It Works

```
Static IP (10.60.0.200)         Keepalive (ping every 30s)
      │                              │
      │  ┌───────────────────────────┘
      │  │
      ▼  ▼
   SMF allocates fixed IP  →  Keepalive keeps data plane active
                                         │
                                         ▼
                              gNB inactivity timer never fires
                                         │
                                         ▼
                              No re-registration → static IP never leaks
                                         │
                                         ▼
                              IP permanently stable: 10.60.0.200
```

| Component | Role |
|-----------|------|
| **Static IP** | Ensures the same IP is allocated even if sessions are rebuilt |
| **Keepalive** | Prevents the 200s re-registration cycle (root cause of IP leak) |

---

## Known Issues and Caveats

### Static IP Leak (free5GC SMF Bug)

If the UE re-registers while a static IP session is active (e.g., phone restart, gNB restart), the SMF may not properly release the static IP, causing subsequent session creation to fail with `fail to allocate PDU address`.

**Recovery:**
```bash
# Restart SMF to clear leaked IP pool state
docker restart smf

# Wait for UE to re-register and re-allocate static IP
# Then restart keepalive
```

### Keepalive Script Persistence

The keepalive script runs as a background process. It does not survive:
- Machine reboots
- Manual kill
- Process crashes

**Recommendation:** Convert to a systemd service — see the "Quick Start" section above for a complete systemd service template. The service uses `Restart=always` for auto-recovery.

### IMS/SIP Registration

If the UE's IMS/SIP registration is working (SIP keepalive on port 5060), the keepalive script is unnecessary — the UE naturally maintains data plane activity. Check IMS core status and P-CSCF reachability if SIP keepalive is not occurring.

---

## Diagnostic Quick Reference

| Check | Command | Expected (Healthy) |
|-------|---------|-------------------|
| Re-registration count (1h) | `docker logs --since 1h amf 2>&1 \| grep "<SUPI>" \| grep "Registration Complete" \| wc -l` | 0-1 (periodic update only) |
| gNB release cause | `docker logs amf 2>&1 \| grep "Cause RadioNetwork"` | No user-inactivity [20] |
| AmfUe context loss | `docker logs --since 30m amf 2>&1 \| grep -c "AmfUe is nil"` | 0 |
| IP stability | `docker logs --since 10m smf 2>&1 \| grep "<SUPI>" \| grep "PDUAdress" \| wc -l` | 0 (no new allocations) |
| UE reachability | `ping -c 3 <UE_IP>` | 0% loss |
| GTP-U keepalive traffic | `tcpdump -i br-free5gc -nn -c 20 udp port 2152` | Active packets (SIP/data) |

---

## Relevant 3GPP References

- **TS 38.413**: NGAP — Cause RadioNetwork[20] = user-inactivity (UEContextReleaseRequest)
- **TS 23.502**: PDU Session Establishment/Release procedures
- **TS 24.501**: NAS Registration procedures (Initial Registration vs. Mobility Registration Update)

---

**Created**: 2026-07-12
**For**: free5gc AMF development and UE connectivity troubleshooting
