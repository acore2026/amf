#!/bin/bash
#
# UE Keepalive Script for free5GC
#
# Prevents gNB RRC inactivity release (user-inactivity, ~200s) by
# periodically pinging UE IP addresses. Keeps the gNB data plane
# active so the UE stays in RRC_CONNECTED — no re-registration,
# no PDU session rebuild, no IP change.
#
# Usage:
#   ./ue-keepalive.sh --inet <IP> [--ims <IP>] [--interval <sec>] [--log <path>]
#
# Examples:
#   # Ping a single internet IP every 30s
#   ./ue-keepalive.sh --inet 10.60.0.200
#
#   # Ping both internet and IMS IPs every 30s
#   ./ue-keepalive.sh --inet 10.60.0.200 --ims 10.64.0.200
#
#   # Custom interval and log path
#   ./ue-keepalive.sh --inet 10.60.0.200 --ims 10.64.0.200 --interval 20 --log /var/log/ue-keepalive.log
#
# Run in background:
#   nohup ./ue-keepalive.sh --inet 10.60.0.200 --ims 10.64.0.200 &
#
# As systemd service: see UE_IP_STABILITY.md "systemd setup" section
#

set -euo pipefail

# Defaults
INET_IP=""
IMS_IP=""
INTERVAL=30
LOG_FILE="/var/log/ue-keepalive.log"
PING_COUNT=2
PING_TIMEOUT=2

usage() {
    cat <<'EOF'
Usage: ue-keepalive.sh --inet <IP> [--ims <IP>] [--interval <sec>] [--log <path>]

Required:
  --inet <IP>       UE internet PDU session IP (e.g. 10.60.0.200)

Optional:
  --ims <IP>        UE IMS PDU session IP (e.g. 10.64.0.200)
  --interval <sec>   Ping interval in seconds (default: 30)
  --log <path>       Log file path (default: /var/log/ue-keepalive.log)
  -h, --help         Show this help

The gNB RRC inactivity timer is typically ~200s. The default 30s interval
ensures data plane activity well within that window.
EOF
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --inet)     INET_IP="$2"; shift 2 ;;
        --ims)      IMS_IP="$2"; shift 2 ;;
        --interval) INTERVAL="$2"; shift 2 ;;
        --log)      LOG_FILE="$2"; shift 2 ;;
        -h|--help)  usage ;;
        *)          echo "Unknown option: $1"; usage ;;
    esac
done

if [[ -z "$INET_IP" && -z "$IMS_IP" ]]; then
    echo "Error: at least --inet <IP> is required"
    usage
fi

# Ensure log directory exists
mkdir -p "$(dirname "$LOG_FILE")" 2>/dev/null || true

# Header
{
    echo "======================================"
    echo " UE Keepalive Started"
    echo " Time     : $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    echo " Internet : ${INET_IP:-none}"
    echo " IMS      : ${IMS_IP:-none}"
    echo " Interval : ${INTERVAL}s"
    echo " Log      : $LOG_FILE"
    echo "======================================"
} | tee -a "$LOG_FILE"

# Main loop
while true; do
    TS=$(date -u '+%H:%M:%S')

    # Ping internet IP
    if [[ -n "$INET_IP" ]]; then
        OUT=$(ping -c "$PING_COUNT" -W "$PING_TIMEOUT" "$INET_IP" 2>&1 || true)
        LOSS=$(echo "$OUT" | grep -oE '[0-9]+% packet loss' | grep -oE '^[0-9]+' || echo "?")
        echo "[$TS] inet $INET_IP -> ${LOSS}% loss" | tee -a "$LOG_FILE" >/dev/null
    fi

    # Ping IMS IP
    if [[ -n "$IMS_IP" ]]; then
        OUT=$(ping -c "$PING_COUNT" -W "$PING_TIMEOUT" "$IMS_IP" 2>&1 || true)
        LOSS=$(echo "$OUT" | grep -oE '[0-9]+% packet loss' | grep -oE '^[0-9]+' || echo "?")
        echo "[$TS] ims  $IMS_IP -> ${LOSS}% loss" | tee -a "$LOG_FILE" >/dev/null
    fi

    sleep "$INTERVAL"
done
