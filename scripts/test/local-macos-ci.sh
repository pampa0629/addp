#!/bin/bash
# Pull origin/main in a dedicated checkout and run the registered ADDP T0-T3 gates.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd -P)
LOCAL_CI_COMPOSE_FILE="$SCRIPT_DIR/docker-compose.local-macos-ci.yml"
MYSQL_CONTAINER_NAME=addp-local-ci-mysql
OCEANBASE_CONTAINER_NAME=addp-local-ci-oceanbase

fail() {
  echo "Local macOS CI failed: $*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: bash scripts/test/local-macos-ci.sh [--full] [--no-fetch]
       bash scripts/test/local-macos-ci.sh --check-only

  no option     Fast-forward to origin/main and test changes since the last successful SHA.
                The first successful run is automatically a full run.
  --check-only  Validate the checkout, toolchain and Docker boundary without fetching or testing.
  --full        Run all deterministic and infrastructure gates instead of the incremental scope.
  --no-fetch    Run against the current clean main checkout without fetching or merging.
                Combine with --full for a complete run without remote synchronization.
EOF
}

MODE=incremental
FETCH_REMOTE=true
check_only_requested=false
full_requested=false
no_fetch_requested=false
for argument in "$@"; do
  case "$argument" in
    --check-only)
      [ "$check_only_requested" = false ] || fail "duplicate option: --check-only"
      check_only_requested=true
      ;;
    --full)
      [ "$full_requested" = false ] || fail "duplicate option: --full"
      full_requested=true
      ;;
    --no-fetch)
      [ "$no_fetch_requested" = false ] || fail "duplicate option: --no-fetch"
      no_fetch_requested=true
      FETCH_REMOTE=false
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      fail "unsupported argument: $argument"
      ;;
  esac
done

if [ "$check_only_requested" = true ]; then
  [ "$full_requested" = false ] && [ "$no_fetch_requested" = false ] ||
    fail "--check-only cannot be combined with other options"
  MODE=check-only
elif [ "$full_requested" = true ]; then
  MODE=full
elif [ "$no_fetch_requested" = true ]; then
  MODE=current
fi

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
      addp-postgres|addp-redis|addp-minio|addp-meilisearch|addp-redpanda|addp-redpanda-init|addp-kafka-connect|addp-local-ci-mysql|addp-local-ci-oceanbase)
        printf '%s\n' "$name"
        ;;
    esac
  done
}

local_ci_compose() {
  docker compose --env-file /dev/null -f "$LOCAL_CI_COMPOSE_FILE" "$@"
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
  local_ci_compose config --quiet >/dev/null 2>&1 ||
    fail "disposable database Compose definition is invalid: $LOCAL_CI_COMPOSE_FILE"
  active_infra=$(running_addp_infra)
  [ -z "$active_infra" ] || fail "running ADDP Infra belongs to another session: $active_infra"
  [ "$(git branch --show-current)" = "main" ] || fail "dedicated checkout must be on main"
  [ ! -e "$ROOT_DIR/.env" ] || fail "dedicated checkout must not contain a repository-root .env"
  [ -z "$(tracked_or_untracked_changes)" ] ||
    fail "dedicated checkout is not clean; refusing to overwrite local work"
  git remote get-url origin >/dev/null 2>&1 || fail "origin remote is missing"
}

