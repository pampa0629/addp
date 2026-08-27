#!/bin/bash
# Pull origin/main in a dedicated checkout and run the registered ADDP T0-T3 gates.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd -P)

fail() {
  echo "Local macOS CI failed: $*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: bash scripts/test/local-macos-ci.sh [--check-only|--full]

  no option     Fast-forward to origin/main and test changes since the last successful SHA.
                The first successful run is automatically a full run.
  --check-only  Validate the checkout, toolchain and Docker boundary without fetching or testing.
  --full        Fast-forward to origin/main and run all deterministic and PostgreSQL gates.
EOF
}

MODE=incremental
case "${1:-}" in
  "") ;;
  --check-only) MODE=check-only ;;
  --full) MODE=full ;;
  -h|--help)
    usage
    exit 0
    ;;
  *)
    usage >&2
    fail "unsupported argument: $1"
    ;;
esac
[ "$#" -le 1 ] || fail "only one option is supported"

cd "$ROOT_DIR"

git_common_dir=$(git rev-parse --git-common-dir 2>/dev/null) ||
  fail "$ROOT_DIR is not a Git checkout"
case "$git_common_dir" in
  /*) ;;
  *) git_common_dir="$ROOT_DIR/$git_common_dir" ;;
esac
git_common_dir=$(cd "$git_common_dir" && pwd -P)
STATE_DIR="$git_common_dir/addp-local-ci"
LOG_DIR="$STATE_DIR/logs"
LOCK_DIR="$STATE_DIR/run.lock"
LOCK_OWNER="$LOCK_DIR/owner"
LAST_SUCCESS_FILE="$STATE_DIR/last-success-sha"
LATEST_SUMMARY="$STATE_DIR/latest-summary.txt"

mkdir -p "$LOG_DIR"

read_lock_pid() {
  sed -n 's/^pid=//p' "$LOCK_OWNER" 2>/dev/null | head -n 1
}

release_lock() {
  local status=$?
  trap - EXIT INT TERM
  if [ -d "$LOCK_DIR" ] && [ "$(read_lock_pid)" = "$$" ]; then
    rm -rf "$LOCK_DIR"
  fi
  return "$status"
}

acquire_lock() {
  if ! mkdir "$LOCK_DIR" 2>/dev/null; then
    local holder_pid
    holder_pid=$(read_lock_pid)
    if [ -n "$holder_pid" ] && ps -p "$holder_pid" >/dev/null 2>&1; then
      fail "another local CI run is active with PID $holder_pid"
    fi
    fail "stale local CI lock found at $LOCK_DIR; inspect it before removing it"
  fi
  {
    printf 'pid=%s\n' "$$"
    printf 'started_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'workspace=%s\n' "$ROOT_DIR"
  } > "$LOCK_OWNER"
  trap release_lock EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command is missing: $1"
}

validate_python() {
  python3 - <<'PY' || exit 1
import sys
if sys.version_info < (3, 11):
    raise SystemExit(f"Python 3.11+ is required, found {sys.version.split()[0]}")
PY
}

validate_node() {
  local required major
  [ -f "$ROOT_DIR/.node-version" ] || fail "Node.js version file is missing: $ROOT_DIR/.node-version"
  required=$(sed -n '1p' "$ROOT_DIR/.node-version")
  case "$required" in
    ''|*[!0-9]*) fail "invalid Node.js major version in $ROOT_DIR/.node-version: ${required:-empty}" ;;
  esac
  major=$(node --version | sed 's/^v//' | cut -d. -f1)
  [ "$major" = "$required" ] || fail "Node.js $required is required, found $(node --version)"
}

validate_go() {
  local version major minor
  version=$(GOTOOLCHAIN=local go env GOVERSION 2>/dev/null | sed 's/^go//')
  major=$(printf '%s' "$version" | cut -d. -f1)
  minor=$(printf '%s' "$version" | cut -d. -f2)
  [ "$major" -gt 1 ] 2>/dev/null ||
    { [ "$major" = "1" ] && [ "$minor" -ge 24 ] 2>/dev/null; } ||
    fail "Go 1.24+ is required, found ${version:-unknown}"
}

tracked_or_untracked_changes() {
  git status --porcelain --untracked-files=all
}

running_addp_infra() {
  docker ps --format '{{.Names}}' | while IFS= read -r name; do
    case "$name" in
      addp-postgres|addp-redis|addp-minio|addp-meilisearch|addp-redpanda|addp-redpanda-init|addp-kafka-connect)
        printf '%s\n' "$name"
        ;;
    esac
  done
}

validate_host() {
  local active_infra
  [ "$(uname -s)" = "Darwin" ] || fail "this auxiliary CI entry requires macOS"
  for command in bash git python3 make go node npm docker curl; do
    require_command "$command"
  done
  validate_python
  validate_node
  validate_go
  docker info >/dev/null 2>&1 || fail "Docker is not running or is not accessible"
  active_infra=$(running_addp_infra)
  [ -z "$active_infra" ] || fail "running ADDP Infra belongs to another session: $active_infra"
  [ "$(git branch --show-current)" = "main" ] || fail "dedicated checkout must be on main"
  [ ! -e "$ROOT_DIR/.env" ] || fail "dedicated checkout must not contain a repository-root .env"
  [ -z "$(tracked_or_untracked_changes)" ] ||
    fail "dedicated checkout is not clean; refusing to overwrite local work"
  git remote get-url origin >/dev/null 2>&1 || fail "origin remote is missing"
}

clear_postgres_gate_environment() {
  unset ADDP_SYSTEM_POSTGRES_TEST_DSN
  unset META_POSTGRES_TEST_DSN
  unset CATALOG_POSTGRES_TEST_DSN
  unset ADDP_TEST_MODEL_POSTGRES_DSN
  unset SERVICE_POSTGRES_TEST_DSN
  unset STANDARD_POSTGRES_TEST_DSN
  unset ADDP_POSTGRES_INTEGRATION
  unset ADDP_TEST_POSTGRES_HOST
  unset ADDP_TEST_POSTGRES_PORT
  unset ADDP_TEST_POSTGRES_USER
  unset ADDP_TEST_POSTGRES_PASSWORD
  unset ADDP_TEST_POSTGRES_DATABASE
  unset ADDP_TEST_POSTGRES_SSLMODE
}

file_fingerprint() {
  local runtime=$1
  shift
  {
    printf '%s\n' "$runtime"
    for path in "$@"; do
      [ -f "$path" ] || fail "dependency manifest is missing: $path"
      shasum -a 256 "$path"
    done
  } | shasum -a 256 | awk '{print $1}'
}

prepare_frontends() {
  local lockfile directory fingerprint state_file recorded
  git ls-files -- '*/frontend/package-lock.json' | while IFS= read -r lockfile; do
    [ -n "$lockfile" ] || continue
    directory=${lockfile%/package-lock.json}
    fingerprint=$(file_fingerprint "$(node --version)" "$lockfile")
    state_file="$STATE_DIR/frontend-$(printf '%s' "$directory" | tr '/' '_').sha256"
    recorded=$(sed -n '1p' "$state_file" 2>/dev/null || true)
    if [ ! -d "$directory/node_modules" ] || [ "$recorded" != "$fingerprint" ]; then
      echo "==> Prepare frontend dependencies: $directory"
      (cd "$directory" && npm ci)
      printf '%s\n' "$fingerprint" > "$state_file"
    fi
  done
}

