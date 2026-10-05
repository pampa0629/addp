#!/bin/bash
# dev-lifecycle-and-build.sh - 开发环境生命周期锁与原子构建回归测试

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOCK_SCRIPT="${ROOT_DIR}/scripts/dev/lifecycle-lock.sh"
BUILD_SCRIPT="${ROOT_DIR}/scripts/dev/build-identity.sh"
PORT_SCRIPT="${ROOT_DIR}/scripts/dev/ports.sh"
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
  for _ in {1..500}; do
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
  grep -qx 'inherited=1' "$child_state" || fail "keepalive child did not use inherited lock: $(cat "$child_state")"
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
  for _ in {1..500}; do
    compgen -G "$workspace/.dev-bins/.tmp/addp-service.*" >/dev/null && break
    sleep 0.02
  done
  compgen -G "$workspace/.dev-bins/.tmp/addp-service.*" >/dev/null || {
    kill "$build_pid" 2>/dev/null || true
    wait "$build_pid" 2>/dev/null || true
    fail "interrupted build did not create temporary binary path"
  }
  if ! wait_for_path "$workspace/wrapper.pid"; then
    kill "$build_pid" 2>/dev/null || true
    wait "$build_pid" 2>/dev/null || true
    fail "interrupted build wrapper pid was not recorded"
  fi
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
  local route found=0
  while IFS= read -r -d '' route; do
    [ -f "$ROOT_DIR/$route" ] || continue
    [[ "$route" == *_test.go ]] && continue
    if grep -Fq 'RegisterHealthRoutes(router)' "$ROOT_DIR/$route"; then
      found=1
      grep -Fq 'modulelifecycle' "$ROOT_DIR/$route" || fail "health route does not use modulelifecycle: $route"
    fi
  done < <(git -C "$ROOT_DIR" ls-files -z --cached --others --exclude-standard -- '*.go')
  [ "$found" -eq 1 ] || fail "no Go health routes found"
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
    ("single", ["-system"], False),
    ("swagger-failure", ["-all"], True),
    ("swagger-failure-single", ["-system"], True),
    ("swagger-failure-legacy-override", ["-all"], True),
    ("coverage-failure", ["-all"], True),
    ("coverage-failure-single", ["-system"], True),
    ("coverage-failure-selected", ["-system", "-asset", "-meta"], True),
    ("coverage-error", ["-all"], True),
    ("coverage-error-single", ["-system"], True),
    ("spark-prepare-failure", ["-all"], True),
):
    root = temporary / ("restart-" + name)
    dev = root / "scripts/dev"
    dev.mkdir(parents=True)
    for filename in ("restart.sh", "lifecycle-lock.sh", "node-dependencies.sh", "jupyter-env.sh"):
        shutil.copy2(repository / "scripts/dev" / filename, dev / filename)
    (dev / "ports.sh").write_text('addp_dev_load_saved_ports() { :; }\n')
    (dev / "spark-workflow.sh").write_text('addp_prepare_spark_workflow() { '
        'echo spark-preflight >> "$FIXTURE_ROOT/events"; [ "$FAIL_SPARK_PREPARE" = 0 ]; }\n')
    (root / ".env").write_text('ADDP_HOST_NODE_NAME=fixture-host-node\nADDP_HOST_NODE_IPS=192.0.2.7,2001:db8::1\n')

    infra = root / "scripts/infra"
    infra.mkdir(parents=True)
    (infra / "ports.sh").write_text('addp_infra_ready() { return 1; }\n')

    def script(relative, body):
        path = root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("#!/bin/bash\nset -e\n" + body)
        path.chmod(0o755)

    # Real restart orchestration, but no process termination, service or database access.
    script("tools/pkill", 'echo "pkill $*" >> "$FIXTURE_ROOT/events"\n')
    script("tools/go", 'echo "$*" >> "$FIXTURE_ROOT/go-calls"\n')
    script("scripts/dev/stop.sh", 'echo stop >> "$FIXTURE_ROOT/events"\n')
    script("scripts/dev/start.sh", '''
ROOT_DIR="$FIXTURE_ROOT"
source "$ROOT_DIR/scripts/dev/lifecycle-lock.sh"
addp_acquire_lifecycle_lock start
[ "$ADDP_HOST_NODE_NAME" = fixture-host-node ]
[ "$ADDP_HOST_NODE_IPS" = "192.0.2.7,2001:db8::1" ]
echo start >> "$ROOT_DIR/events"
''')
    script("scripts/swagger/gen-swagger.sh", '''
echo "generate $*" >> "$FIXTURE_ROOT/events"
if [ "$FAIL_SWAGGER" = 1 ]; then exit 7; fi
echo generated >> "$FIXTURE_ROOT/events"
''')
    script("scripts/swagger/check-route-coverage.sh", '''
echo "coverage $*" >> "$FIXTURE_ROOT/events"
exit "$FAIL_COVERAGE"
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
        FIXTURE_ROOT=str(root), FAIL_SWAGGER=str(int(name.startswith('swagger-failure'))),
        FAIL_COVERAGE='2' if name.startswith('coverage-error') else '1' if name.startswith('coverage-failure') else '0',
        FAIL_SPARK_PREPARE=str(int(name == 'spark-prepare-failure')),
        ALLOW_SWAGGER_FAILURE="1", SWAGGER_COVERAGE_WARN_ONLY="1", MEILISEARCH_PORT="17700", SERVICE_HOST="localhost",
    )
    result = subprocess.run(["bash", str(dev / "restart.sh"), *args], env=env,
                            text=True, capture_output=True, timeout=30)
    assert (result.returncode != 0) == failed, result.stdout + result.stderr
    assert not (root / "go-calls").exists(), "restart must not clear the global Go cache"
    assert all(source.stat().st_mtime_ns == 1_600_000_000_000_000_000 for source in sources), \
        "restart must not touch source timestamps"
    events = (root / "events").read_text().splitlines()
    assert not any(event.startswith("pkill ") for event in events), \
        "global restart must delegate shutdown to stop.sh without killing Python processes first: " + repr(events)
    target = "all" if args in ([], ["-all"]) else "system" if args == ["-system"] else "system asset meta"
    expected = ([] if args == ['-system'] else ['spark-preflight']) + ["stop", "generate " + target]
    if name.startswith('coverage-'):
        expected += ["generated", "coverage " + target]
    elif not failed:
        expected += ["generated", "coverage " + target, "start"]
    if name == 'spark-prepare-failure':
        expected = ['spark-preflight']
    for binary in ("system", "asset", "meta", "meta-worker", "gateway"):
        assert (bins / ("addp-" + binary)).read_text() == "old binary", "restart must preserve " + binary
    assert events == expected, events
    assert not (root / ".dev-state/lifecycle.lock").exists(), "restart leaked its lock"
print("PASS: restart preserves cache, batches Swagger and stops on Swagger generation or coverage failure")
PY
}

test_stop_keeps_system_available_for_deregistration() {
  python3 - "$ROOT_DIR" "$TEST_ROOT" <<'PY'
import os
from pathlib import Path
import shutil
import subprocess
import sys

repository, temporary = map(Path, sys.argv[1:])
for mode in ('pid', 'listeners', 'launchd', 'launchd-failure'):
    root = temporary / ('stop-order-' + mode)
    (root / 'scripts/dev').mkdir(parents=True)
    (root / 'scripts/utils').mkdir()
    (root / 'scripts/utils/colors.sh').write_text('YELLOW= RED= GREEN= NC=\n')
    for name in ('stop.sh', 'ports.sh', 'lifecycle-lock.sh'):
        shutil.copy2(repository / 'scripts/dev' / name, root / 'scripts/dev' / name)
    (root / '.dev-pids').mkdir()
    for pid in (101, 102, 103, 105):
        (root / str(pid)).touch()
    if mode != 'listeners':
        for name, pid in (('system', 101), ('meta-worker', 102), ('manager-backend', 103)):
            (root / '.dev-pids' / (name + '.pid')).write_text(str(pid))
    else:
        # Workers without listeners still use their PID file.
        (root / '.dev-pids/meta-worker.pid').write_text('102')
    hooks = root / 'hooks.sh'
    hooks.write_text(r'''
uname() { printf 'Darwin\n'; }
sleep() { :; }
ps() {
  [ -f "$FIXTURE_ROOT/$2" ] || return 1
  if [[ " $* " == *" -o command= "* ]]; then
    case "$2" in
      101)
        if [ "$FIXTURE_MODE" = listeners ]; then
          printf '%s/.dev-bins/addp-system\n' "$FIXTURE_ROOT"
        else
          printf '.dev-bins/addp-system\n'
        fi ;;
      202) printf '/other-workspace/server\n' ;;
      *) printf '%s/.dev-bins/addp-manager\n' "$FIXTURE_ROOT" ;;
    esac
  fi
}
lsof() {
  if [[ " $* " == *" -d cwd "* ]]; then
    if [ "$4" = 202 ]; then printf 'n/other-workspace\n'; else printf 'n%s\n' "$FIXTURE_ROOT"; fi
  else
    echo "$*" >> "$FIXTURE_ROOT/scans"
    # Duplicate owned listeners, a foreign listener and an exited listener.
    printf 'p101\np103\np105\np105\np202\np303\n'
  fi
}
finish_module() {
  [ -f "$FIXTURE_ROOT/101" ] || echo unavailable >> "$FIXTURE_ROOT/failures"
  echo "deregister $1" >> "$FIXTURE_ROOT/events"
  rm -f "$FIXTURE_ROOT/$1"
}
kill() {
  local pid="${@: -1}"
  echo "signal $*" >> "$FIXTURE_ROOT/events"
  if [ "$pid" = 101 ]; then
    for dependent in 102 103 105; do
      [ ! -f "$FIXTURE_ROOT/$dependent" ] || echo premature >> "$FIXTURE_ROOT/failures"
    done
    [ -f "$FIXTURE_ROOT/runtime-stopped" ] || echo runtime-premature >> "$FIXTURE_ROOT/failures"
    rm -f "$FIXTURE_ROOT/101"
  elif [ "$pid" = 103 ] && [ "$1" != '-KILL' ]; then
    # A module can exceed the grace period; it must be forced before System.
    echo slow >> "$FIXTURE_ROOT/events"
  elif [ "$1" = '-KILL' ]; then
    rm -f "$FIXTURE_ROOT/$pid"
  else
    finish_module "$pid"
  fi
}
launchctl() {
  case "$1" in
    list)
      case "$FIXTURE_MODE" in launchd*) printf '101 0 com.addp.codex.system\n102 0 com.addp.codex.worker\n202 0 com.addp.codex.foreign\n';; esac ;;
    print)
      case "$2" in
        *.system) [ -f "$FIXTURE_ROOT/101" ] && printf '%s/.dev-bins/addp-system\n' "$FIXTURE_ROOT" ;;
        *.worker) [ -f "$FIXTURE_ROOT/102" ] && printf '%s/.dev-bins/addp-meta-worker\n' "$FIXTURE_ROOT" ;;
        *.foreign) printf '/other-workspace/server\n' ;;
      esac ;;
    bootout)
      echo "bootout $2" >> "$FIXTURE_ROOT/events"
      case "$2" in
        *.system) kill 101 ;;
        *.worker) [ "$FIXTURE_MODE" != launchd-failure ] || return 1; finish_module 102 ;;
        *) echo foreign >> "$FIXTURE_ROOT/failures" ;;
      esac ;;
  esac
}
docker() {
  case "$1" in
    inspect)
      [ "${@: -1}" = pointcloud-workflow-engine ] || return 1
      printf 'addp-runtimes|pointcloud-workflow-engine|%s\n' "$FIXTURE_ROOT" ;;
    stop)
      [ -f "$FIXTURE_ROOT/101" ] || echo container-unavailable >> "$FIXTURE_ROOT/failures"
      echo "docker $*" >> "$FIXTURE_ROOT/events"
      touch "$FIXTURE_ROOT/runtime-stopped" ;;
    rm)
      [ -f "$FIXTURE_ROOT/runtime-stopped" ] || echo container-force >> "$FIXTURE_ROOT/failures"
      echo "docker $*" >> "$FIXTURE_ROOT/events" ;;
  esac
}
''')
    (root / '202').touch()
    env = {k: v for k, v in os.environ.items() if not k.startswith('ADDP_LIFECYCLE_')}
    env.update(BASH_ENV=str(hooks), FIXTURE_ROOT=str(root), FIXTURE_MODE=mode)
    result = subprocess.run(['bash', str(root / 'scripts/dev/stop.sh')],
                            env=env, capture_output=True, text=True, timeout=15)
    assert (result.returncode != 0) == (mode == 'launchd-failure'), result.stdout + result.stderr
    assert not (root / 'failures').exists(), (mode, (root / 'failures').read_text(), result.stdout)
    events = (root / 'events').read_text().splitlines()
    assert 'deregister 102' in events and 'deregister 105' in events, events
    assert any(event == 'signal -KILL 103' for event in events), events
    assert sum('105' in event and event.startswith('signal ') for event in events) == 1, events
    assert (root / '202').exists(), 'foreign listener was terminated'
    scans = (root / 'scans').read_text().splitlines()
    assert len(scans) == 1 and '-sTCP:LISTEN' in scans[0] and '-Fp' in scans[0], scans
    assert '-iTCP:8000,8180,8081' in scans[0], scans
    assert not (root / '.dev-state/lifecycle.lock').exists(), 'stop leaked lifecycle lock'
print('PASS: System stops last; deregistration, bounded TERM/KILL, Runtime, launchd and foreign listeners')
PY
}


test_start_loads_runtime_ownership_in_all_profiles() {
  python3 - "$ROOT_DIR" "$TEST_ROOT" <<'PY'
from pathlib import Path
import os
import subprocess
import sys

root, temporary = map(Path, sys.argv[1:])
source = (root / 'scripts/dev/start.sh').read_text()
start = source.index('\nADDP_INFRA_PORT_SCOPE=core addp_infra_read_actual_ports\n')
end = source.index('\n# 2. 启动 System Backend', start)
phase = source[start:end]
for online, hosted in [('0', '0'), ('1', '0'), ('1', '1')]:
    workspace = temporary / f'start-ownership-{online}-{hosted}'
    workspace.mkdir()
    environment = {**os.environ, 'ROOT_DIR': str(workspace), 'SCRIPT_DIR': str(root / 'scripts/dev'),
                   'ADDP_ONLINE_HOST': online, 'ADDP_ONLINE_HOSTED': hosted,
                   'POINTCLOUD_WORKFLOW_PORT': '18102', 'ADDP_DEV_PORTS_RESOLVED': ''}
    script = '''