clear_integration_gate_environment() {
  unset ADDP_SYSTEM_POSTGRES_TEST_DSN
  unset ASSET_POSTGRES_TEST_DSN
  unset META_POSTGRES_TEST_DSN
  unset CATALOG_POSTGRES_TEST_DSN
  unset DEVELOP_POSTGRES_TEST_DSN
  unset ADDP_TEST_MODEL_POSTGRES_DSN
  unset SERVICE_POSTGRES_TEST_DSN
  unset STANDARD_POSTGRES_TEST_DSN
  unset WORKBENCH_POSTGRES_TEST_DSN
  unset ADDP_POSTGRES_INTEGRATION
  unset ADDP_TEST_POSTGRES_HOST
  unset ADDP_TEST_POSTGRES_PORT
  unset ADDP_TEST_POSTGRES_USER
  unset ADDP_TEST_POSTGRES_PASSWORD
  unset ADDP_TEST_POSTGRES_DATABASE
  unset ADDP_TEST_POSTGRES_SSLMODE
  unset ADDP_TEST_MYSQL_HOST
  unset ADDP_TEST_MYSQL_PORT
  unset ADDP_TEST_MYSQL_USER
  unset ADDP_TEST_MYSQL_PASSWORD
  unset ADDP_TEST_MYSQL_DATABASE
  unset ADDP_LOCAL_CI_MYSQL
  unset ADDP_TEST_OCEANBASE_HOST
  unset ADDP_TEST_OCEANBASE_PORT
  unset ADDP_TEST_OCEANBASE_TENANT
  unset ADDP_TEST_OCEANBASE_USER
  unset ADDP_TEST_OCEANBASE_PASSWORD
  unset ADDP_TEST_OCEANBASE_DATABASE
  unset ADDP_LOCAL_CI_OCEANBASE
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

prepare_playwright_browser() {
  echo "==> Prepare Playwright Chromium"
  npm --prefix model/frontend exec -- playwright install chromium
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
  prepare_playwright_browser
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

run_integration_gates() {
  local shared_dsn iam_dsn
  shared_dsn='postgres://addp:addp_password@127.0.0.1:15432/addp_test?sslmode=disable'
  iam_dsn='postgres://addp:addp_password@127.0.0.1:15432/addp_iam_test?sslmode=disable'
  env \
    ADDP_SYSTEM_POSTGRES_TEST_DSN="$iam_dsn" \
    ASSET_POSTGRES_TEST_DSN="$shared_dsn" \
    META_POSTGRES_TEST_DSN="$shared_dsn" \
    CATALOG_POSTGRES_TEST_DSN="$shared_dsn" \
    DEVELOP_POSTGRES_TEST_DSN="$shared_dsn" \
    ADDP_TEST_MODEL_POSTGRES_DSN="$shared_dsn" \
    SERVICE_POSTGRES_TEST_DSN="$shared_dsn" \
    STANDARD_POSTGRES_TEST_DSN="$shared_dsn" \
    WORKBENCH_POSTGRES_TEST_DSN="$shared_dsn" \
    ADDP_TEST_POSTGRES_HOST=127.0.0.1 \
    ADDP_TEST_POSTGRES_PORT=15432 \
    ADDP_TEST_POSTGRES_USER=addp \
    ADDP_TEST_POSTGRES_PASSWORD=addp_password \
    ADDP_TEST_POSTGRES_DATABASE=addp_test \
    ADDP_TEST_POSTGRES_SSLMODE=disable \
    ADDP_LOCAL_CI_MYSQL=1 \
    ADDP_LOCAL_CI_OCEANBASE=1 \
    "$@"
}

wait_for_disposable_database() {
  local service=$1
  local display_name=$2
  local container=$3
  local endpoint=$4
  local attempts=$5
  local health

  for _ in $(seq 1 "$attempts"); do
    health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' \
      "$container" 2>/dev/null || true)
    case "$health" in
      healthy)
        echo "Disposable $display_name is healthy: $endpoint"
        return
        ;;
      unhealthy|exited|dead)
        local_ci_compose logs --no-color "$service" || true
        fail "disposable $display_name failed to start: container state is $health"
        ;;
    esac
    sleep 1
  done

  local_ci_compose logs --no-color "$service" || true
  fail "disposable $display_name failed to start: health check timed out"
}