prepare_python_venv() {
  local name=$1
  local venv=$2
  local requirements=$3
  local editable=$4
  shift 4
  local fingerprint state_file recorded python
  fingerprint=$(file_fingerprint "$(python3 --version 2>&1)" "$@")
  state_file="$STATE_DIR/python-${name}.sha256"
  recorded=$(sed -n '1p' "$state_file" 2>/dev/null || true)
  python="$venv/bin/python"
  if [ ! -x "$python" ] || [ "$recorded" != "$fingerprint" ]; then
    echo "==> Prepare Python environment: $name"
    rm -rf "$venv"
    python3 -m venv "$venv"
    "$python" -m pip install --disable-pip-version-check --upgrade pip
    if [ -n "$requirements" ]; then
      local requirements_dir requirements_name
      requirements_dir=${requirements%/*}
      requirements_name=${requirements##*/}
      (cd "$requirements_dir" && "$ROOT_DIR/$python" -m pip install --requirement "$requirements_name")
    fi
    if [ -n "$editable" ]; then
      "$python" -m pip install --editable "$editable"
    fi
    printf '%s\n' "$fingerprint" > "$state_file"
  fi
}

prepare_dependencies() {
  prepare_frontends
  prepare_python_venv \
    common-python common-python/.venv "" './common-python[dev]' \
    common-python/pyproject.toml
  prepare_python_venv \
    agent agent/backend/venv agent/backend/requirements.txt "" \
    agent/backend/requirements.txt common-python/pyproject.toml
  prepare_python_venv \
    copilot copilot/backend/venv copilot/backend/requirements.txt './common-python[dev,inference-langchain]' \
    copilot/backend/requirements.txt common-python/pyproject.toml
}

