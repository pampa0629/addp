#!/usr/bin/env bash
# Disposable Security masking and owner protection acceptance on GitHub Hosted.
# ADDP_ONLINE_SUITES=security-mysql-owner-protection
# ADDP_ONLINE_SUITES=security-plaintext-access
# ADDP_ONLINE_RUNNER=github-hosted-linux-x86_64
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd -P)
ONLINE_SUITE=${ONLINE_SUITE_INPUT:-}
case "$ONLINE_SUITE" in
  security-mysql-owner-protection) INITIALIZER_SCRIPT=security-mysql-owner-protection-online.py ;;
  security-plaintext-access) INITIALIZER_SCRIPT=security-plaintext-access-online.py ;;
  *) echo 'Unsupported Hosted Security suite' >&2; exit 1 ;;
esac
HOSTED_FIXTURE_CONTAINERS=(addp-security-online-postgres addp-security-online-mysql addp-security-online-mongodb)
stop_online_fixture() {
  run_logged bash business/scripts/online-security-owner-fixture.sh stop
}
source "$ROOT_DIR/scripts/utils/hosted-online.sh"
export SECURITY_URL=http://127.0.0.1:8194
export MONITOR_URL=http://127.0.0.1:8100 CONSOLE_URL=http://127.0.0.1:5170
export ADDP_ONLINE_WORKBENCH_MYSQL_DATABASE=security_fixture
# Swagger generation requires an explicit workspace in a clean checkout.
# Discover the same tracked modules as test-go; the Hosted exit trap owns it.
export GOWORK="$ADDP_ONLINE_SECRET_DIR/go.work"
export GOMODCACHE="$ROOT_DIR/.gomodcache"
cp "$ROOT_DIR/go.work.sum" "${GOWORK}.sum"
go_modules=()
while IFS= read -r module; do
  go_modules+=("$ROOT_DIR/$(dirname "$module")")
done < <(git ls-files -- 'go.mod' '**/go.mod')
[ "${#go_modules[@]}" -gt 0 ] || fail 'Security Hosted checkout has no tracked Go modules'
run_logged go work init "${go_modules[@]}"
infra_owned=1
run_logged bash scripts/infra/up.sh
fixture_owned=1
run_logged bash business/scripts/online-security-owner-fixture.sh start
application_owned=1
for start_target in -meta -security -manager -develop -service -transfer -monitor; do
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$start_target"
done
if [ "$ONLINE_SUITE" = security-mysql-owner-protection ]; then
  run_logged npm --prefix console/frontend exec -- playwright install --with-deps chromium
fi
run_logged bash -c 'cd system/backend && go run ./cmd/online-test-fixture --suite "$1" --output "$2"' _ "$ONLINE_SUITE" "$IDENTITY_ENV"
source "$IDENTITY_ENV"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_SECRET_DIR/security-postgres.json" --output "$ENGINE_RESULT_ENV"
source "$ENGINE_RESULT_ENV"
export ADDP_ONLINE_TEST_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
run_logged python3 scripts/test/online-engine-registration.py \
  --descriptor "$ADDP_ONLINE_SECRET_DIR/security-mysql.json" --output "$ADDP_ONLINE_SECRET_DIR/mysql-engine.env"
source "$ADDP_ONLINE_SECRET_DIR/mysql-engine.env"
export ADDP_ONLINE_WORKBENCH_MYSQL_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
if [ "$ONLINE_SUITE" = security-mysql-owner-protection ]; then
  run_logged python3 scripts/test/online-engine-registration.py \
    --descriptor "$ADDP_ONLINE_SECRET_DIR/security-mongodb.json" --output "$ADDP_ONLINE_SECRET_DIR/mongodb-engine.env"
  source "$ADDP_ONLINE_SECRET_DIR/mongodb-engine.env"
  export ADDP_ONLINE_SECURITY_MONGODB_ENGINE_ID="$ADDP_ONLINE_CONSUMER_ENGINE_ID"
fi
run_logged python3 "scripts/test/$INITIALIZER_SCRIPT" --initialize
unset ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN
run_logged make test-online "ONLINE_SUITE=$ONLINE_SUITE"
