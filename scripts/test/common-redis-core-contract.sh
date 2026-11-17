#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
: "${ADDP_REDIS_CORE_ENDPOINT:?owned Infra Redis endpoint required}"
: "${ADDP_REDIS_CORE_PASSWORD:?owned Infra Redis credential required}"
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/redis-core-contract.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT
for owner in common gateway system/backend; do
  case "$owner" in
    common) packages=(./infra/bootstrap ./modulelifecycle); required=(TestRequiredRedisAgainstRedis TestRedisReadinessAgainstRedis) ;;
    gateway) packages=(./internal/middleware); required=(TestConsumerRateLimitAgainstRedis) ;;
    system/backend) packages=(./internal/middleware); required=(TestOAuthRateLimitAgainstRedis) ;;
  esac
  (
    cd "$ROOT_DIR/$owner"
    GOWORK=off go test "${packages[@]}" -run 'AgainstRedis$' -count=1 -timeout=2m -v
  ) 2>&1 | tee "$work_dir/tests.log"
  if grep -q -- '--- SKIP:' "$work_dir/tests.log"; then
    echo 'Redis core gate refuses skipped tests' >&2; exit 1
  fi
  for name in "${required[@]}"; do
    grep -q -- "^--- PASS: $name " "$work_dir/tests.log" || { echo "Redis core gate did not execute $name" >&2; exit 1; }
  done
done
export ADDP_REDIS_CORE_REQUIRED=1
make -C "$ROOT_DIR" test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--package api --test oauth-client-credentials" 2>&1 | tee "$work_dir/iam.log"
grep -q -- 'PASS: TestIAMOAuthClientCredentialsAuthContextAgainstPostgres/Redis_core_outage_preserves_Token_and_grant_facts ' "$work_dir/iam.log" || {
  echo 'Redis core gate did not observe actual System Token/Grant invariants' >&2; exit 1;
}

# New acceptance preparation must prove the pinned identity and actual
# cross-process production Router wiring. This remains T2, never dispatches T4.
make -C "$ROOT_DIR" test-system-iam-postgres SYSTEM_IAM_POSTGRES_TEST_ARGS="--test redis-core-wiring" 2>&1 | tee "$work_dir/wiring.log"
grep -q -- 'Redis pinned identity and cross-process System/Gateway wiring: passed; product=690754af4890f493e5ec3fe560e7a5d12ba60c9d' "$work_dir/wiring.log" || {
  echo 'Redis core gate lacks pinned cross-process wiring evidence' >&2; exit 1;
}
