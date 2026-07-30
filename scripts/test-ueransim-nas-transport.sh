#!/usr/bin/env bash
set -Eeuo pipefail

UERANSIM_DIR="${UERANSIM_DIR:-/root/zqm/UERANSIM}"
BUILD_DIR="${BUILD_DIR:-${UERANSIM_DIR}/cmake-build-release}"
GNB_BIN="${GNB_BIN:-${BUILD_DIR}/nr-gnb}"
UE_BIN="${UE_BIN:-${BUILD_DIR}/nr-ue}"
GNB_CONFIG="${GNB_CONFIG:-${UERANSIM_DIR}/config/free5gc-gnb.yaml}"
UE_CONFIG="${UE_CONFIG:-${UERANSIM_DIR}/config/free5gc-ue.yaml}"
AMF_CONFIG="${AMF_CONFIG:-/root/zqm/NewNAS/amf/amfcfg.yaml}"

AMF_NGAP_PORT="${AMF_NGAP_PORT:-38412}"
PAYLOAD_CONTAINER_TYPE="${PAYLOAD_CONTAINER_TYPE:-4}"
PAYLOAD="${PAYLOAD:-}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-75}"
STARTUP_DELAY_SECONDS="${STARTUP_DELAY_SECONDS:-2}"
KEEP_LOGS="${KEEP_LOGS:-0}"
SKIP_AMF_CHECK="${SKIP_AMF_CHECK:-0}"

tmpdir=""
gnb_pid=""
ue_pid=""

die() {
  echo "ERROR: $*" >&2
  exit 1
}

info() {
  echo "[test] $*"
}

cleanup() {
  local status=$?
  if [[ -n "${ue_pid}" ]] && kill -0 "${ue_pid}" 2>/dev/null; then
    kill "${ue_pid}" 2>/dev/null || true
    wait "${ue_pid}" 2>/dev/null || true
  fi
  if [[ -n "${gnb_pid}" ]] && kill -0 "${gnb_pid}" 2>/dev/null; then
    kill "${gnb_pid}" 2>/dev/null || true
    wait "${gnb_pid}" 2>/dev/null || true
  fi
  if [[ -n "${tmpdir}" && "${KEEP_LOGS}" != "1" ]]; then
    rm -rf "${tmpdir}"
  elif [[ -n "${tmpdir}" ]]; then
    info "kept logs/configs at ${tmpdir}"
  fi
  exit "${status}"
}
trap cleanup EXIT

require_file() {
  [[ -f "$1" ]] || die "missing file: $1"
}

require_executable() {
  [[ -x "$1" ]] || die "missing executable: $1"
}

check_amf_listening() {
  if [[ "${SKIP_AMF_CHECK}" == "1" ]]; then
    info "skipping AMF SCTP listen check"
    return
  fi
  if ! command -v ss >/dev/null 2>&1; then
    info "ss is unavailable; skipping AMF SCTP listen check"
    return
  fi
  if ! ss -H -l -A sctp 2>/dev/null | awk -v port=":${AMF_NGAP_PORT}" '$0 ~ port { found = 1 } END { exit !found }'; then
    die "AMF SCTP port ${AMF_NGAP_PORT} is not listening. Start AMF first, or set SKIP_AMF_CHECK=1."
  fi
}

warn_amf_config() {
  if [[ ! -f "${AMF_CONFIG}" ]]; then
    info "AMF config not found at ${AMF_CONFIG}; skipping config hint"
    return
  fi
  if ! grep -q "transportPassthrough:" "${AMF_CONFIG}" ||
    ! grep -q "payloadContainerType:[[:space:]]*${PAYLOAD_CONTAINER_TYPE}" "${AMF_CONFIG}"; then
    info "AMF config hint: ensure nagent.transportPassthrough.enabled=true and payloadContainerType=${PAYLOAD_CONTAINER_TYPE}"
  fi
}

