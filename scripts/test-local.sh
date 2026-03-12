#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CACHE_DIR="${ROOT_DIR}/.cache"

mkdir -p "${CACHE_DIR}/gocache"

if [[ -n "${GOFLAGS:-}" ]]; then
  export GOFLAGS="${GOFLAGS} -buildvcs=false"
else
  export GOFLAGS="-buildvcs=false"
fi
export GOCACHE="${CACHE_DIR}/gocache"

cd "${ROOT_DIR}"
if [[ "$#" -gt 0 ]]; then
  exec go test "$@"
fi

exec go test ./...
