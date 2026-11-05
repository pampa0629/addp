#!/usr/bin/env bash
# PG-only source authorization acceptance on an isolated Hosted deployment.
# ADDP_ONLINE_SUITES=pg-source-authorization
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=pg-source-authorization
HOSTED_FIXTURE_CONTAINERS=(addp-metric-online-disposable)
HOSTED_FIXTURE_IMAGES=()
stop_online_fixture() {
  run_logged bash business/scripts/online-metric-postgres-fixture.sh stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"

cpu_arm=${ADDP_ONLINE_PG_CPU_ARM:-}
case "$cpu_arm" in
  '') ;;
  3|4) [ "$(cat /sys/devices/system/cpu/online)" = 0-3 ] ||
    fail 'PG CPU comparison requires exactly four online vCPUs' ;;
  *) fail 'PG CPU arm must be 3 or 4' ;;
esac

# On the disposable VM, change CPU availability for native services, capture
# processes and Docker together. Keep provisioning and cleanup at full CPU.
run_pg_cpu_arm() (
  local cpu_dir=/sys/devices/system/cpu
  local expected=0-3
  local report="$ADDP_ONLINE_ARTIFACT_DIR/cpu-arm.txt"
  local before actual
  before=$(cat "$cpu_dir/online")
  [ "$before" = 0-3 ] || fail 'PG CPU availability changed before the suite'
  printf 'requested_vcpus=%s\nbefore=%s\n' "$cpu_arm" "$before" > "$report"

  restore_cpu() {
    local status=$? after
    trap - EXIT INT TERM
    if [ "$cpu_arm" = 3 ] && [ "$(cat "$cpu_dir/online")" != "$before" ]; then
      printf '1\n' | sudo -n tee "$cpu_dir/cpu3/online" >/dev/null || status=1
    fi
    after=$(cat "$cpu_dir/online") || after=unavailable
    printf 'after=%s\n' "$after" >> "$report"
    if [ "$after" != "$before" ]; then
      echo 'PG CPU availability was not restored' >&2
      status=1
    fi
    exit "$status"
  }
  trap restore_cpu EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM

  if [ "$cpu_arm" = 3 ]; then
    expected=0-2
    printf '0\n' | sudo -n tee "$cpu_dir/cpu3/online" >/dev/null
  fi
  actual=$(cat "$cpu_dir/online")
  printf 'during_start=%s\n' "$actual" >> "$report"
  [ "$actual" = "$expected" ] || fail 'PG CPU arm did not take effect'
  local suite_status=0
  run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE" || suite_status=$?
  actual=$(cat "$cpu_dir/online")
  printf 'during_end=%s\n' "$actual" >> "$report"
  [ "$actual" = "$expected" ] || fail 'PG CPU availability changed during the suite'
  exit "$suite_status"
)

export CONSOLE_URL=http://127.0.0.1:5170
export CATALOG_URL=http://127.0.0.1:8192
export SECURITY_URL=http://127.0.0.1:8194
export TRANSFER_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export META_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export CATALOG_SERVICE_CLIENT_SECRET="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$ADDP_ONLINE_SECRET_DIR/pg-authorization-engine.json"
infra_owned=1
run_logged bash scripts/infra/up.sh
# Observation only, on the fresh disposable Infra admitted by hosted-online.sh.
# Do not alter durability, audit persistence, permissions or application code.
run_logged docker exec addp-postgres psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d addp_online \
  -c "ALTER SYSTEM SET track_io_timing = 'on'" \
  -c "ALTER SYSTEM SET track_wal_io_timing = 'on'" \
  -c 'SELECT pg_reload_conf()'
fixture_owned=1
run_logged bash business/scripts/online-metric-postgres-fixture.sh start
application_owned=1
export ADDP_ONLINE_PG_SOURCE_PREFIX="pg_auth_$(python3 -c 'import uuid; print(uuid.uuid4().hex[:16])')"
# Start shared dependencies separately so a module's PGAPPNAME never labels
# another module's source connections. All launches use the standard lifecycle.
for start_target in -system -gateway; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 PGAPPNAME="${ADDP_ONLINE_PG_SOURCE_PREFIX}_${start_target#-}" \
    bash scripts/dev/start.sh --exact-process "$start_target"
done
# Manager requires an initialized Security projection before any source read.
for start_target in -meta -security -transfer -manager -catalog -system; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 PGAPPNAME="${ADDP_ONLINE_PG_SOURCE_PREFIX}_${start_target#-}" \
    bash scripts/dev/start.sh "$start_target"
done
run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite pg-source-authorization --output "$1"' _ "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
# Reuse the existing registration User only for one exact source-statistics control.
export ADDP_ONLINE_PG_PROBE_ACCESS_TOKEN="$ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN"
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN
export ADDP_ONLINE_CONSUMER_ENGINE_ID
if [ -n "$cpu_arm" ]; then
  run_pg_cpu_arm
else
  run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
fi