patch_ue_config() {
  local input="$1"
  local output="$2"
  local payload_file="$3"
  cp "${input}" "${output}"
  python3 - "${output}" "${PAYLOAD_CONTAINER_TYPE}" "${payload_file}" <<'PY'
import re
import sys
from pathlib import Path

path = Path(sys.argv[1])
payload_container_type = sys.argv[2]
payload = Path(sys.argv[3]).read_text()
text = path.read_text()

def replace_or_append_block(text, name, body):
    pattern = re.compile(rf"(?m)^{re.escape(name)}:\n(?:^[ \t]+.*\n?)*")
    block = f"{name}:\n{body}"
    if pattern.search(text):
        return pattern.sub(block, text, count=1)
    if not text.endswith("\n"):
        text += "\n"
    return text + "\n" + block

def force_nested_enabled_false(text, name):
    pattern = re.compile(rf"(?m)^({re.escape(name)}:\n)((?:^[ \t]+.*\n?)*)")
    match = pattern.search(text)
    if not match:
        return text
    body = match.group(2)
    if re.search(r"(?m)^[ \t]+enabled:", body):
        body = re.sub(r"(?m)^([ \t]+enabled:).*$", r"\1 false", body, count=1)
    else:
        body = "  enabled: false\n" + body
    return text[:match.start()] + match.group(1) + body + text[match.end():]

text = force_nested_enabled_false(text, "cooperationTest")
payload_value = payload.replace("\\", "\\\\").replace("'", "''")
body = (
    "  enabled: true\n"
    f"  payloadContainerType: {payload_container_type}\n"
    f"  payload: '{payload_value}'\n"
)
text = replace_or_append_block(text, "nasTransportTest", body)
path.write_text(text)
PY
}

tail_logs() {
  local gnb_log="$1"
  local ue_log="$2"
  echo
  echo "----- nr-gnb log tail -----"
  tail -80 "${gnb_log}" || true
  echo
  echo "----- nr-ue log tail -----"
  tail -120 "${ue_log}" || true
}

require_executable "${GNB_BIN}"
require_executable "${UE_BIN}"
require_file "${GNB_CONFIG}"
require_file "${UE_CONFIG}"
check_amf_listening
warn_amf_config

tmpdir="$(mktemp -d /tmp/ueransim-nas-transport.XXXXXX)"
gnb_cfg="${tmpdir}/free5gc-gnb.yaml"
ue_cfg="${tmpdir}/free5gc-ue.yaml"
payload_file="${tmpdir}/payload.txt"
gnb_log="${tmpdir}/nr-gnb.log"
ue_log="${tmpdir}/nr-ue.log"

printf "%s" "${PAYLOAD}" >"${payload_file}"
cp "${GNB_CONFIG}" "${gnb_cfg}"
patch_ue_config "${UE_CONFIG}" "${ue_cfg}" "${payload_file}"

info "temporary configs/logs: ${tmpdir}"
info "starting nr-gnb"
"${GNB_BIN}" -c "${gnb_cfg}" >"${gnb_log}" 2>&1 &
gnb_pid=$!
sleep "${STARTUP_DELAY_SECONDS}"
if ! kill -0 "${gnb_pid}" 2>/dev/null; then
  tail_logs "${gnb_log}" "${ue_log}"
  die "nr-gnb exited early"
fi

info "starting nr-ue with NAS Transport type=${PAYLOAD_CONTAINER_TYPE}"
"${UE_BIN}" -c "${ue_cfg}" >"${ue_log}" 2>&1 &
ue_pid=$!

deadline=$((SECONDS + TIMEOUT_SECONDS))
sent_seen=0
dl_seen=0
registered_seen=0
while (( SECONDS < deadline )); do
  if grep -q "Sending Registration Complete" "${ue_log}" 2>/dev/null ||
    grep -q "UE switches to state \\[MM-REGISTERED" "${ue_log}" 2>/dev/null; then
    registered_seen=1
  fi
  if grep -q "Sending UL NAS Transport test NAS" "${ue_log}" 2>/dev/null; then
    sent_seen=1
  fi
  if grep -q "DL NAS Transport type=4 received" "${ue_log}" 2>/dev/null; then
    dl_seen=1
  fi
  if (( sent_seen == 1 && dl_seen == 1 )); then
    info "PASS: UE sent UL NAS Transport type=${PAYLOAD_CONTAINER_TYPE} and received DL NAS Transport type=4 response"
    tail_logs "${gnb_log}" "${ue_log}"
    exit 0
  fi
  if [[ -n "${ue_pid}" ]] && ! kill -0 "${ue_pid}" 2>/dev/null; then
    tail_logs "${gnb_log}" "${ue_log}"
    die "nr-ue exited before receiving DL NAS Transport type=4"
  fi
  if [[ -n "${gnb_pid}" ]] && ! kill -0 "${gnb_pid}" 2>/dev/null; then
    tail_logs "${gnb_log}" "${ue_log}"
    die "nr-gnb exited before test completed"
  fi
  sleep 1
done

tail_logs "${gnb_log}" "${ue_log}"
die "timeout after ${TIMEOUT_SECONDS}s: registered_seen=${registered_seen}, sent_seen=${sent_seen}, dl_seen=${dl_seen}"
