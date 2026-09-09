#!/bin/bash

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
HELPER="$ROOT_DIR/scripts/dev/node-dependencies.sh"
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/addp-node-dependencies-test.XXXXXX")
LOCKED_DIR="$TEST_ROOT/locked app"
UNLOCKED_DIR="$TEST_ROOT/unlocked app"
NPM_LOG="$TEST_ROOT/npm.log"

cleanup() {
  rm -rf "$TEST_ROOT"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

mkdir -p "$TEST_ROOT/bin" "$LOCKED_DIR" "$UNLOCKED_DIR"
printf '{}\n' > "$LOCKED_DIR/package.json"
printf '{}\n' > "$LOCKED_DIR/package-lock.json"
printf '{}\n' > "$UNLOCKED_DIR/package.json"
cat > "$TEST_ROOT/bin/npm" <<'EOF'
#!/bin/bash
printf '%s|%s\n' "$PWD" "$*" >> "$ADDP_TEST_NPM_LOG"
EOF
chmod +x "$TEST_ROOT/bin/npm"

export PATH="$TEST_ROOT/bin:$PATH"
export ADDP_TEST_NPM_LOG="$NPM_LOG"
# shellcheck source=../dev/node-dependencies.sh
source "$HELPER"

addp_install_node_dependencies "$LOCKED_DIR" --omit=dev
if addp_install_node_dependencies "$UNLOCKED_DIR" --omit=dev 2>/dev/null; then
  fail "directory without package-lock.json was accepted"
fi

LOCKED_COMMAND=$(sed -n '1s/^[^|]*|//p' "$NPM_LOG")
[ "$LOCKED_COMMAND" = "ci --omit=dev" ] ||
  fail "locked dependency install did not use npm ci: $(cat "$NPM_LOG")"
[ "$(wc -l < "$NPM_LOG" | tr -d ' ')" = "1" ] ||
  fail "rejected dependency directory still invoked npm: $(cat "$NPM_LOG")"

if addp_install_node_dependencies "$TEST_ROOT/missing" 2>/dev/null; then
  fail "directory without package.json was accepted"
fi

for lifecycle_script in "$ROOT_DIR/scripts/dev/start.sh" "$ROOT_DIR/scripts/dev/restart.sh"; do
  grep -Fq 'source "${SCRIPT_DIR}/node-dependencies.sh"' "$lifecycle_script" ||
    fail "$(basename "$lifecycle_script") does not source the shared Node dependency policy"
done
grep -Fq 'addp_install_node_dependencies "$dir"' "$ROOT_DIR/scripts/dev/start.sh" ||
  fail "start.sh does not use the shared Node dependency policy"
grep -Fq 'addp_install_node_dependencies "$dir"' "$ROOT_DIR/scripts/dev/restart.sh" ||
  fail "restart.sh does not use the shared Node dependency policy"
if rg -n '\(cd "\$dir" && npm (ci|install)' \
  "$ROOT_DIR/scripts/dev/start.sh" "$ROOT_DIR/scripts/dev/restart.sh"; then
  fail "lifecycle scripts bypass the shared Node dependency policy"
fi

echo "PASS: Node dependency lifecycle policy tests"