set -euo pipefail
addp_infra_read_actual_ports() { POSTGRES_PORT=25432 REDIS_PORT=26379 MINIO_API_PORT=29000; }
addp_runtime_logs_enabled() { return 0; }
generate_service_urls() { :; }
lsof() { return 1; }
docker() { return 1; }
'''
    script += phase + '''
for helper in addp_dev_remove_owned_container addp_dev_owned_listener addp_dev_resolve_ports; do
  declare -F "$helper" >/dev/null || { echo "missing lifecycle helper: $helper" >&2; exit 1; }
done
addp_dev_remove_owned_container pointcloud-workflow-engine
docker() {
  case "$1" in
    inspect) printf 'addp-runtimes|pointcloud-workflow-engine|%s\\n' "$ROOT_DIR" ;;
    rm) touch "$ROOT_DIR/removed-container" ;;
    *) return 2 ;;
  esac
}
addp_dev_remove_owned_container pointcloud-workflow-engine
[ -f "$ROOT_DIR/removed-container" ]
if [ "$ADDP_ONLINE_HOST" = 1 ]; then
  [ "$POINTCLOUD_WORKFLOW_PORT" = 18102 ]
  [ ! -e "$ROOT_DIR/.dev-state/ports.env" ]
else
  [ "$ADDP_DEV_PORTS_RESOLVED" = 1 ]
  [ -f "$ROOT_DIR/.dev-state/ports.env" ]