run_postgres_gates() {
  local shared_dsn iam_dsn
  shared_dsn='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable'
  iam_dsn='postgres://addp:addp_password@127.0.0.1:15432/addp_iam_test?sslmode=disable'
  env \
    ADDP_SYSTEM_POSTGRES_TEST_DSN="$iam_dsn" \
    META_POSTGRES_TEST_DSN="$shared_dsn" \
    CATALOG_POSTGRES_TEST_DSN="$shared_dsn" \
    ADDP_TEST_MODEL_POSTGRES_DSN="$shared_dsn" \
    SERVICE_POSTGRES_TEST_DSN="$shared_dsn" \
    STANDARD_POSTGRES_TEST_DSN="$shared_dsn" \
    ADDP_TEST_POSTGRES_HOST=127.0.0.1 \
    ADDP_TEST_POSTGRES_PORT=15432 \
    ADDP_TEST_POSTGRES_USER=addp \
    ADDP_TEST_POSTGRES_PASSWORD=addp_password \
    ADDP_TEST_POSTGRES_DATABASE=addp_test \
    ADDP_TEST_POSTGRES_SSLMODE=disable \
    "$@"
}

run_infra() {
  env \
    POSTGRES_USER=addp \
    POSTGRES_PASSWORD=addp_password \
    POSTGRES_DB=addp \
    POSTGRES_HOST=127.0.0.1 \
    POSTGRES_PORT=15432 \
    "$@"
}

write_summary() {
  local result=$1
  local sha=$2
  local scope=$3
  local log_file=$4
  local temporary="$LATEST_SUMMARY.tmp.$$"
  {
    printf 'schema_version=addp.local-ci-summary/v1\n'
    printf 'result=%s\n' "$result"
    printf 'sha=%s\n' "$sha"
    printf 'scope=%s\n' "$scope"
    printf 'finished_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'log=%s\n' "$log_file"
  } > "$temporary"
  mv "$temporary" "$LATEST_SUMMARY"
}

validate_host
clear_postgres_gate_environment
if [ "$MODE" = "check-only" ]; then
  echo "Local macOS CI readiness check passed: $ROOT_DIR"
  exit 0
fi

acquire_lock

echo "==> Fetch origin/main"
git fetch origin main
git merge --ff-only refs/remotes/origin/main
[ "$(git rev-parse HEAD)" = "$(git rev-parse refs/remotes/origin/main)" ] ||
  fail "local main does not exactly match origin/main"
[ -z "$(tracked_or_untracked_changes)" ] || fail "checkout became dirty after fast-forward"

target_sha=$(git rev-parse HEAD)
last_success=$(sed -n '1p' "$LAST_SUCCESS_FILE" 2>/dev/null || true)
if [ "$MODE" = "incremental" ] && [ -n "$last_success" ] && [ "$last_success" = "$target_sha" ]; then
  echo "Local macOS CI is already successful for $target_sha"
  exit 0
fi

scope=$MODE
if [ -z "$last_success" ] || ! git merge-base --is-ancestor "$last_success" "$target_sha"; then
  scope=full
fi

timestamp=$(date -u '+%Y%m%dT%H%M%SZ')
short_sha=$(git rev-parse --short=12 "$target_sha")
log_file="$LOG_DIR/${timestamp}-${short_sha}-${scope}.log"
exec > >(tee -a "$log_file") 2>&1

infra_started=0
finish_run() {
  local status=$?
  trap - EXIT INT TERM
  if [ "$infra_started" -eq 1 ]; then
    echo "==> Stop ADDP CI infrastructure"
    if ! run_infra make infra-down; then
      status=1
    fi
  fi
  if [ "$status" -eq 0 ]; then
    if [ -n "$(tracked_or_untracked_changes)" ]; then
      echo "Local macOS CI failed: tests modified the dedicated checkout" >&2
      status=1
    else
      printf '%s\n' "$target_sha" > "$LAST_SUCCESS_FILE"
    fi
  fi
  if [ "$status" -eq 0 ]; then
    write_summary passed "$target_sha" "$scope" "$log_file"
    echo "Local macOS CI passed: $target_sha ($scope)"
  else
    write_summary failed "$target_sha" "$scope" "$log_file"
    echo "Local macOS CI failed: $target_sha ($scope)" >&2
  fi
  find "$LOG_DIR" -type f -name '*.log' -mtime +13 -delete 2>/dev/null || true
  release_lock
  exit "$status"
}
trap finish_run EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "Local macOS CI start: sha=$target_sha scope=$scope"
prepare_dependencies

if [ "$scope" = "full" ]; then
  echo "==> Run all deterministic T0-T3 gates"
  make test
else
  echo "==> Run changed T0-T3 gates since $last_success"
fi

echo "==> Compile all Linux product binaries"
make build BUILD_ARGS=--force

echo "==> Start disposable ADDP test infrastructure"
infra_started=1
run_infra make infra-up

if [ "$scope" = "full" ]; then
  echo "==> Run all registered PostgreSQL integration gates"
  run_postgres_gates make test-integration
else
  run_postgres_gates make test-changed "BASE_REF=$last_success"
fi
