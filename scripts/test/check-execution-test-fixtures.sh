#!/usr/bin/env bash
# check-execution-test-fixtures.sh - Enforce the shared task execution test fixture.
#
# Business module tests must initialize common.task_executions through
# executiontest.EnsureSQLiteStore(db). Only focused tests that verify Common,
# PostgreSQL migrations/authorization, or legacy-table cleanup may own schema SQL.
#
# Usage: bash scripts/test/check-execution-test-fixtures.sh

set -euo pipefail

ROOT_DIR=${ADDP_REPOSITORY_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}

if ! git -C "$ROOT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "Repository root is not a Git work tree: $ROOT_DIR" >&2
    exit 2
fi

readonly TABLE_PATTERN='CREATE[[:space:]]+TABLE[[:space:]]+(IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+)?(["`]?common["`]?[[:space:]]*\.[[:space:]]*)?["`]?task_executions["`]?'

is_allowed_schema_owner() {
    case "$1" in
        common/execution/repository_test.go | \
        manager/backend/internal/repository/database_test.go | \
        system/backend/internal/iam/execution_authorization_postgres_test.go | \
        system/backend/internal/migration/runner_postgres_test.go)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

test_files=()
while IFS= read -r -d '' file; do
    if [ -f "$ROOT_DIR/$file" ]; then
        test_files+=("$file")
    fi
done < <(git -C "$ROOT_DIR" ls-files -z -- '*_test.go')

if [ ${#test_files[@]} -eq 0 ]; then
    echo "No tracked Go test files found under $ROOT_DIR" >&2
    exit 2
fi

violations=()
while IFS= read -r file; do
    if ! is_allowed_schema_owner "$file"; then
        violations+=("$file")
    fi
done < <(
    cd "$ROOT_DIR"
    rg --files-with-matches --multiline --ignore-case "$TABLE_PATTERN" -- "${test_files[@]}" || true
)

if [ ${#violations[@]} -gt 0 ]; then
    echo "Direct task_executions schema definitions are not allowed in business tests:" >&2
    for file in "${violations[@]}"; do
        echo "  $file" >&2
    done
    echo >&2
    echo "Use executiontest.EnsureSQLiteStore(db) instead." >&2
    exit 1
fi

echo "Execution test fixture gate passed (${#test_files[@]} tracked Go test files checked)."
