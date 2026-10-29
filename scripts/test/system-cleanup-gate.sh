#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN
# Hosted CI: addp_test or *disposable* database; local shared: addp_test only.
set -euo pipefail

if [ "$#" -ne 0 ]; then
    echo "usage: $0 (configure ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN)" >&2
    exit 2
fi
: "${ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN:?Set ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN to the verified Cleanup test database}"

# Validate the database policy without printing credentials. The shared Infra
# helper below separately proves the actual local container and mapped port.
python3 -c '
import ipaddress, os, re, sys
from urllib.parse import parse_qsl, unquote, urlsplit
try:
    value = urlsplit(sys.stdin.readline().rstrip("\n"))
    database = unquote(value.path.lstrip("/"))
    if (value.scheme not in ("postgres", "postgresql") or not value.hostname
            or not value.port or not 1 <= value.port <= 65535 or value.fragment
            or not database or any(c in database for c in "/\r\n")):
        raise ValueError()
    if any(k.lower() in {"host", "hostaddr", "port", "dbname", "database"}
           for k, _ in parse_qsl(value.query)):
        raise ValueError()
    disposable = "disposable" in re.split(r"[^a-z0-9]+", database.lower())
    isolated = os.environ.get("GITHUB_ACTIONS") == "true" or os.environ.get("ADDP_LOCAL_CI_POSTGRES") == "1"
    if isolated:
        if database != "addp_test" and not disposable:
            raise ValueError()
    else:
        host = value.hostname.lower()
        loopback = host in {"localhost", "localhost."}
        if not loopback:
            try:
                loopback = ipaddress.ip_address(host).is_loopback
            except ValueError:
                pass
        if database != "addp_test" or not loopback:
            raise ValueError()
except ValueError:
    sys.exit("Cleanup PostgreSQL gate requires an explicit-port URL to local addp_test or a CI-owned disposable database")
' <<< "$ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN"

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_dsn "$ADDP_SYSTEM_CLEANUP_POSTGRES_TEST_DSN"

# Cleanup and Common/Graph touch the same execution tables in addp_test.
# Retain the same inode and open descriptor through all Go child processes.
exec 9>>/tmp/addp-common-postgres-gate.lock
python3 - <<'PY'
import fcntl
import sys
try:
    fcntl.flock(9, fcntl.LOCK_EX | fcntl.LOCK_NB)
except BlockingIOError:
    sys.exit("Common/Graph/Cleanup PostgreSQL gate is already running; run gates serially")
PY

WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-system-cleanup.XXXXXX")
trap 'rm -rf "$WORK_DIR"' EXIT
cd "$ROOT_DIR/system/backend"
# Owner tests own minimal initialization, exact-UUID cleanup and zero-residue
# assertions. This runner never resets IAM, Common or System schemas.
GOWORK=off go test ./internal/service -run '^TestIntegrationPostgresCleanup' \
    -count=1 -timeout=3m -v 2>&1 | tee "$WORK_DIR/cleanup.log"
if grep -q -- '--- SKIP:' "$WORK_DIR/cleanup.log"; then
    echo "Cleanup PostgreSQL gate refuses skipped tests" >&2
    exit 1
fi
for test_name in \
    TestIntegrationPostgresCleanupAcceptanceAtomicity \
    TestIntegrationPostgresCleanupIdempotence \
    TestIntegrationPostgresCleanupCoordinatorRecovery \
    TestIntegrationPostgresCleanupAdmissionRace \
    TestIntegrationPostgresCleanupReadOnlySnapshot; do
    grep -q -- "^--- PASS: $test_name " "$WORK_DIR/cleanup.log" || {
        echo "Cleanup PostgreSQL gate requires the real $test_name test" >&2
        exit 1
    }
done
echo "System Cleanup PostgreSQL admission/coordinator/read-only contracts passed (not IAM policy or Hosted validation)"
