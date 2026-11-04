#!/usr/bin/env bash
# Owned by monitor-metrics-gate.sh --case=node-recovery and its CI matrix.
# Only a disposable bridge source: no host root/proc/sys mounts or host namespaces.
set -euo pipefail
[[ $# == 0 ]] || { echo 'Node recovery T2: unexpected arguments' >&2; exit 2; }
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
make -C "$ROOT_DIR" test-node-metrics-recovery
python3 "$ROOT_DIR/scripts/test/node-metrics-recovery-probe.py"