fi
'''
    result = subprocess.run(['bash', '-c', script], env=environment, cwd=workspace,
                            capture_output=True, text=True, timeout=15)
    assert result.returncode == 0, (online, hosted, result.stdout, result.stderr)
print('PASS: startup loads Runtime ownership helpers in local and Online profiles without resolving Online ports')
PY
}

test_dev_port_resolution() {
  local workspace="${TEST_ROOT}/dev-ports"
  mkdir -p "$workspace"
  ROOT_DIR="$workspace" PORT_SCRIPT="$PORT_SCRIPT" bash -c '
    set -e
    source "$PORT_SCRIPT"
    addp_dev_port_busy() { [ "$1" = 8000 ] || [ "$1" = 5170 ]; }
    addp_dev_owned_listener() { return 1; }
    addp_dev_resolve_ports >/dev/null
    [ "$GATEWAY_PORT" = 18000 ]
    [ "$CONSOLE_FE_PORT" = 15170 ]
    [ "$PUBLIC_API_URL" = http://localhost:18000 ]
    [ "$CONSOLE_URL" = http://localhost:15170 ]
    [[ "$VITE_ADDP_FRONTEND_PORTS" == *"console:15170"* ]]
    [[ "$ALLOWED_ORIGINS" == *"http://localhost:15170"* ]]
    grep -qx "GATEWAY_PORT=18000" "$ROOT_DIR/.dev-state/ports.env"
    addp_dev_owned_listener() { [ "$1:$2" = gateway:18000 ]; }
    addp_dev_resolve_ports >/dev/null
    [ "$GATEWAY_PORT" = 18000 ]
    docker() {
      if [ "$1" = inspect ] && [ "$2" = --format ]; then
        printf "%s\n" "$container_labels"
      elif [ "$1" = rm ]; then
        touch "$ROOT_DIR/removed-container"
      fi
    }
    container_labels="foreign|geopython-workflow-engine|$ROOT_DIR"
    if addp_dev_remove_owned_container geopython-workflow-engine 2>/dev/null; then exit 1; fi
    [ ! -e "$ROOT_DIR/removed-container" ]
    container_labels="addp-runtimes|geopython-workflow-engine|$ROOT_DIR"
    addp_dev_remove_owned_container geopython-workflow-engine
    [ -e "$ROOT_DIR/removed-container" ]
  ' || fail "development port resolution did not propagate or preserve the selected port"
}

test_dev_real_listener_collision() {
  local workspace="${TEST_ROOT}/dev-real-listener-collision"
  mkdir -p "$workspace"
  python3 - "$PORT_SCRIPT" "$workspace" <<'PY'
import os
from pathlib import Path
import socket
import subprocess
import sys

port_script, workspace = sys.argv[1:]
listeners = []
for _ in range(2):
    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    listener.listen()
    listeners.append(listener)
gateway_busy, console_busy = (listener.getsockname()[1] for listener in listeners)
environment = dict(os.environ, ROOT_DIR=workspace, PORT_SCRIPT=port_script,
                   GATEWAY_PORT=str(gateway_busy), CONSOLE_FE_PORT=str(console_busy),
                   SERVICE_HOST="localhost", ALLOWED_ORIGINS="")
script = '''
set -euo pipefail
source "$PORT_SCRIPT"
addp_dev_port_specs() {
  printf 'gateway GATEWAY_PORT 8000\\nconsole-frontend CONSOLE_FE_PORT 5170\\n'
}
addp_dev_resolve_ports >/dev/null
printf '%s\\n' "$GATEWAY_PORT" "$CONSOLE_FE_PORT" "$PUBLIC_API_URL" "$CONSOLE_URL" "$VITE_ADDP_FRONTEND_PORTS" "$ALLOWED_ORIGINS"
'''
result = subprocess.run(["bash", "-c", script], env=environment,
                        capture_output=True, text=True, timeout=15)
assert result.returncode == 0, result.stdout + result.stderr
gateway, console, api_url, console_url, frontend_ports, origins = result.stdout.splitlines()
assert 18000 <= int(gateway) < 18100 and int(gateway) != gateway_busy, result.stdout
assert 15170 <= int(console) < 15270 and int(console) != console_busy, result.stdout
assert api_url == f"http://localhost:{gateway}", api_url
assert console_url == f"http://localhost:{console}", console_url
assert frontend_ports == f"console:{console}", frontend_ports
assert f"http://localhost:{console}" in origins.split(","), origins
state = (Path(workspace) / ".dev-state/ports.env").read_text()
assert f"GATEWAY_PORT={gateway}\n" in state, state
assert f"CONSOLE_FE_PORT={console}\n" in state, state
for listener in listeners:
    listener.close()
print("PASS: real TCP listeners trigger free Gateway and Vite ports with propagated origins")
PY
}

test_dev_owned_listener_matches_recorded_pid() {
  local workspace="${TEST_ROOT}/dev-owned-listener"
  mkdir -p "$workspace/.dev-pids"
  ROOT_DIR="$workspace" PORT_SCRIPT="$PORT_SCRIPT" bash -c '
    set -e
    source "$PORT_SCRIPT"
    printf "%s\n" "$$" > "$ROOT_DIR/.dev-pids/system.pid"
    lsof() { printf "%s\n" "$$"; }
    addp_dev_owned_listener system 8180
  ' || fail "development port ownership did not match the recorded listener PID"
}

test_dev_runtime_owned_listeners_match_pidfiles() {
  local workspace="${TEST_ROOT}/dev-runtime-owned-listeners"
  mkdir -p "$workspace/.dev-pids" "$workspace/.dev-state"
  ROOT_DIR="$workspace" PORT_SCRIPT="$PORT_SCRIPT" bash -c '
    set -euo pipefail
    source "$PORT_SCRIPT"
    lsof() { printf "%s\n" "$$"; }
    checked=0
    while read -r name variable preferred; do
      case "$variable" in
        MATH_WORKFLOW_PORT) pidfile=math-workflow-engine ;;
        JUPYTER_API_PORT) pidfile=jupyter-api-server ;;
        MODEL3D_WORKFLOW_PORT) pidfile=model3d-workflow-engine ;;
        SPARK_WORKFLOW_PORT) pidfile=spark-workflow-engine ;;
        *) continue ;;
      esac
      printf "%s\n" "$$" > "$ROOT_DIR/.dev-pids/${pidfile}.pid"
      addp_dev_owned_listener "$name" "$preferred"
      printf "%s=%s\n" "$variable" "$((preferred + 10000))" >> "$ROOT_DIR/.dev-state/ports.env"
      checked=$((checked + 1))
    done < <(addp_dev_port_specs)
    [ "$checked" -eq 4 ]
    addp_dev_load_saved_ports
    [ "$MATH_WORKFLOW_PORT" -eq 18089 ]
    [ "$JUPYTER_API_PORT" -eq 18097 ]
    [ "$MODEL3D_WORKFLOW_PORT" -eq 18101 ]
  ' || fail "runtime port ownership did not match the startup PID files"
}

test_spark_native_lifecycle() {
  ROOT_DIR="$ROOT_DIR" TEST_ROOT="$TEST_ROOT" python3 - <<'PY_SPARK'
import os
from pathlib import Path
import socket
import subprocess
import sys
root = Path(os.environ['ROOT_DIR'])
work = Path(os.environ['TEST_ROOT']) / 'spark-native'
runtime = work / 'engines/spark-workflow'
(runtime / 'venv/bin').mkdir(parents=True)
java = work / 'jdk/bin'
java.mkdir(parents=True)
(java / 'java').write_text('#!/bin/bash\necho \'openjdk version "11.0.32"\' >&2\n')
(java / 'java').chmod(0o755)
python = runtime / 'venv/bin/python'
python.write_text('#!' + sys.executable + '''
import os, sys
if len(sys.argv)>2 and sys.argv[1]=='-c':
    if 'sys.version_info' in sys.argv[2]: sys.exit(int(os.environ.get('FAIL_PYTHON', '0')))
    if 'import api_server' in sys.argv[2]: sys.exit(int(os.environ.get('FAIL_IMPORT', '0')))
