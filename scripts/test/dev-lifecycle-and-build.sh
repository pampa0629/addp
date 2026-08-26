#!/bin/bash
# dev-lifecycle-and-build.sh - 开发环境生命周期锁与原子构建回归测试

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOCK_SCRIPT="${ROOT_DIR}/scripts/dev/lifecycle-lock.sh"
BUILD_SCRIPT="${ROOT_DIR}/scripts/dev/build-identity.sh"
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/addp-dev-lifecycle-test.XXXXXX")

cleanup() {
  rm -rf "$TEST_ROOT"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

wait_for_path() {
  local path="$1"
  for _ in {1..50}; do
    [ -e "$path" ] && return 0
    sleep 0.02
  done
  return 1
}

test_lifecycle_lock_rejects_concurrent_owner() {
  local workspace="${TEST_ROOT}/lock-concurrent"
  mkdir -p "$workspace"

  ROOT_DIR="$workspace" bash -c 'source "$1"; addp_acquire_lifecycle_lock holder; sleep 3' _ "$LOCK_SCRIPT" &
  local holder_pid=$!
  wait_for_path "$workspace/.dev-state/lifecycle.lock/owner" || fail "lock owner metadata not created"

  local output
  if output=$(ROOT_DIR="$workspace" bash -c 'source "$1"; addp_acquire_lifecycle_lock contender' _ "$LOCK_SCRIPT" 2>&1); then
    kill "$holder_pid" 2>/dev/null || true
    fail "concurrent lock acquisition unexpectedly succeeded"
  fi
  [[ "$output" == *"拒绝并行执行 contender"* ]] || fail "concurrent error lacks operation details: $output"
  wait "$holder_pid"
  [ ! -d "$workspace/.dev-state/lifecycle.lock" ] || fail "lock directory remained after owner exit"
}

test_lifecycle_lock_allows_descendant_inheritance() {
  local workspace="${TEST_ROOT}/lock-inherit"
  mkdir -p "$workspace"

  ROOT_DIR="$workspace" LOCK_SCRIPT="$LOCK_SCRIPT" bash -c '
    source "$LOCK_SCRIPT"
    addp_acquire_lifecycle_lock parent
    bash -c '\''source "$LOCK_SCRIPT"; addp_acquire_lifecycle_lock child'\''
  '
  [ ! -d "$workspace/.dev-state/lifecycle.lock" ] || fail "inherited lock was not released by owner"
}

test_keepalive_style_cleanup_inherits_and_releases_lock() {
  local workspace="${TEST_ROOT}/lock-keepalive-cleanup"
  local child_state="${workspace}/child-state"
  mkdir -p "$workspace"

  local exit_code
  set +e
  ROOT_DIR="$workspace" LOCK_SCRIPT="$LOCK_SCRIPT" CHILD_STATE="$child_state" bash -c '
    source "$LOCK_SCRIPT"
    addp_acquire_lifecycle_lock keepalive restart -all

    cleanup() {
      local cleanup_exit_code=$?
      trap - INT TERM
      bash -c '\''
        source "$LOCK_SCRIPT"
        addp_acquire_lifecycle_lock stop
        printf "inherited=%s\nowner=%s\n" "$ADDP_LIFECYCLE_LOCK_INHERITED" "$ADDP_LIFECYCLE_OWNER_PID" > "$CHILD_STATE"
      '\''
      addp_release_lifecycle_lock || true
      exit "$cleanup_exit_code"
    }

    trap cleanup EXIT
    trap '\''exit 130'\'' INT
    trap '\''exit 143'\'' TERM
    exit 17
  '
  exit_code=$?
  set -e

  [ "$exit_code" -eq 17 ] || fail "keepalive-style cleanup changed exit code: $exit_code"
  [ -f "$child_state" ] || fail "keepalive child did not acquire inherited lock"
  rg -q '^inherited=1$' "$child_state" || fail "keepalive child did not use inherited lock: $(cat "$child_state")"
  [ ! -d "$workspace/.dev-state/lifecycle.lock" ] || fail "keepalive owner did not release lifecycle lock"
}

test_lifecycle_lock_rejects_stale_lock() {
  local workspace="${TEST_ROOT}/lock-stale"
  mkdir -p "$workspace/.dev-state/lifecycle.lock"
  printf 'pid=99999999\noperation=stale\nstarted_at=2026-08-13T00:00:00Z\n' > "$workspace/.dev-state/lifecycle.lock/owner"

  local output
  if output=$(ROOT_DIR="$workspace" bash -c 'source "$1"; addp_acquire_lifecycle_lock contender' _ "$LOCK_SCRIPT" 2>&1); then
    fail "stale lock acquisition unexpectedly succeeded"
  fi
  [[ "$output" == *"失效的开发环境生命周期锁"* ]] || fail "stale lock error is not explicit: $output"
}

test_atomic_build_does_not_replace_binary_on_failure() {
  local workspace="${TEST_ROOT}/build-failure"
  mkdir -p "$workspace/common/buildinfo" "$workspace/service/cmd/server" "$workspace/.dev-bins"
  printf 'module github.com/addp/common\n\ngo 1.23\n' > "$workspace/common/go.mod"
  printf 'package buildinfo\nvar BuildID, GitCommit, SourceFingerprint, BuiltAt string\n' > "$workspace/common/buildinfo/buildinfo.go"
  printf 'module example/service\n\ngo 1.23\n\nrequire github.com/addp/common v0.0.0\nreplace github.com/addp/common => ../common\n' > "$workspace/service/go.mod"
  printf 'package main\nfunc main() { doesNotCompile }\n' > "$workspace/service/cmd/server/main.go"
  printf 'old-binary' > "$workspace/.dev-bins/addp-service"

  if (cd "$workspace" && PROJECT_ROOT="$workspace" bash -c 'source "$1"; addp_atomic_go_build service service .dev-bins/addp-service ./cmd/server' _ "$BUILD_SCRIPT") >/dev/null 2>&1; then
    fail "invalid source unexpectedly built"
  fi
  [ "$(cat "$workspace/.dev-bins/addp-service")" = "old-binary" ] || fail "failed build replaced existing binary"
}

test_atomic_build_rejects_non_workspace_output() {
  local workspace="${TEST_ROOT}/build-invalid-output"
  mkdir -p "$workspace"

  local output
  if output=$(cd "$workspace" && PROJECT_ROOT="$workspace" bash -c 'source "$1"; addp_atomic_go_build service service /tmp/addp-invalid-output ./cmd/server' _ "$BUILD_SCRIPT" 2>&1); then
    fail "absolute build output unexpectedly accepted"
  fi
  [[ "$output" == *"工作区内的规范相对路径"* ]] || fail "invalid output error is not explicit: $output"
}

test_source_fingerprint_excludes_workspace_build_caches() {
  local workspace="${TEST_ROOT}/fingerprint-cache-exclusion"
  mkdir -p "$workspace/service" "$workspace/.gomodcache/example" "$workspace/tools"
  printf 'package service\n' > "$workspace/service/service.go"
  printf 'module example/service\n\ngo 1.23\n' > "$workspace/service/go.mod"
  printf 'cached dependency source\n' > "$workspace/.gomodcache/example/dependency.go"
  cat > "$workspace/tools/go-list-wrapper" <<'EOF'
#!/bin/bash
workspace=$(cd "$(dirname "$0")/.." && pwd -P)
printf '%s\n' \
  "$workspace/service/service.go" \
  "$workspace/service/go.mod" \
  "$workspace/.gomodcache/example/dependency.go"
EOF
  chmod +x "$workspace/tools/go-list-wrapper"

  local before cache_changed source_changed
  before=$(cd "$workspace" && PROJECT_ROOT="$workspace" ADDP_GO_LIST_COMMAND="$workspace/tools/go-list-wrapper" bash -c 'source "$1"; addp_source_fingerprint service ./...' _ "$BUILD_SCRIPT")
  printf 'cache changed\n' >> "$workspace/.gomodcache/example/dependency.go"
  cache_changed=$(cd "$workspace" && PROJECT_ROOT="$workspace" ADDP_GO_LIST_COMMAND="$workspace/tools/go-list-wrapper" bash -c 'source "$1"; addp_source_fingerprint service ./...' _ "$BUILD_SCRIPT")
  [ "$cache_changed" = "$before" ] || fail "module cache content changed source fingerprint"

  printf '// local source changed\n' >> "$workspace/service/service.go"
  source_changed=$(cd "$workspace" && PROJECT_ROOT="$workspace" ADDP_GO_LIST_COMMAND="$workspace/tools/go-list-wrapper" bash -c 'source "$1"; addp_source_fingerprint service ./...' _ "$BUILD_SCRIPT")
  [ "$source_changed" != "$before" ] || fail "workspace source change did not change fingerprint"
}

test_atomic_build_embeds_identity_and_replaces_binary() {
  local workspace="${TEST_ROOT}/build-success"
  mkdir -p "$workspace/common/buildinfo" "$workspace/service/cmd/server" "$workspace/.dev-bins"
  printf 'module github.com/addp/common\n\ngo 1.23\n' > "$workspace/common/go.mod"
  printf 'package buildinfo\nvar BuildID = "unknown"\nvar GitCommit = "unknown"\nvar SourceFingerprint = "unknown"\nvar BuiltAt = "unknown"\n' > "$workspace/common/buildinfo/buildinfo.go"
  printf 'module example/service\n\ngo 1.23\n\nrequire github.com/addp/common v0.0.0\nreplace github.com/addp/common => ../common\n' > "$workspace/service/go.mod"
  printf 'package main\nimport (_ "embed"; "fmt"; "github.com/addp/common/buildinfo")\n//go:embed payload.txt\nvar payload string\nfunc main() { fmt.Printf("%%s|%%s|%%s|%%s", buildinfo.BuildID, buildinfo.GitCommit, buildinfo.SourceFingerprint, buildinfo.BuiltAt) }\n' > "$workspace/service/cmd/server/main.go"
  printf 'original payload\n' > "$workspace/service/cmd/server/payload.txt"
  printf 'old-binary' > "$workspace/.dev-bins/addp-service"

  (cd "$workspace" && PROJECT_ROOT="$workspace" bash -c 'source "$1"; addp_atomic_go_build service service .dev-bins/addp-service ./cmd/server' _ "$BUILD_SCRIPT")
  local identity
  identity=$($workspace/.dev-bins/addp-service)
  [[ "$identity" == *"service"*"|"*"|sha256:"*"|"* ]] || fail "build identity was not embedded: $identity"
  [ "$(cat "$workspace/.dev-bins/addp-service" 2>/dev/null || true)" != "old-binary" ] || fail "successful build did not replace binary"
  (cd "$workspace" && PROJECT_ROOT="$workspace" bash -c 'source "$1"; addp_go_build_is_current service .dev-bins/addp-service ./cmd/server' _ "$BUILD_SCRIPT") || fail "successful build was not recognized as current"
  printf 'changed payload\n' >> "$workspace/service/cmd/server/payload.txt"
  if (cd "$workspace" && PROJECT_ROOT="$workspace" bash -c 'source "$1"; addp_go_build_is_current service .dev-bins/addp-service ./cmd/server' _ "$BUILD_SCRIPT"); then
    fail "embedded resource change did not invalidate incremental build"
  fi
}

test_interrupted_build_removes_temporary_binary() {
  local workspace="${TEST_ROOT}/build-interrupted"
  mkdir -p "$workspace/common/buildinfo" "$workspace/service/cmd/server" "$workspace/.dev-bins" "$workspace/tools"
  printf 'module github.com/addp/common\n\ngo 1.23\n' > "$workspace/common/go.mod"
  printf 'package buildinfo\nvar BuildID, GitCommit, SourceFingerprint, BuiltAt string\n' > "$workspace/common/buildinfo/buildinfo.go"
  printf 'module example/service\n\ngo 1.23\n\nrequire github.com/addp/common v0.0.0\nreplace github.com/addp/common => ../common\n' > "$workspace/service/go.mod"
  printf 'package main\nimport "github.com/addp/common/buildinfo"\nfunc main() { _ = buildinfo.BuildID }\n' > "$workspace/service/cmd/server/main.go"
  cat > "$workspace/tools/go-wrapper" <<'EOF'
#!/bin/bash
set -e
workspace=$(cd "$(dirname "$0")/.." && pwd)
previous=""
for argument in "$@"; do
  if [ "$previous" = "-o" ]; then
    : > "$argument"
    break
  fi
  previous="$argument"
done
printf '%s\n' "$$" > "$workspace/wrapper.pid"
trap 'exit 143' TERM
while true; do sleep 1; done
EOF
  chmod +x "$workspace/tools/go-wrapper"

  PROJECT_ROOT="$workspace" ADDP_GO_COMMAND="$workspace/tools/go-wrapper" bash -c 'cd "$2"; source "$1"; addp_atomic_go_build service service .dev-bins/addp-service ./cmd/server' _ "$BUILD_SCRIPT" "$workspace" &
  local build_pid=$!
  for _ in {1..50}; do
    compgen -G "$workspace/.dev-bins/.tmp/addp-service.*" >/dev/null && break
    sleep 0.02
  done
  compgen -G "$workspace/.dev-bins/.tmp/addp-service.*" >/dev/null || {
    kill "$build_pid" 2>/dev/null || true
    fail "interrupted build did not create temporary binary path"
  }
  wait_for_path "$workspace/wrapper.pid" || fail "interrupted build wrapper pid was not recorded"
  kill -TERM "$(cat "$workspace/wrapper.pid")" 2>/dev/null || true
  wait "$build_pid" 2>/dev/null || true
  if compgen -G "$workspace/.dev-bins/.tmp/addp-service.*" >/dev/null; then
    fail "interrupted build left a temporary binary"
  fi
}

test_atomic_build_rejects_source_change() {
  local workspace="${TEST_ROOT}/build-source-change"
  mkdir -p "$workspace/common/buildinfo" "$workspace/service/cmd/server" "$workspace/.dev-bins" "$workspace/tools"
  printf 'module github.com/addp/common\n\ngo 1.23\n' > "$workspace/common/go.mod"
  printf 'package buildinfo\nvar BuildID = "unknown"\nvar GitCommit = "unknown"\nvar SourceFingerprint = "unknown"\nvar BuiltAt = "unknown"\n' > "$workspace/common/buildinfo/buildinfo.go"
  printf 'module example/service\n\ngo 1.23\n\nrequire github.com/addp/common v0.0.0\nreplace github.com/addp/common => ../common\n' > "$workspace/service/go.mod"
  printf 'package main\nimport (_ "embed"; "github.com/addp/common/buildinfo")\n//go:embed payload.txt\nvar payload string\nfunc main() { _, _ = buildinfo.BuildID, payload }\n' > "$workspace/service/cmd/server/main.go"
  printf 'original payload\n' > "$workspace/service/cmd/server/payload.txt"
  printf 'old-binary' > "$workspace/.dev-bins/addp-service"
  cat > "$workspace/tools/go-wrapper" <<'EOF'
#!/bin/bash
set -e
go "$@"
printf 'changed during build\n' >> cmd/server/payload.txt
EOF
  chmod +x "$workspace/tools/go-wrapper"

  local output
  if output=$(cd "$workspace" && PROJECT_ROOT="$workspace" ADDP_GO_COMMAND="$workspace/tools/go-wrapper" bash -c 'source "$1"; addp_atomic_go_build service service .dev-bins/addp-service ./cmd/server' _ "$BUILD_SCRIPT" 2>&1); then
    fail "source-changing build unexpectedly succeeded"
  fi
  [[ "$output" == *"构建期间源码发生变化"* ]] || fail "source change error is not explicit: $output"
  [ "$(cat "$workspace/.dev-bins/addp-service")" = "old-binary" ] || fail "source-changing build replaced existing binary"
}

test_all_go_health_routes_use_module_lifecycle() {
  local health_route_count buildinfo_route_count
  health_route_count=$(rg -l 'RegisterHealthRoutes\(router\)' "$ROOT_DIR" --glob '*.go' --glob '!**/*_test.go' | wc -l | tr -d ' ')
  buildinfo_route_count=$(rg -l 'RegisterHealthRoutes\(router\)' "$ROOT_DIR" --glob '*.go' --glob '!**/*_test.go' | xargs rg -l 'modulelifecycle' | wc -l | tr -d ' ')
  [ "$health_route_count" -gt 0 ] || fail "no Go health routes found"
  [ "$buildinfo_route_count" = "$health_route_count" ] || fail "health routes using buildinfo = $buildinfo_route_count, want $health_route_count"
}

test_lifecycle_lock_rejects_concurrent_owner
test_lifecycle_lock_allows_descendant_inheritance
test_keepalive_style_cleanup_inherits_and_releases_lock
test_lifecycle_lock_rejects_stale_lock
test_source_fingerprint_excludes_workspace_build_caches
test_atomic_build_rejects_non_workspace_output
test_atomic_build_does_not_replace_binary_on_failure
test_atomic_build_embeds_identity_and_replaces_binary
test_interrupted_build_removes_temporary_binary
test_atomic_build_rejects_source_change
test_all_go_health_routes_use_module_lifecycle

echo "PASS: dev lifecycle lock and atomic build tests"
