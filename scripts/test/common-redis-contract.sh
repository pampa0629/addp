#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-redis-contract.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT
export ADDP_REDIS_INTEGRATION=1
[[ $# == 0 || ( $# == 1 && "$1" == --monitor-only ) ]] || { echo 'Redis T2: unsupported scope' >&2; exit 2; }
specifications=('common engine/plugins/redis' 'system/backend internal/service' 'meta/backend internal/scanruntime' 'manager/backend internal/preview')
if [[ $# == 1 ]]; then specifications=('monitor/backend internal/redissource'); fi
for specification in "${specifications[@]}"; do
    read -r owner package <<< "$specification"
    (cd "$ROOT_DIR/$owner" && GOWORK=off go test "./$package" -run '^TestIntegrationRedis' -count=1 -v) 2>&1 | tee "$WORK_DIR/${owner//\//-}.log"
    if grep -q -- '--- SKIP:' "$WORK_DIR/${owner//\//-}.log"; then echo "Redis T2 refuses skipped tests" >&2; exit 1; fi
    grep -q -- '--- PASS: TestIntegrationRedis' "$WORK_DIR/${owner//\//-}.log" || { echo "Redis T2 requires real consumer tests" >&2; exit 1; }
done
if [[ $# == 1 ]]; then
    python3 "$ROOT_DIR/scripts/test/redis-metrics-lifecycle-probe.py"
fi