os.execv(sys.executable, [sys.executable]+sys.argv[1:])
''')
python.chmod(0o755)
(runtime / 'api_server.py').write_text('''
import http.server, os, sys
assert os.environ['WORKFLOW_BIND_HOST']=='127.0.0.1'
assert os.environ['SPARK_WORKFLOW_SHARED_HOST']=='127.0.0.1'
assert os.environ['JAVA_HOME']==os.environ['EXPECTED_JAVA']
if os.environ.get('FAIL_START')=='1': sys.exit(1)
http.server.HTTPServer(('127.0.0.1',int(os.environ['PORT'])), http.server.SimpleHTTPRequestHandler).serve_forever()
''')
(runtime / 'health').write_text('ok')
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0)); port = str(sock.getsockname()[1])
launcher = '''
set -euo pipefail
source "$SOURCE_ROOT/scripts/dev/lifecycle-lock.sh"
source "$SOURCE_ROOT/scripts/dev/ports.sh"
source "$SOURCE_ROOT/scripts/dev/spark-workflow.sh"
addp_sync_python_dependencies() {
  [ "$1" = "$ROOT_DIR" ] && [ "$2" = "$ROOT_DIR/engines/spark-workflow" ] || return 2
  echo sync >> "$ROOT_DIR/trace"
  [ "${FAIL_SYNC:-0}" = 0 ]
}
addp_start_spark_workflow
pid=$(cat "$ROOT_DIR/.dev-pids/spark-workflow-engine.pid")
trap 'kill -TERM "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true' EXIT
addp_dev_owned_listener spark-workflow-engine "$SPARK_WORKFLOW_PORT"
'''
env = dict(os.environ, SOURCE_ROOT=str(root), ROOT_DIR=str(work), JAVA_HOME=str(java.parent),
           EXPECTED_JAVA=str(java.parent), SPARK_WORKFLOW_SHARED_HOST='127.0.0.1', SPARK_WORKFLOW_PORT=port, SPARK_MODE='')
result = subprocess.run(['bash', '-c', launcher], env=env, capture_output=True, text=True, timeout=15)
assert result.returncode == 0, (result.stdout,result.stderr)
assert (work / 'trace').read_text().splitlines()==['sync']
assert (work / '.dev-pids/spark-workflow-engine.pid').read_text().strip().isdigit()
for flags in ({'FAIL_SYNC':'1'}, {'FAIL_PYTHON':'1'}, {'FAIL_IMPORT':'1'}, {'SPARK_MODE':'local'},
              {'SPARK_WORKFLOW_SHARED_HOST':'invalid.addp.example'}, {'FAIL_START':'1'}):
    (work / '.dev-pids/spark-workflow-engine.pid').unlink(missing_ok=True)
    result=subprocess.run(['bash','-c',launcher],env=dict(env,**flags),capture_output=True,text=True,timeout=15)
    assert result.returncode != 0, (flags,result)
    assert not (work / '.dev-pids/spark-workflow-engine.pid').exists(), flags
# An unrelated HTTP listener must fail before launching a replacement.
with socket.socket() as sock:
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    sock.bind(('127.0.0.1',int(port))); sock.listen()
    result=subprocess.run(['bash','-c',launcher],env=env,capture_output=True,text=True,timeout=15)
    assert result.returncode != 0 and '外部监听者' in result.stderr, result.stderr
    assert not (work / '.dev-pids/spark-workflow-engine.pid').exists()
# Shared host defaults and explicit override retain one address contract.
for kernel, hosted, explicit, expected in [('Darwin','0','','host.docker.internal'),('Linux','1','','127.0.0.1'),
                                           ('Linux','0','','192.0.2.8'),('Darwin','0','shared.example','shared.example')]:
    script='source "$SOURCE_ROOT/scripts/dev/spark-workflow.sh"; uname() { echo "$KERNEL"; }; ip() { echo "1.1.1.1 dev eth0 src 192.0.2.8"; }; addp_spark_shared_host'
    result=subprocess.run(['bash','-c',script],env=dict(env,KERNEL=kernel,ADDP_ONLINE_HOSTED=hosted,SPARK_WORKFLOW_SHARED_HOST=explicit),capture_output=True,text=True)
    assert result.returncode==0 and result.stdout.strip()==expected,result
helper=(root/'scripts/dev/spark-workflow.sh').read_text()
assert 'docker ' not in helper and 'make build-images' not in helper
assert 'addp_sync_python_dependencies' in helper and 'exec "$runtime_dir/venv/bin/python" api_server.py' in helper
assert 'addp_start_spark_workflow' in (root/'scripts/dev/start.sh').read_text()
restart=(root/'scripts/dev/restart.sh').read_text()
assert 'addp_launch_spark_workflow' in restart
assert restart.index('(addp_prepare_spark_workflow) || exit 1') < restart.index('if ! "${SCRIPT_DIR}/stop.sh"')
assert 'supermap-workflow spark-workflow;' not in (root/'scripts/dev/stop.sh').read_text()
print('PASS: native Spark PID/HTTP ownership, dependency/import/DNS failures and one shared address')
PY_SPARK
}

test_hosted_runtime_owned_listener() {
  local workspace="${TEST_ROOT}/hosted-runtime-owned-listener"
  mkdir -p "$workspace"
  ROOT_DIR="$workspace" PORT_SCRIPT="$PORT_SCRIPT" LOCK_SCRIPT="$LOCK_SCRIPT" bash -c '
    set -euo pipefail
    source "$PORT_SCRIPT"
    export ADDP_ONLINE_HOSTED=1
    source "$LOCK_SCRIPT"
    mock_mode=host mock_port=18102 mock_bind=127.0.0.1 mock_foreign=0 mock_labels=owned
    uname() { printf "%s\n" Linux; }
    docker() {
      if [ "$1" = port ]; then printf "%s\n" "8102/tcp -> 0.0.0.0:18102"; return; fi
      case "$3" in
        *Config.Labels*)
          if [ "$mock_labels" = owned ]; then printf "addp-runtimes|pointcloud-workflow-engine|%s\n" "$ROOT_DIR";
          else printf "foreign|pointcloud-workflow-engine|%s\n" "$ROOT_DIR"; fi ;;
        *State.Running*) printf "%s\n" true ;;
        *NetworkMode*) printf "%s\n" "$mock_mode" ;;
        *State.Pid*) printf "%s\n" "$$" ;;
        *Config.Env*) printf "PORT=%s\nWORKFLOW_BIND_HOST=%s\n" "$mock_port" "$mock_bind" ;;
        *) return 2 ;;
      esac
    }
    lsof() { if [ "$mock_foreign" = 1 ]; then printf "%s\n" 1; else printf "%s\n" "$$"; fi; }
    sudo() { [ "$1" = -n ] && [ "$2" = lsof ] || return 2; shift; "$@"; }
    addp_dev_owned_listener pointcloud-workflow 18102
    mock_port=8102
    if addp_dev_owned_listener pointcloud-workflow 18102; then exit 11; fi
    mock_port=18102 mock_bind=0.0.0.0
    if addp_dev_owned_listener pointcloud-workflow 18102; then exit 12; fi
    mock_bind=127.0.0.1 mock_foreign=1
    if addp_dev_owned_listener pointcloud-workflow 18102; then exit 13; fi
    mock_foreign=0 mock_labels=foreign
    if addp_dev_owned_listener pointcloud-workflow 18102; then exit 14; fi
    mock_labels=owned ADDP_ONLINE_HOSTED=0
    if addp_dev_owned_listener pointcloud-workflow 18102; then exit 15; fi
    mock_mode=bridge
    addp_dev_owned_listener pointcloud-workflow 18102
  ' || fail "Hosted Runtime ownership must match network, binding, port, labels and listener PID"
}

test_runtime_host_port_advertisement() {
  python3 - "$ROOT_DIR" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
start = (root / 'scripts/dev/start.sh').read_text()
for name, variable in (
    ('geopython-workflow', 'GEOPYTHON_WORKFLOW_PORT'),
    ('pointcloud-workflow', 'POINTCLOUD_WORKFLOW_PORT'),
    ('document-workflow', 'DOCUMENT_WORKFLOW_PORT'),
):
    assert f'-e RUNTIME_PUBLIC_PORT="${{{variable}}}"' in start, (name, variable)
    source = (root / f'engines/{name}/api_server.py').read_text()
    assert 'runtime_advertised_port(port)' in source, name
supermap = (root / 'scripts/dev/supermap-workflow.sh').read_text()
assert 'ADDP_DEV_PORTS_RESOLVED' in supermap
print('PASS: container Runtime host ports reach System registration')
PY
}

test_hosted_runtime_network() {
  python3 - "$ROOT_DIR" "$TEST_ROOT" <<'PY'
from pathlib import Path
import ast
import os
import re
import subprocess
import sys
import threading
from unittest.mock import Mock, patch

root, temporary = map(Path, sys.argv[1:])
source = (root / 'scripts/dev/start.sh').read_text()
def function(name):
    match = re.search(r'^' + name + r'\(\) \{.*?^\}', source, re.M | re.S)
    assert match, f'missing startup function: {name}'
    return match.group()

helper = function('configure_workflow_container_network')
for runtime, port in [('geopython', 8099), ('pointcloud', 8102), ('document', 8105)]:
    fixture = temporary / ('runtime-network-' + runtime)
    fixture.mkdir()
    for hosted, kernel in [('1', 'Linux'), ('0', 'Linux'), ('0', 'Darwin'), ('1', 'Darwin')]:
        arguments = fixture / 'arguments'
        arguments.unlink(missing_ok=True)
        public_port = port + 10000
        env = {**os.environ, 'ROOT_DIR': str(fixture), 'ARGUMENTS': str(arguments),
               'ADDP_ONLINE_HOSTED': hosted, 'MOCK_KERNEL': kernel,
               runtime.upper() + '_WORKFLOW_PORT': str(public_port),
               'SYSTEM_BACKEND_PORT': '18180', 'POSTGRES_PORT': '25432',
               'POINTCLOUD_OBJECT_STORE_LOOPBACK_HOST': 'custom-host',
               'DOCUMENT_OBJECT_STORE_LOOPBACK_HOST': 'custom-host'}
        mocks = '''