start_disposable_databases() {
  echo "==> Start disposable MySQL 8 for local CI"
  disposable_databases_started=1
  if ! local_ci_compose up -d --force-recreate mysql; then
    local_ci_compose logs --no-color mysql || true
    fail "disposable MySQL failed to start"
  fi
  wait_for_disposable_database mysql MySQL "$MYSQL_CONTAINER_NAME" 127.0.0.1:13306 120

  echo "==> Start disposable OceanBase CE 4.4.2 LTS for local CI"
  if ! local_ci_compose up -d --force-recreate oceanbase; then
    local_ci_compose logs --no-color oceanbase || true
    fail "disposable OceanBase failed to start"
  fi
  wait_for_disposable_database oceanbase OceanBase "$OCEANBASE_CONTAINER_NAME" 127.0.0.1:12881 720
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
  local remote_sync=$4
  local log_file=$5
  local temporary="$LATEST_SUMMARY.tmp.$$"
  {
    printf 'schema_version=addp.local-ci-summary/v2\n'
    printf 'result=%s\n' "$result"
    printf 'sha=%s\n' "$sha"
    printf 'scope=%s\n' "$scope"
    printf 'remote_sync=%s\n' "$remote_sync"
    printf 'finished_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'log=%s\n' "$log_file"
  } > "$temporary"
  mv "$temporary" "$LATEST_SUMMARY"
}

validate_host
clear_integration_gate_environment
if [ "$MODE" = "check-only" ]; then
  echo "Local macOS CI readiness check passed: $ROOT_DIR"
  exit 0
fi

acquire_lock

if [ "$FETCH_REMOTE" = true ]; then
  remote_sync=performed
  echo "==> Fetch origin/main"
  git fetch origin main
  git merge --ff-only refs/remotes/origin/main
  [ "$(git rev-parse HEAD)" = "$(git rev-parse refs/remotes/origin/main)" ] ||
    fail "local main does not exactly match origin/main"
  [ -z "$(tracked_or_untracked_changes)" ] || fail "checkout became dirty after fast-forward"
else
  remote_sync=skipped
  echo "==> Use current main checkout (no fetch)"
fi

target_sha=$(git rev-parse HEAD)
last_success=$(sed -n '1p' "$LAST_SUCCESS_FILE" 2>/dev/null || true)
if [ "$MODE" = "incremental" ] && [ -n "$last_success" ] && [ "$last_success" = "$target_sha" ]; then
  echo "Local macOS CI is already successful for $target_sha"
  exit 0
fi

scope=$MODE
if [ "$scope" = current ]; then
  scope=incremental
fi
if [ -z "$last_success" ] || ! git merge-base --is-ancestor "$last_success" "$target_sha"; then
  scope=full
fi

timestamp=$(date -u '+%Y%m%dT%H%M%SZ')
short_sha=$(git rev-parse --short=12 "$target_sha")
log_file="$LOG_DIR/${timestamp}-${short_sha}-${scope}.log"
exec > >(tee -a "$log_file") 2>&1

infra_started=0
disposable_databases_started=0
finish_run() {
  local status=$?
  trap - EXIT INT TERM
  if [ "$infra_started" -eq 1 ]; then
    echo "==> Stop ADDP CI infrastructure"
    if ! run_infra make infra-down; then
      status=1
    fi
  fi
  if [ "$disposable_databases_started" -eq 1 ]; then
    echo "==> Stop disposable database CI infrastructure"
    if ! local_ci_compose down --volumes --remove-orphans; then
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
    write_summary passed "$target_sha" "$scope" "$remote_sync" "$log_file"
    echo "Local macOS CI passed: $target_sha (scope=$scope remote_sync=$remote_sync)"
  else
    write_summary failed "$target_sha" "$scope" "$remote_sync" "$log_file"
    echo "Local macOS CI failed: $target_sha (scope=$scope remote_sync=$remote_sync)" >&2
  fi
  find "$LOG_DIR" -type f -name '*.log' -mtime +13 -delete 2>/dev/null || true
  release_lock
  exit "$status"
}
trap finish_run EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "Local macOS CI start: sha=$target_sha scope=$scope remote_sync=$remote_sync"
start_disposable_databases
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
  echo "==> Run all registered infrastructure integration gates"
  run_integration_gates make test-integration
else
  run_integration_gates make test-changed "BASE_REF=$last_success"
fi
