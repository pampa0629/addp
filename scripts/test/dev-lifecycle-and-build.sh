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

test_parallel_build_wait_propagates_failure() {
  local completed_marker="${TEST_ROOT}/parallel-build-completed"
  local output exit_code

  set +e
  output=$(BUILD_SCRIPT="$BUILD_SCRIPT" COMPLETED_MARKER="$completed_marker" bash -c '
    source "$BUILD_SCRIPT"
    (sleep 0.1; printf "completed\n" > "$COMPLETED_MARKER") &
    successful_pid=$!
    (exit 7) &
    failed_pid=$!
    addp_wait_for_parallel_tasks "构建" "$successful_pid" "$failed_pid"
  ' 2>&1)
  exit_code=$?
  set -e

  [ "$exit_code" -ne 0 ] || fail "parallel build failure was reported as success"
  [ -f "$completed_marker" ] || fail "parallel build wait returned before every build finished"
  [[ "$output" == *"并行构建任务失败"* ]] || fail "parallel build failure lacks an explicit diagnostic: $output"

  BUILD_SCRIPT="$BUILD_SCRIPT" bash -c '
    source "$BUILD_SCRIPT"
    (exit 0) &
    first_pid=$!
    (exit 0) &
    second_pid=$!
    addp_wait_for_parallel_tasks "构建" "$first_pid" "$second_pid"
  ' || fail "successful parallel builds were reported as failed"
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
  # Execute the real start.sh build dispatch: an unchanged artifact must avoid the linker.
  python3 - "$ROOT_DIR/scripts/dev/start.sh" "$workspace/build-service.sh" <<'PYFUNC'
from pathlib import Path
import re
import sys
source = Path(sys.argv[1]).read_text()
Path(sys.argv[2]).write_text(re.search(r'^build_service\(\) \{.*?^\}', source, re.M | re.S).group(0))
PYFUNC
  (cd "$workspace" && PROJECT_ROOT="$workspace" bash -c '
    source "$1"
    source ./build-service.sh
    addp_atomic_go_build() { echo "unexpected rebuild" >&2; return 1; }
    build_service service service
  ' _ "$BUILD_SCRIPT") || fail "unchanged startup invoked a rebuild"
  local record="$workspace/.dev-bins/addp-service.fingerprint"
  mv "$record" "$record.saved"
  if (cd "$workspace" && PROJECT_ROOT="$workspace" bash -c 'source "$1"; addp_go_build_is_current service .dev-bins/addp-service ./cmd/server' _ "$BUILD_SCRIPT"); then
    fail "missing fingerprint reused a binary"
  fi
  mv "$record.saved" "$record"
  mkdir -p "$workspace/tools"
  cat > "$workspace/tools/go-version-wrapper" <<'VERSION'
#!/bin/bash
if [ "$1" = env ]; then
  go "$@" | sed 's/"GOVERSION": "[^"]*"/"GOVERSION": "go0.fixture"/'
else
  exec go "$@"
fi
VERSION
  chmod +x "$workspace/tools/go-version-wrapper"
  if (cd "$workspace" && PROJECT_ROOT="$workspace" ADDP_GO_LIST_COMMAND="$workspace/tools/go-version-wrapper" bash -c 'source "$1"; addp_go_build_is_current service .dev-bins/addp-service ./cmd/server' _ "$BUILD_SCRIPT"); then
    fail "Go toolchain change did not invalidate cached binary"
  fi
  if (cd "$workspace" && PROJECT_ROOT="$workspace" GOFLAGS=-trimpath bash -c 'source "$1"; addp_go_build_is_current service .dev-bins/addp-service ./cmd/server' _ "$BUILD_SCRIPT"); then
    fail "Go build flags did not invalidate cached binary"
  fi
  if (cd "$workspace" && PROJECT_ROOT="$workspace" bash -c 'source "$1"; addp_go_build_is_current service .dev-bins/addp-service -tags unused_fixture_tag ./cmd/server' _ "$BUILD_SCRIPT"); then
    fail "build arguments did not invalidate cached binary"
  fi
  printf '// shared dependency changed\n' >> "$workspace/common/buildinfo/buildinfo.go"
  if (cd "$workspace" && PROJECT_ROOT="$workspace" bash -c 'source "$1"; addp_go_build_is_current service .dev-bins/addp-service ./cmd/server' _ "$BUILD_SCRIPT"); then
    fail "shared dependency change did not invalidate cached binary"
  fi
  sed '$d' "$workspace/common/buildinfo/buildinfo.go" > "$workspace/common/buildinfo/restored"
  mv "$workspace/common/buildinfo/restored" "$workspace/common/buildinfo/buildinfo.go"
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

test_restart_preserves_cache_and_batches_swagger() {
  python3 - "$ROOT_DIR" "$TEST_ROOT" <<'PY'
import os
from pathlib import Path
import shutil
import subprocess
import sys

repository, temporary = map(Path, sys.argv[1:])
for name, args, failed in (
    ("all", ["-all"], False),
    ("default", [], False),
    ("selected", ["-system", "-asset", "-meta"], False),
    ("swagger-failure", ["-all"], True),
):
    root = temporary / ("restart-" + name)
    dev = root / "scripts/dev"
    dev.mkdir(parents=True)
    for filename in ("restart.sh", "lifecycle-lock.sh", "node-dependencies.sh", "jupyter-env.sh"):
        shutil.copy2(repository / "scripts/dev" / filename, dev / filename)

    def script(relative, body):
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("#!/bin/bash\nset -e\n" + body)
        path.chmod(0o755)

    # Real restart orchestration, but no process termination, service or database access.
    script("tools/pkill", "exit 0\n")
    script("tools/go", 'echo "$*" >> "$FIXTURE_ROOT/go-calls"\n')
    script("scripts/dev/stop.sh", 'echo stop >> "$FIXTURE_ROOT/events"\n')
    script("scripts/dev/start.sh", '''
ROOT_DIR="$FIXTURE_ROOT"
source "$ROOT_DIR/scripts/dev/lifecycle-lock.sh"
addp_acquire_lifecycle_lock start
echo start >> "$ROOT_DIR/events"
''')
    script("scripts/swagger/gen-swagger.sh", '''
echo "generate $*" >> "$FIXTURE_ROOT/events"
if [ "$FAIL_SWAGGER" = 1 ]; then exit 7; fi
echo generated >> "$FIXTURE_ROOT/events"
''')
    script("scripts/swagger/check-route-coverage.sh", '''
echo "coverage $*" >> "$FIXTURE_ROOT/events"
# Coverage remains advisory during the documented cleanup period.
exit 1
''')
    sources = []
    for module in ("system/backend", "asset/backend", "meta/backend", "common", "gateway"):
        source = root / module / "source.go"
        source.parent.mkdir(parents=True, exist_ok=True)
        source.write_text("package fixture\n")
        os.utime(source, (1_600_000_000, 1_600_000_000))
        sources.append(source)
    bins = root / ".dev-bins"
    bins.mkdir()
    for binary in ("system", "asset", "meta", "meta-worker", "gateway"):
        (bins / ("addp-" + binary)).write_text("old binary")
    env = os.environ.copy()
    for key in list(env):
        if key.startswith("ADDP_LIFECYCLE_"):
            del env[key]
    env.update(
        PATH=str(root / "tools") + os.pathsep + env["PATH"],
        FIXTURE_ROOT=str(root), FAIL_SWAGGER=str(int(failed)),
        ALLOW_SWAGGER_FAILURE="0", MEILISEARCH_PORT="17700", SERVICE_HOST="localhost",
    )
    result = subprocess.run(["bash", str(dev / "restart.sh"), *args], env=env,
                            text=True, capture_output=True, timeout=30)
    assert (result.returncode != 0) == failed, result.stdout + result.stderr
    assert not (root / "go-calls").exists(), "restart must not clear the global Go cache"
    assert all(source.stat().st_mtime_ns == 1_600_000_000_000_000_000 for source in sources), \
        "restart must not touch source timestamps"
    events = (root / "events").read_text().splitlines()
    target = "all" if args in ([], ["-all"]) else "system asset meta"
    expected = ["stop", "generate " + target]
    if not failed:
        expected += ["generated", "coverage " + target, "start"]
    for binary in ("system", "asset", "meta", "meta-worker", "gateway"):
        assert (bins / ("addp-" + binary)).read_text() == "old binary", "restart must preserve " + binary
    assert events == expected, events
    assert not (root / ".dev-state/lifecycle.lock").exists(), "restart leaked its lock"
print("PASS: restart preserves cache, batches Swagger and stops on generation failure")
PY
}

test_stop_batches_listening_ports() {
  python3 - "$ROOT_DIR" "$TEST_ROOT" <<'PY'
import os
from pathlib import Path
import re
import subprocess
import sys

root, temporary = map(Path, sys.argv[1:])
source = (root / "scripts/dev/stop.sh").read_text()
# Exercise the actual Phase 6 orchestration without running destructive stop phases.
phase = source.split("  # Phase 6:", 1)[1].split("  # Phase 7:", 1)[0]
phase = phase.split("\n", 1)[1]
helper = re.search(r"^stop_port_listeners\(\) \{.*?^\}", source, re.M | re.S)
helpers = helper.group(0) if helper else ""
for mode in ("listeners", "empty"):
    workspace = temporary / ("stop-ports-" + mode)
    workspace.mkdir()
    script = r'''
YELLOW= RED= NC=
lsof() {
  printf '%s\n' "$*" >> "$FIXTURE_ROOT/lsof-calls"
  [ "$FIXTURE_MODE" != empty ] || return 1
  if [[ " $* " == *" -Fp "* ]]; then
    printf 'p101\nf10\np202\nf12\np101\nf15\np303\nf19\n'
  else
    printf '101\n202\n303\n'
  fi
}
ps() {
  printf '%s\n' "$*" >> "$FIXTURE_ROOT/ps-calls"
  case "$2" in
    101) printf '/workspace/.dev-bins/addp-manager\n' ;;
    202) printf '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome\n' ;;
    303) return 1 ;;
    *) printf '/workspace/.dev-bins/addp-manager\n/Applications/Google Chrome\n' ;;
  esac
}
kill() { printf '%s\n' "$*" >> "$FIXTURE_ROOT/kill-calls"; }
'''
    script += helpers + "\nrun_phase() {\n" + phase + "\n}\nrun_phase\n"
    result = subprocess.run(["bash", "-c", script], text=True, capture_output=True,
                            env=dict(os.environ, FIXTURE_ROOT=str(workspace), FIXTURE_MODE=mode), timeout=10)
    assert result.returncode == 0, result.stdout + result.stderr
    calls = (workspace / "lsof-calls").read_text().splitlines()
    assert len(calls) == 1, f"expected one listener scan, got {len(calls)}"
    args = calls[0].split()
    assert {"-nP", "-a", "-sTCP:LISTEN", "-Fp"} <= set(args), args
    ports = next(arg.removeprefix("-iTCP:") for arg in args if arg.startswith("-iTCP:"))
    assert {"8180", "8081", "5170"} <= set(ports.split(",")), ports
    if mode == "listeners":
        assert (workspace / "kill-calls").read_text().splitlines() == ["-9 101"]
        assert (workspace / "ps-calls").read_text().splitlines() == [
            "-p 101 -o command=", "-p 202 -o command=", "-p 303 -o command=",
        ]
        assert "202" in result.stdout and "非 ADDP" in result.stdout
        assert "303" not in result.stdout, "exited processes must not produce misleading warnings"
    else:
        assert not (workspace / "kill-calls").exists()
        assert not (workspace / "ps-calls").exists()
print("PASS: one LISTEN-only scan, deduplicated PIDs, foreign listeners preserved, exited/empty skipped")
PY
}

test_parallel_runtime_startup() {
  python3 - "$ROOT_DIR" "$TEST_ROOT" <<'PY'
import os
from pathlib import Path
import re
import subprocess
import sys

root, temp = map(Path, sys.argv[1:])
source = (root / 'scripts/dev/start.sh').read_text()
start = source.index('start_selected_runtimes() {')
end = source.index('\nstart_selected_runtimes\n', start)
phase = source[start:end]
assert source.index('✓ 后端服务和选定 Worker 全部就绪') < start < end < source.index('# 6. 启动 Gateway')
rows = re.findall(r'"(\w+)\|(START_\w+)\|(\w+)\|([\w-]+)"', phase)
assert len(rows) == 11
assert {name for name, *_ in rows} == set(re.findall(r'^start_runtime_(\w+)\(\) \(', source, re.M))
for mode in ('all', 'selected', 'none', 'failure'):
    workspace = temp / ('runtime-' + mode)
    (workspace / '.dev-pids').mkdir(parents=True)
    enabled = [row[0] for row in rows] if mode in ('all', 'failure') else (['math', 'jupyter'] if mode == 'selected' else [])
    script = 'set -eu\nsource "$BUILD_SCRIPT"\ncd "$ROOT_DIR"\nPORT=parent\n'
    script += '''fixture_task() {
  local name="$1" pidfile="$2" own_port="$1-port"
  [ "$PORT" = parent ]
  [ "$PWD" = "$ROOT_DIR" ]
  export PORT="$own_port"
  mkdir "$ROOT_DIR/$name.started"
  cd "$ROOT_DIR/$name.started"
  # All selected tasks must enter before any can complete; serial execution times out.
  for attempt in {1..150}; do
    count=$(find "$ROOT_DIR" -maxdepth 1 -name '*.started' | wc -l | tr -d ' ')
    [ "$count" -eq "$EXPECTED_COUNT" ] && break
    sleep 0.02
  done
  [ "$count" -eq "$EXPECTED_COUNT" ]
  [ "$PORT" = "$own_port" ]
  if [ "$MODE" = failure ] && [ "$name" = duckdb ]; then
    false
    touch "$ROOT_DIR/failure-was-masked"
  fi
  if [ "$MODE" = failure ]; then sleep 0.1; fi
  printf '%s\\n' "$name-service-id" > "$ROOT_DIR/.dev-pids/$pidfile.pid"
  touch "$ROOT_DIR/$name.done"
}
'''
    for name, flag, pidvar, pidfile in rows:
        script += f'{flag}={str(name in enabled).lower()}\n{pidvar}=stale\n'
        script += f'start_runtime_{name}() ( fixture_task {name} {pidfile}; )\n'
    script += phase + '\nstart_selected_runtimes\ntouch "$ROOT_DIR/gateway-started"\n'
    script += '[ "$PORT" = parent ]\n[ "$PWD" = "$ROOT_DIR" ]\n'
    for name, flag, pidvar, pidfile in rows:
        expected = name + '-service-id' if name in enabled else ''
        script += f'[ "${pidvar}" = "{expected}" ]\n'
    result = subprocess.run(['bash', '-c', script], env=dict(os.environ,
        ROOT_DIR=str(workspace), BUILD_SCRIPT=str(root/'scripts/dev/build-identity.sh'),
        EXPECTED_COUNT=str(len(enabled)), MODE=mode), capture_output=True, text=True, timeout=15)
    assert (result.returncode != 0) == (mode == 'failure'), result.stdout + result.stderr
    assert not (workspace/'failure-was-masked').exists(), 'set -e disabled inside startup function'
    assert (workspace/'gateway-started').exists() == (mode != 'failure')
    done = {p.name.removesuffix('.done') for p in workspace.glob('*.done')}
    assert done == set(enabled) - ({'duckdb'} if mode == 'failure' else set()), (mode, done)
print('PASS: runtime concurrency, selection, isolation, service IDs and failure barrier')
PY
}

test_python_dependency_install_lock() {
  local workspace="${TEST_ROOT}/python-install"
  mkdir -p "$workspace"
  cat > "$workspace/install.sh" <<'INSTALL'
#!/bin/bash
set -eu
mkdir "$1/installing"
trap 'rmdir "$1/installing"' EXIT
[ "$2" = "argument with spaces" ]
sleep 0.05
echo complete >> "$1/completed"
INSTALL
  ROOT_DIR="$workspace" LOCK_SCRIPT="$LOCK_SCRIPT" bash -c '
    set -eu
    source "$LOCK_SCRIPT"
    pids=()
    for task in 1 2 3 4; do
      addp_with_python_dependency_lock "$ROOT_DIR" bash "$ROOT_DIR/install.sh" "$ROOT_DIR" "argument with spaces" &
      pids+=("$!")
    done
    failed=0
    for pid in "${pids[@]}"; do wait "$pid" || failed=1; done
    [ "$failed" -eq 0 ]
    if addp_with_python_dependency_lock "$ROOT_DIR" bash -c "exit 17"; then
      exit 1
    else
      [ "$?" -eq 17 ]
    fi
    addp_with_python_dependency_lock "$ROOT_DIR" bash "$ROOT_DIR/install.sh" "$ROOT_DIR" "argument with spaces"
    [ "$(wc -l < "$ROOT_DIR/completed" | tr -d " ")" -eq 5 ]
  ' || fail "Python install lock failed mutual exclusion, argument preservation or failure release"
  echo "PASS: Python dependency installs serialize and release on failure"
}

test_start_batches_listening_ports() {
  python3 - "$ROOT_DIR" "$TEST_ROOT" <<'PY'
import os
from pathlib import Path
import subprocess
import sys
root, temporary = map(Path, sys.argv[1:])
source = (root/'scripts/dev/start.sh').read_text()
marker = '# 批量阶段共用监听快照' if '# 批量阶段共用监听快照' in source else '# 检查服务是否已在运行'
helpers = source[source.index(marker):source.index('# 编译函数:')].rsplit('# ============================================================',1)[0]
base = r'''
set -eu
YELLOW= RED= NC=
cd "$CASE_ROOT"
lsof() {
  echo "$*" >> calls
  case "$MODE" in
    error) echo 'lsof: failed to scan' >&2; return 1 ;;
    malformed) echo 'not a listener record' ;;
    occupied|parse) printf 'p101\nf1\nn*:8180\nf2\nn[::1]:8180\np102\nf3\nn127.0.0.1:5170\n' ;;
    stale) if [ "$(wc -l < calls | tr -d ' ')" -eq 1 ]; then printf 'p101\nf1\nn*:8180\n'; else return 1; fi ;;
    *) return 1 ;;
  esac
}
ps() {
  case "$2" in
    111) return 0 ;;
    101) echo /Applications/foreign-service ;;
    102) echo /workspace/.dev-bins/addp-other ;;
    *) return 1 ;;
  esac
}
kill() { [ "$1" = -0 ] && [ "$2" = 111 ]; }
'''
for mode in ('empty', 'occupied', 'stale', 'error', 'managed', 'worker', 'dead-process', 'malformed', 'parse'):
    workspace=temporary/('start-ports-'+mode)
    (workspace/'.dev-pids').mkdir(parents=True)
    script=base+helpers+'\n'
    if mode=='managed':
        (workspace/'.dev-pids/managed.pid').write_text('111\n')
        script+='if check_service_running managed 8180; then exit 7; fi\ntouch completed\n'
    elif mode=='parse':
        script+='scan_start_listeners > parsed\ntouch completed\n'
    elif mode=='worker':
        script+='check_service_running worker\ntouch completed\n'
    elif mode=='dead-process':
        script+='require_started_process alive 111\nrequire_started_process dead 999\ntouch completed\n'
    else:
        script+='if declare -F begin_start_listener_batch >/dev/null; then begin_start_listener_batch; fi\n'
        if mode=='empty':
            script+='for port in 8180 8081 5170 5173 29101 29102; do check_service_running fixture "$port"; done\ntouch batched\n'
            script+='[ "$(wc -l < calls | tr -d " ")" -eq 1 ]\nend_start_listener_batch\ncheck_service_running single 8180\n'
        else:
            script+='check_service_running fixture 8180\n'
        script+='touch completed\n'
    result=subprocess.run(['bash','-c',script],env=dict(os.environ,CASE_ROOT=str(workspace),MODE=mode),capture_output=True,text=True,timeout=10)
    failed=mode in ('occupied','error','dead-process','malformed')
    assert (result.returncode!=0)==failed, (mode,result.stdout,result.stderr)
    assert (workspace/'completed').exists()!=failed, mode
    calls=(workspace/'calls').read_text().splitlines() if (workspace/'calls').exists() else []
    assert len(calls)=={'empty':2,'occupied':2,'stale':2,'error':1,'malformed':1,'parse':1}.get(mode,0), (mode,calls)
    for call in calls:
        assert {'-nP','-a','-sTCP:LISTEN','-Fpn'}<=set(call.split()), call
    if mode=='occupied':
        assert '101' in result.stderr and '未受管' in result.stderr
    if mode=='error': assert '无法查询' in result.stderr
    if mode=='parse': assert (workspace/'parsed').read_text().splitlines()==['5170 102','8180 101']
# Exercise the actual frontend health loop: another server answers 200, but our child exits.
workspace=temporary/'frontend-bind-race'
workspace.mkdir()
(workspace/'pids').write_text('fixture:111\n')
health=source[source.index('# 并发等待所有前端的健康检查'):source.index('echo -e "${GREEN}✓ 所有前端服务已启动')]
script=base+helpers+r'''
GREEN=
FRONTEND_CONFIGS=("fixture:8180:unused")
FRONTEND_PID_FILE="$CASE_ROOT/pids"
kill() {
  echo probe >> process-probes
  [ "$(wc -l < process-probes | tr -d ' ')" -eq 1 ]
}
curl() { touch foreign-http-success; return 0; }
'''+health+'\ntouch completed\n'
result=subprocess.run(['bash','-c',script],env=dict(os.environ,CASE_ROOT=str(workspace),MODE='race'),capture_output=True,text=True,timeout=10)
assert result.returncode!=0 and not (workspace/'completed').exists(), result.stdout+result.stderr
assert (workspace/'foreign-http-success').exists(), 'did not exercise foreign HTTP success'
assert '不能报告就绪' in result.stderr
assert source.count('  begin_start_listener_batch\n')==2, 'backend/frontend must each take a fresh snapshot'
assert source.count('  end_start_listener_batch\n')==2, 'snapshots must end with their launch batch'
assert '--strictPort' in source
assert 'require_started_process "$name" "$pid"' in source
print('PASS: batched startup scans, IPv4/IPv6, stale listeners, managed PIDs, scan errors and dead-process rejection')
PY
}

test_swagger_incremental_generation() {
  python3 - "$ROOT_DIR" "$TEST_ROOT" <<'PY'
import concurrent.futures
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

root = Path(sys.argv[1])
fixture = Path(sys.argv[2]) / 'swagger-cache'
fixture.mkdir()
script = fixture / 'scripts/swagger/gen-swagger.sh'
script.parent.mkdir(parents=True)
shutil.copy2(root / 'scripts/swagger/gen-swagger.sh', script)
tools = fixture / 'tools'
tools.mkdir()
for module in ('system', 'manager', 'common', 'external'):
    directory = fixture / module / 'backend'
    (directory / 'cmd/server').mkdir(parents=True)
    (directory / 'cmd/server/main.go').write_text('package main\n')
    (directory / 'go.mod').write_text('module example/' + module + '\n')
    (directory / 'go.sum').write_text('dependency v1 hash\n')
(fixture / 'go.work').write_text('workspace\n')
(fixture / 'go.work.sum').write_text('workspace checksums\n')

def tool(name, body):
    path = tools / name
    path.write_text('#!' + sys.executable + '\n' + body)
    path.chmod(0o755)

tool('go', '''import json, os
from pathlib import Path
import sys
root = Path.cwd()
if sys.argv[1] == 'env':
    print(json.dumps({'GOWORK': str(root / 'go.work'), 'GOFLAGS': os.environ.get('GOFLAGS', '')}))
else:
    for name in ('system', 'manager', 'common'):
        print(json.dumps({'Path': name, 'Main': True, 'Dir': str(root / name / 'backend')}))
    print(json.dumps({'Path': 'external', 'Replace': {'Path': '../external', 'Dir': str(root / 'external/backend')}}))
''')
tool('swag', '''from pathlib import Path
import os, time, sys
root = Path.cwd().parents[1]
with (root / 'calls').open('a') as stream:
    stream.write(Path.cwd().parent.name + '\\n')
time.sleep(float(os.environ.get('SWAG_DELAY', '0')))
Path('docs').mkdir(exist_ok=True)
for name in ('docs.go', 'swagger.json', 'swagger.yaml'):
    Path('docs', name).write_text('generated ' + name)
if os.environ.get('SWAG_MUTATE'):
    Path('cmd/server/main.go').write_text('changed while generating')
if os.environ.get('SWAG_FAIL'):
    sys.exit(9)
''')
env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ['PATH'])

def run(*modules, success=True, **extra):
    result = subprocess.run(['bash', str(script), *modules], cwd=fixture, env=dict(env, **extra), text=True, capture_output=True)
    assert (result.returncode == 0) == success, (result.returncode, result.stdout, result.stderr)
    return result

def calls():
    return len((fixture / 'calls').read_text().splitlines())

run('system', 'manager')
assert calls() == 2
run('system', 'manager')
assert calls() == 2, 'unchanged modules regenerated'
(fixture / 'system/backend/cmd/server/main.go').touch()
run('system')
assert calls() == 2, 'mtime-only changes invalidated content cache'
# Inputs include own annotations, shared types, local replacements, dependencies and tool.
for path in [fixture / p for p in (
    'system/backend/cmd/server/main.go', 'common/backend/cmd/server/main.go',
    'external/backend/cmd/server/main.go', 'external/backend/docs/docs.go',
    'common/backend/.types/model.go', 'system/backend/go.mod',
    'common/backend/go.sum', 'go.work', 'go.work.sum', 'system/backend/.swaggo',
    'system/backend/unimported_handler.go', 'tools/swag', 'scripts/swagger/gen-swagger.sh',
)]:
    previous = calls()
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text((path.read_text() if path.exists() else '') + '\n# changed\n')
    run('system')
    assert calls() == previous + 1, path
handler = fixture / 'system/backend/unimported_handler.go'
handler.unlink()
previous = calls()
run('system')
assert calls() == previous + 1, 'deleted source did not invalidate'
previous = calls()
run('system', GOFLAGS='-tags=swagger')
assert calls() == previous + 1, 'Go environment not included'
run('system')
for name in ('docs.go', 'swagger.json', 'swagger.yaml'):
    path = fixture / 'system/backend/docs' / name
    for change in ('edit', 'delete'):
        previous = calls()
        path.write_text('tampered') if change == 'edit' else path.unlink()
        run('system')
        assert calls() == previous + 1, (name, change)
manifest = fixture / '.dev-state/swagger/manifest.json'
for malformed in ('{', '[]', '{"system": 7}'):
    manifest.write_text(malformed)
    previous = calls()
    run('system')
    assert calls() == previous + 1
# Failed/interrupted generation never publishes a valid record.
(fixture / 'system/backend/docs/docs.go').unlink()
run('system', success=False, SWAG_FAIL='1')
assert 'system' not in json.loads(manifest.read_text())
previous = calls()
run('system')
assert calls() == previous + 1
(fixture / 'system/backend/docs/docs.go').unlink()
run('system', success=False, SWAG_MUTATE='1')
assert 'system' not in json.loads(manifest.read_text())
run('system')
# Two concurrent invocations only generate once and publish a complete manifest.
manifest.unlink()
previous = calls()
with concurrent.futures.ThreadPoolExecutor(2) as pool:
    futures = [pool.submit(run, 'system', SWAG_DELAY='0.2') for _ in range(2)]
    for future in futures:
        future.result()
assert calls() == previous + 1
# FastAPI reflects environment changes on every export, even with unchanged source.
directory = fixture / 'agent/backend'
directory.mkdir(parents=True)
(directory / 'main.py').write_text('import os\nclass App:\n def openapi(self): return {"value": os.environ["SCHEMA_VALUE"]}\napp = App()\n')
run('agent', SCHEMA_VALUE='first')
run('agent', SCHEMA_VALUE='second')
assert json.loads((directory / 'openapi.json').read_text()) == {'value': 'second'}
# A concurrent external edit of a reused document must never be blessed.
(directory / 'main.py').write_text('from pathlib import Path\nPath("../../system/backend/docs/swagger.json").write_text("edited during export")\nclass App:\n def openapi(self): return {}\napp = App()\n')
run('system', 'agent', success=False)
print('PASS: Swagger cache reuse, content/tool/dependency/output invalidation, failures, concurrent generation and live FastAPI export')
PY
}

test_worker_backend_readiness_order() {
  python3 - "$ROOT_DIR" "$TEST_ROOT" <<'PY'
from pathlib import Path
import os, re, subprocess
root, fixture = Path(__import__('sys').argv[1]), Path(__import__('sys').argv[2]) / 'worker-order'
fixture.mkdir()
source=(root/'scripts/dev/start.sh').read_text()
helper=source[source.index('start_module_workers() ('):source.index('BACKEND_STARTS=()')]
for directory in ('.dev-bins','.dev-pids','logs'):(fixture/directory).mkdir()
for name in ('fast-worker','slow-worker','failed-worker'):
 path=fixture/'.dev-bins'/('addp-'+name)
 path.write_text('#!/bin/bash\ntouch "'+name+'-started"\nexec sleep 10\n')
 path.chmod(0o755)
# Fast module must launch before the unrelated slow Backend becomes ready.
body='''set -e
require_started_process() { [ "$2" != dead ]; }
check_service_running() { return 0; }
curl() { case "$*" in *:1111/*) return 0;; *:2222/*) [ -f slow-ready ];; *) return 1;; esac; }
'''+helper+'''
trap 'for path in .dev-pids/*.pid; do [ ! -f "$path" ] || kill "$(cat "$path")" 2>/dev/null || true; done' EXIT
start_module_workers slow alive 2222 slow-worker &
slow=$!
start_module_workers fast alive 1111 fast-worker &
fast=$!
wait "$fast"
for i in {1..100}; do [ ! -f fast-worker-started ] || break; sleep 0.01; done
[ -f fast-worker-started ]
[ ! -f slow-worker-started ]
touch slow-ready
wait "$slow"
for i in {1..100}; do [ ! -f slow-worker-started ] || break; sleep 0.01; done
[ -f slow-worker-started ]
if start_module_workers failed dead 3333 failed-worker; then exit 7; fi
[ ! -f failed-worker-started ]
# Backend remains alive but never ready: bounded timeout also prevents Worker launch.
sleep() { :; }
if start_module_workers failed alive 3333 failed-worker; then exit 8; fi
[ ! -f failed-worker-started ]
'''
result=subprocess.run(['bash','-c',body],cwd=fixture,text=True,capture_output=True,timeout=15)
assert result.returncode==0,(result.stdout,result.stderr)
# Guard ownership and indirect initialization paths at the production entry points.
workers=['security/backend/cmd/worker/main.go','meta/backend/cmd/worker/main.go','quality/backend/cmd/worker/main.go','transfer/backend/cmd/worker/main.go','transfer/backend/cmd/continuous-worker/main.go']
for name in workers:
 content=(root/name).read_text()
 assert not re.search(r'\.(?:Migrate|AutoMigrate|EnsureStore|InitDatabase|PrepareSchema)\(',content),name
 assert 'schema.Require(' in content,name
for path in root.glob('*/backend/**/*.go'):
 if path.name.endswith('_test.go') or path.parts[-4:]==('system','backend','internal','repository'):continue
 text=path.read_text()
 if 'schema.InitializeCommon(' in text: assert path==root/'system/backend/internal/repository/database.go',path
 assert not re.search(r'(?i)(?:common)?(?:execution|runtimehealth)\.EnsureStore\(', text), path
compose=(root/'docker-compose.yml').read_text()
assert 'DB_SCHEMA=metadata' not in compose, 'retired Meta schema in deployment'
for worker,module in [('meta-worker','meta'),('quality-worker','quality'),('security-worker','security'),('transfer-bounded-worker','transfer')]:
 block=re.search(r'^  '+worker+r':.*?(?=^  [a-zA-Z][\w-]*:|\Z)',compose,re.M|re.S).group()
 assert module+'-backend:\n        condition: service_healthy' in block,worker
print('PASS: per-module Worker ordering, independent progress, dead/timeout Backend rejection and migration ownership')
PY
}

test_worker_backend_readiness_order
test_swagger_incremental_generation
test_start_batches_listening_ports
test_parallel_runtime_startup
test_python_dependency_install_lock
test_stop_batches_listening_ports
test_restart_preserves_cache_and_batches_swagger
test_lifecycle_lock_rejects_concurrent_owner
test_lifecycle_lock_allows_descendant_inheritance
test_keepalive_style_cleanup_inherits_and_releases_lock
test_lifecycle_lock_rejects_stale_lock
test_source_fingerprint_excludes_workspace_build_caches
test_atomic_build_rejects_non_workspace_output
test_parallel_build_wait_propagates_failure
test_atomic_build_does_not_replace_binary_on_failure
test_atomic_build_embeds_identity_and_replaces_binary
test_interrupted_build_removes_temporary_binary
test_atomic_build_rejects_source_change
test_all_go_health_routes_use_module_lifecycle

echo "PASS: dev lifecycle lock and atomic build tests"