set -euo pipefail
RED= GREEN= YELLOW= NC=
uname() { printf '%s\\n' "$MOCK_KERNEL"; }
docker() {
  [ "$1" = run ] || return 2
  if [ "$2" = --rm ]; then [ "$3" = --entrypoint ] && [ "$4" = id ] || return 2; printf '%s\\n' 10001; return; fi
  printf '%s\\n' "$@" > "$ARGUMENTS"
  printf '%s\\n' mock-container
}
sudo() { [ "$1" = -n ] && [ "$2" = chown ] && [ "$3" = 10001 ] && [ -d "$4" ]; }
curl() { printf '%s\\n' '{"status":"healthy"}'; }
addp_dev_remove_owned_container() { :; }
ensure_geopython_workflow_image() { :; }
ensure_pointcloud_workflow_image() { :; }
ensure_document_workflow_image() { :; }
'''
        script = mocks + helper + '\n' + function('start_' + runtime + '_workflow_engine_process')
        script += '\nstart_' + runtime + '_workflow_engine_process\n'
        result = subprocess.run(['bash', '-c', script], cwd=fixture, env=env,
                                text=True, capture_output=True, timeout=10)
        if hosted == '1' and kernel != 'Linux':
            assert result.returncode != 0, result
            assert not arguments.exists(), 'unsupported host must fail before docker run'
            continue
        assert result.returncode == 0, (runtime, hosted, kernel, result.stdout, result.stderr)
        args = arguments.read_text().splitlines()
        envs = {args[i + 1].split('=', 1)[0]: args[i + 1].split('=', 1)[1]
                for i, arg in enumerate(args[:-1]) if arg == '-e'}
        assert envs['RUNTIME_PUBLIC_PORT'] == str(public_port), envs
        bind_host = '127.0.0.1' if hosted == '1' else '0.0.0.0'
        assert envs['WORKFLOW_BIND_HOST'] == bind_host, envs
        # Execute the production main block without opening sockets or registering a real engine.
        module = ast.parse((root / f'engines/{runtime}-workflow/api_server.py').read_text())
        main = next(node for node in module.body if isinstance(node, ast.If)
                    and ast.unparse(node.test) == "__name__ == '__main__'")
        app = Mock()
        namespace = {'app': app, 'os': os, 'threading': threading, 'logger': Mock(),
                     'register_to_system_with_retry': lambda: None, 'list_operators': lambda: []}
        with patch.dict(os.environ, envs), patch('threading.Thread'):
            exec(compile(ast.Module(body=main.body, type_ignores=[]), '<runtime-main>', 'exec'), namespace)
        app.run.assert_called_once_with(host=bind_host, port=int(envs['PORT']), debug=False)
        loopback_key = {'geopython': 'GEOPYTHON_WORKFLOW_LOOPBACK_HOST',
                        'pointcloud': 'POINTCLOUD_OBJECT_STORE_LOOPBACK_HOST',
                        'document': 'DOCUMENT_OBJECT_STORE_LOOPBACK_HOST'}[runtime]
        if hosted == '1':
            assert args[args.index('--network') + 1] == 'host', args
            assert '-p' not in args and not any(arg.startswith('--add-host') for arg in args), args
            assert envs['PORT'] == str(public_port), envs
            assert envs['SYSTEM_URL'] == 'http://127.0.0.1:18180', envs
            assert envs[loopback_key] == '127.0.0.1', envs
        else:
            assert '--network' not in args, args
            assert args[args.index('-p') + 1] == f'{public_port}:{port}', args
            assert '--add-host=host.docker.internal:host-gateway' in args, args
            assert envs['PORT'] == str(port), envs
            assert envs['SYSTEM_URL'] == 'http://host.docker.internal:18180', envs
            assert envs[loopback_key] == ('host.docker.internal' if runtime == 'geopython' else 'custom-host'), envs
        if runtime == 'geopython':
            assert envs['POSTGRES_HOST'] == ('127.0.0.1' if hosted == '1' else 'host.docker.internal'), envs
            assert envs['POSTGRES_PORT'] == '25432', envs
            # GeoPython's Docker CMD uses Gunicorn rather than the Flask main block.
            commands = fixture / 'commands'
            commands.mkdir(exist_ok=True)
            gunicorn_args, health_url = fixture / 'gunicorn-args', fixture / 'health-url'
            gunicorn = commands / 'gunicorn'
            gunicorn.write_text('#!/bin/sh\nprintf "%s\\n" "$@" > "$GUNICORN_ARGS"\n')
            gunicorn.chmod(0o755)
            python = commands / 'python'
            python.write_text('#!' + sys.executable + '\nimport os, sys, urllib.request\nfrom pathlib import Path\n'
                              'urllib.request.urlopen = lambda url, timeout: Path(os.environ["HEALTH_URL"]).write_text(url)\n'
                              'if "urllib.request" in sys.argv[2]: exec(sys.argv[2])\n')
            python.chmod(0o755)
            environment = {**os.environ, **envs, 'PATH': str(commands) + ':' + os.environ['PATH'],
                           'GUNICORN_ARGS': str(gunicorn_args), 'HEALTH_URL': str(health_url)}
            entrypoint = root / 'engines/geopython-workflow/container_entrypoint.sh'
            result = subprocess.run(['sh', str(entrypoint)], env=environment, capture_output=True, text=True, timeout=10)
            assert result.returncode == 0, result.stderr
            gunicorn_cli = gunicorn_args.read_text().splitlines()
            assert gunicorn_cli[gunicorn_cli.index('--bind') + 1] == f'{bind_host}:{envs["PORT"]}', gunicorn_cli
            assert health_url.read_text() == f'http://127.0.0.1:{envs["PORT"]}/health'
        if runtime == 'document':
            assert '--read-only' in args and '--cap-drop=ALL' in args, args
            assert '--security-opt=no-new-privileges' in args and '--tmpfs' in args, args
        assert 'com.docker.compose.project=addp-runtimes' in args, args
        assert 'com.docker.compose.project.working_dir=' + str(fixture) in args, args
print('PASS: Hosted Linux Runtime network, ports, service access, ownership and Document restrictions')
PY
}

test_compose_public_port_policy() {
  bash -n "$ROOT_DIR/scripts/local/start.sh" "$ROOT_DIR/scripts/local/stop.sh" "$ROOT_DIR/scripts/local/status.sh" \
    "$ROOT_DIR/scripts/prod/start.sh" "$ROOT_DIR/scripts/prod/health-check.sh" \
    "$ROOT_DIR/scripts/prod/deploy.sh" "$ROOT_DIR/scripts/prod/stop.sh" || fail "Compose lifecycle shell syntax is invalid"
  python3 - "$ROOT_DIR" <<'PY'
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
compose = (root / 'docker-compose.yml').read_text()
runtimes = (root / 'docker-compose.runtimes.yml').read_text()
assert re.search(r'(?m)^name: addp-platform$', compose)
assert re.search(r'(?m)^name: addp-runtimes$', runtimes)
assert re.search(r'(?m)^    external: true$', runtimes)
published = []
for block in re.split(r'(?=^  [a-z0-9-]+:\n)', compose, flags=re.M):
    service = re.match(r'^  ([a-z0-9-]+):\n', block)
    if service and re.search(r'(?m)^    ports:\s*$', block):
        published.append(service.group(1))
assert published == ['nginx'], published
assert not re.search(r'(?m)^    ports:\s*$', runtimes), 'Runtime must not publish host ports'
for variable in ('PUBLIC_API_URL', 'CONSOLE_URL', 'MONITOR_CONSOLE_BASE_URL'):
    assert f'{variable}=${{ADDP_PUBLIC_ORIGIN:-http://localhost:${{NGINX_PORT:-80}}}}' in compose, variable
for folder in ('local', 'prod'):
    start = (root / 'scripts' / folder / 'start.sh').read_text()
    stop = (root / 'scripts' / folder / 'stop.sh').read_text()
    assert 'docker compose -f docker-compose.runtimes.yml up' in start, folder
    assert re.search(r'docker compose -f docker-compose\.runtimes\.yml(?: --env-file \.env)? down', stop), folder
print('PASS: platform and Runtime Compose projects preserve one public origin and lifecycle')
PY
}

test_local_stop_rejects_volume_deletion() {
  local fixture
  fixture=$(mktemp -d)
  cat > "$fixture/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$ADDP_STOP_MOCK_LOG"
EOF
  chmod +x "$fixture/docker"
  if PATH="$fixture:$PATH" ADDP_STOP_MOCK_LOG="$fixture/docker.log" \
    bash "$ROOT_DIR/scripts/local/stop.sh" --all --volumes > "$fixture/output" 2>&1; then
    rm -rf "$fixture"
    fail "local stop accepted volume deletion"
  fi
  if [ -e "$fixture/docker.log" ]; then
    rm -rf "$fixture"
    fail "local stop contacted Docker before rejecting volume deletion"
  fi
  if PATH="$fixture:$PATH" ADDP_STOP_MOCK_LOG="$fixture/docker.log" \
    bash "$ROOT_DIR/scripts/prod/stop.sh" --volumes > "$fixture/output" 2>&1; then
    rm -rf "$fixture"
    fail "production stop accepted an unsupported volume option"
  fi
  if [ -e "$fixture/docker.log" ]; then
    rm -rf "$fixture"
    fail "production stop contacted Docker before rejecting the volume option"
  fi
  python3 - "$ROOT_DIR/scripts/local/stop.sh" <<'PY'
from pathlib import Path
import sys

source = Path(sys.argv[1]).read_text()
assert 'bash "$SCRIPT_DIR/../infra/down.sh"' in source
assert 'docker compose -f docker-compose.infra.yml down' not in source
assert ' down -v' not in source
PY
  rm -rf "$fixture"
  echo "PASS: local stop rejects volume deletion before Docker and delegates Infra stop"
}

test_prod_compose_init_health() {
  python3 - "$ROOT_DIR" <<'PY'
from pathlib import Path
import re
import subprocess
import sys

source = (Path(sys.argv[1]) / 'scripts/prod/health-check.sh').read_text()
function = re.search(r'^check_service\(\) \{.*?^\}', source, re.M | re.S)
assert function, 'production Compose health checker is missing'
harness = function.group() + '''
docker() {
  if [ "$1" = compose ]; then printf '%s\\n' fixture-container; return; fi
  if [ "$3" = '{{.State.ExitCode}}' ]; then printf '%s\\n' "$fixture_exit_code"; return; fi
  printf '%s\\n' exited
}
failed=0
fixture_exit_code=0
check_service docker-compose.infra.yml redpanda-init
[ "$failed" -eq 0 ] || exit 1
failed=0
fixture_exit_code=1
check_service docker-compose.infra.yml redpanda-init
[ "$failed" -eq 1 ] || exit 1
'''
result = subprocess.run(['bash'], input=harness, text=True, capture_output=True)
assert result.returncode == 0, (result.stdout, result.stderr)
print('PASS: completed Infra initializer succeeds only with exit code zero')
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

test_model3d_python_dependency_sync() {
  python3 - "$ROOT_DIR" "$TEST_ROOT" <<'PY'
import json
import os
from pathlib import Path
import subprocess
import sys

root, temporary = map(Path, sys.argv[1:])
source = (root/'scripts/dev/start.sh').read_text()
start = source.index('start_runtime_model3d() (')
preparation = source[start:source.index('\nensure_model3d_node_dependencies', start)]
workspace = temporary/'model3d workspace with spaces'
engine = workspace/'engines/model3d-workflow'
engine.mkdir(parents=True)
(workspace/'common-python').mkdir()
requirements = engine/'requirements.txt'
requirements.write_text('Flask==3.0.0\nPillow==12.3.0\n')
fake_python = workspace/'fake-python'
fake_python.write_text('''#!/usr/bin/env python3
import json, os, pathlib, shutil, sys
root = pathlib.Path(os.environ['ROOT_DIR'])
args = sys.argv[1:]
with (root/'calls.jsonl').open('a') as stream:
    stream.write(json.dumps(args)+'\\n')
if args[:2] == ['-m', 'venv']:
    target = pathlib.Path(args[2])/'bin/python'
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(root/'fake-python', target)
elif args[:3] == ['-m', 'pip', 'install']:
    if os.environ.get('FAIL_INSTALL') == '1': sys.exit(17)
    requirement = pathlib.Path(args[args.index('-r')+1])
    (root/'installed.txt').write_text(requirement.read_text())
elif args[:3] == ['-m', 'pip', 'check']:
    if os.environ.get('FAIL_CHECK') == '1': sys.exit(19)
elif args == ['--version']:
    print('Python 3.11')
elif args[:1] == ['-c']:
    pass # 旧入口的 Flask/common import 成功，但不代表 Pillow 已安装。
else:
    raise SystemExit('unexpected Python invocation: '+str(args))
''')
fake_python.chmod(0o755)
runner = f'''
set -eu
BLUE= RED= GREEN= NC=
cd "$ROOT_DIR"
source "{root}/scripts/dev/lifecycle-lock.sh"
select_python() {{ printf '%s\\n' "$ROOT_DIR/fake-python"; }}
{preparation}
printf 'ready\\n' > "$ROOT_DIR/prepared"
)
start_runtime_model3d
'''
environment = dict(os.environ, ROOT_DIR=str(workspace), PIP_INDEX_URL='https://index.example/simple', PIP_TRUSTED_HOST='index.example')
calls = workspace/'calls.jsonl'
prepared = workspace/'prepared'

def run(**flags):
    calls.unlink(missing_ok=True)
    prepared.unlink(missing_ok=True)
    result = subprocess.run(['bash', '-c', runner], env=dict(environment, **flags), capture_output=True, text=True)
    arguments = [json.loads(line) for line in calls.read_text().splitlines()] if calls.exists() else []
    return result, arguments

# 新虚拟环境走标准创建和同步；含空格路径、镜像参数保持一个实参。
result, arguments = run()
assert result.returncode == 0 and prepared.exists(), result.stdout+result.stderr
assert any(args[:2] == ['-m', 'venv'] for args in arguments), arguments
installs = [args for args in arguments if args[:3] == ['-m', 'pip', 'install']]
assert len(installs) == 1, arguments
assert installs[0] == ['-m', 'pip', 'install', '-r', str(requirements), '-e', str(workspace/'common-python'), '-i', environment['PIP_INDEX_URL'], '--trusted-host', environment['PIP_TRUSTED_HOST']], installs

# 已有 venv 且旧 import 检查通过，也必须同步新增或升级的 Pillow 声明。
requirements.write_text('Flask==3.0.0\nPillow==12.4.0\n')
(workspace/'installed.txt').write_text('Flask==3.0.0\n')
result, arguments = run()
assert result.returncode == 0 and prepared.exists(), result.stdout+result.stderr
assert (workspace/'installed.txt').read_text() == requirements.read_text(), 'existing venv skipped changed dependency declarations'
assert not any(args[:2] == ['-m', 'venv'] for args in arguments), arguments
assert arguments[-1] == ['-m', 'pip', 'check'], arguments

result, arguments = run(FAIL_INSTALL='1')
assert result.returncode != 0 and not prepared.exists(), result.stdout+result.stderr
assert not any(args[:3] == ['-m', 'pip', 'check'] for args in arguments), arguments
result, arguments = run(FAIL_CHECK='1')
assert result.returncode != 0 and not prepared.exists(), result.stdout+result.stderr

# 局部重启也执行同一同步函数，安装失败不能继续启动进程。
restart_source = (root/'scripts/dev/restart.sh').read_text()
restart_function = restart_source[restart_source.index('restart_model3d_workflow_service() {'):restart_source.index('pointcloud_workflow_source_fingerprint() {')]
runner = f'''
set -eu
cd "$ROOT_DIR"
source "{root}/scripts/dev/lifecycle-lock.sh"
stop_pidfile_process() {{ :; }}
stop_matching_port_process() {{ :; }}
ensure_model3d_node_dependencies() {{ :; }}
start_background_process() {{ printf 'ready\\n' > "$ROOT_DIR/prepared"; }}
wait_http_ready() {{ :; }}
verify_pidfile_process_alive() {{ :; }}
{restart_function}
restart_model3d_workflow_service
'''
result, arguments = run()
assert result.returncode == 0 and prepared.exists(), result.stdout+result.stderr
assert arguments[-1] == ['-m', 'pip', 'check'], arguments
result, arguments = run(FAIL_INSTALL='1')
assert result.returncode != 0 and not prepared.exists(), result.stdout+result.stderr
result, arguments = run(PIP_INDEX_URL='', PIP_TRUSTED_HOST='')
assert result.returncode == 0 and prepared.exists(), result.stdout+result.stderr
installs = [args for args in arguments if args[:3] == ['-m', 'pip', 'install']]
assert len(installs) == 1 and '-i' not in installs[0] and '--trusted-host' not in installs[0], installs
(engine/'venv/bin/python').unlink()
result, arguments = run()
assert result.returncode != 0 and not prepared.exists() and not arguments, result.stdout+result.stderr
print('PASS: Model3D start/restart syncs full declarations in new/existing venv; failures block startup')
PY
}

test_frontend_dependencies_precede_health_wait() {
  python3 - "$ROOT_DIR" "$TEST_ROOT" <<'PY'
import os
from pathlib import Path
import subprocess
import sys
import time

root, temporary = map(Path, sys.argv[1:])
source = (root/'scripts/dev/start.sh').read_text()
launch = source[source.index('# 并发启动所有前端\nfor config'):source.index('  end_start_listener_batch', source.index('# 并发启动所有前端\nfor config'))]
for mode in ('success', 'failure', 'running'):
    workspace = temporary/('frontend-dependencies-'+mode)
    (workspace/'fixture/frontend').mkdir(parents=True)
    (workspace/'logs').mkdir()
    (workspace/'.dev-pids').mkdir()
    (workspace/'.dev-pids/fixture-frontend.pid').write_text('111\n')
    script = r'''
set -eu
cd "$CASE_ROOT"
FRONTEND_CONFIGS=("fixture:8180:fixture/frontend")
FRONTEND_PID_FILE="$CASE_ROOT/pids"
check_service_running() { [ "$MODE" != running ]; }
ensure_node_modules() {
  touch "$CASE_ROOT/install-started"
  until [ -f "$CASE_ROOT/release-install" ]; do sleep 0.02; done
  [ "$MODE" != failure ] || return 19
  touch "$CASE_ROOT/install-completed"
}
npm() { touch "$CASE_ROOT/server-started"; }
''' + launch + '\ntouch "$CASE_ROOT/health-started"\nwait\n'
    process = subprocess.Popen(['bash', '-c', script], env=dict(os.environ, CASE_ROOT=str(workspace), MODE=mode), stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    try:
        if mode != 'running':
            deadline = time.monotonic()+5
            while not (workspace/'install-started').exists() and time.monotonic()<deadline:
                time.sleep(0.02)
            assert (workspace/'install-started').exists(), mode
            time.sleep(0.1)
            assert not (workspace/'health-started').exists(), 'HTTP timeout began while npm ci was still running'
            assert not (workspace/'server-started').exists(), 'Vite started before dependencies completed'
    finally:
        (workspace/'release-install').touch()
        stdout, stderr = process.communicate(timeout=5)
    assert process.returncode == (19 if mode == 'failure' else 0), (mode, stdout, stderr)
    assert (workspace/'health-started').exists() == (mode != 'failure'), mode
    assert (workspace/'server-started').exists() == (mode == 'success'), mode
    assert (workspace/'install-completed').exists() == (mode == 'success'), mode
    if mode == 'running':
        assert not (workspace/'install-started').exists(), 'reused frontend installed dependencies'
print('PASS: frontend dependency preparation precedes HTTP wait; failed installs stop startup; running frontend is reused')
PY
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
for mode in ('empty', 'occupied', 'stale', 'error', 'managed', 'managed-mismatch', 'owned', 'worker', 'dead-process', 'malformed', 'parse'):
    workspace=temporary/('start-ports-'+mode)
    (workspace/'.dev-pids').mkdir(parents=True)
    script=base+helpers+'\n'
    if mode=='managed':
        (workspace/'.dev-pids/managed.pid').write_text('111\n')
        script+='if check_service_running managed 8180; then exit 7; fi\ntouch completed\n'
    elif mode=='managed-mismatch':
        (workspace/'.dev-pids/managed.pid').write_text('111\n')
        script+='addp_dev_owned_listener() { return 1; }\ncheck_service_running managed 8180\ntouch completed\n'
    elif mode=='owned':
        script+='addp_dev_owned_listener() { [ "$1:$2" = "geopython-workflow-engine:18099" ]; }\n'
        script+='if check_service_running geopython-workflow-engine 18099; then exit 7; fi\ntouch completed\n'
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
    failed=mode in ('occupied','error','dead-process','malformed','managed-mismatch')
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
(directory / 'venv/bin').mkdir(parents=True)
(directory / 'venv/bin/python').symlink_to(sys.executable)
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
 path.write_text('#!/bin/bash\ntouch "'+name+'-started"\n')
 path.chmod(0o755)
launcher=fixture/'.dev-bins/addp-runtime-log'
launcher.write_text('#!/bin/bash\n[ "$1" = launch ] || exit 11\n[ "$2" = --module ] || exit 12\n[ "$4" = --role ] && [ "$5" = worker ] || exit 13\n[ "$6" = -- ] || exit 14\nshift 6\nexec "$@"\n')
launcher.chmod(0o755)
# Fast module must launch before the unrelated slow Backend becomes ready.
body='PROJECT_ROOT='+__import__('shlex').quote(str(fixture))+'''\nset -e
require_started_process() { [ "$2" != dead ]; }
check_service_running() { return 0; }
curl() { case "$*" in *:1111/*) return 0;; *:2222/*) [ -f slow-ready ];; *) return 1;; esac; }
wait_for_marker() {
  local marker="$1" i
  for i in {1..500}; do [ -f "$marker" ] && return 0; sleep 0.01; done
  printf 'Worker fixture marker not observed: %s\\n' "$marker" >&2
  return 1
}
'''+helper+'''
trap 'for path in .dev-pids/*.pid; do [ ! -f "$path" ] || kill "$(cat "$path")" 2>/dev/null || true; done' EXIT
start_module_workers slow alive 2222 slow-worker &
slow=$!
start_module_workers fast alive 1111 fast-worker &
fast=$!
wait "$fast"
wait_for_marker fast-worker-started
[ ! -f slow-worker-started ]
touch slow-ready
wait "$slow"
wait_for_marker slow-worker-started
if start_module_workers failed dead 3333 failed-worker; then exit 7; fi
[ ! -f failed-worker-started ]
# Backend remains alive but never ready: bounded timeout also prevents Worker launch.
sleep() { :; }
if start_module_workers failed alive 3333 failed-worker; then exit 8; fi
[ ! -f failed-worker-started ]
'''
result=subprocess.run(['bash','-c',body],cwd=fixture,text=True,capture_output=True,timeout=30)
assert result.returncode==0,(result.returncode,result.stdout,result.stderr,
 {path.name:path.read_text() for path in (fixture/'logs').glob('*.log')})
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

test_stop_keeps_system_available_for_deregistration
test_worker_backend_readiness_order
test_swagger_incremental_generation
test_start_batches_listening_ports
test_frontend_dependencies_precede_health_wait
test_parallel_runtime_startup
test_python_dependency_install_lock
test_model3d_python_dependency_sync
test_start_loads_runtime_ownership_in_all_profiles
test_dev_port_resolution
test_dev_real_listener_collision
test_dev_owned_listener_matches_recorded_pid
test_dev_runtime_owned_listeners_match_pidfiles
test_spark_native_lifecycle
test_hosted_runtime_owned_listener
test_runtime_host_port_advertisement
test_hosted_runtime_network
test_compose_public_port_policy
test_local_stop_rejects_volume_deletion
test_prod_compose_init_health
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
