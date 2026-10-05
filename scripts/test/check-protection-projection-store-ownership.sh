#!/usr/bin/env bash
# check-protection-projection-store-ownership.sh - Keep projection-store schema ownership in Common.

set -euo pipefail

ROOT_DIR=${ADDP_REPOSITORY_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}

for required_command in git rg; do
    command -v "$required_command" >/dev/null 2>&1 || {
        echo "required command is missing: $required_command" >&2
        exit 2
    }
done

if ! git -C "$ROOT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "Repository root is not a Git work tree: $ROOT_DIR" >&2
    exit 2
fi

scratch_dir=$(mktemp -d "${TMPDIR:-/tmp}/addp-projection-ownership.XXXXXX")
trap 'rm -rf "$scratch_dir"' EXIT
if ! git -C "$ROOT_DIR" ls-files --cached --others --exclude-standard -z -- '*.go' '*.sql' > "$scratch_dir/files"; then
    echo "Git file enumeration failed: $ROOT_DIR" >&2
    exit 2
fi

readonly TABLE_PATTERN='protection_projection_(entries|checkpoints|store_migrations)'

is_allowed_schema_owner() {
    case "$1" in
        common/dataprotection/projectionstore/store.go | \
        common/dataprotection/projectionstore/schema.go | \
        common/dataprotection/projectionstore/store_postgres_integration_test.go)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

source_files=()
while IFS= read -r -d '' file; do
    if [ -f "$ROOT_DIR/$file" ]; then
        source_files+=("$file")
    fi
done < "$scratch_dir/files"

if [ ${#source_files[@]} -eq 0 ]; then
    echo "No Go or SQL source files found under $ROOT_DIR" >&2
    exit 2
fi

search_status=0
(
    cd "$ROOT_DIR" || exit 2
    rg --files-with-matches --null "$TABLE_PATTERN" -- "${source_files[@]}"
) > "$scratch_dir/matches" || search_status=$?
if [ "$search_status" -gt 1 ]; then
    echo "ripgrep search failed (exit status $search_status)" >&2
    exit 2
fi

violations=()
while IFS= read -r -d '' file; do
    if ! is_allowed_schema_owner "$file"; then
        violations+=("$file")
    fi
done < "$scratch_dir/matches"

if [ ${#violations[@]} -gt 0 ]; then
    echo "Protection projection store tables may only be defined by common/dataprotection/projectionstore:" >&2
    for file in "${violations[@]}"; do
        echo "  $file" >&2
    done
    echo >&2
    echo "Use Backend-owned projectionstore.Migrate and read-only projectionstore.Open with the Owner schema." >&2
    exit 1
fi

echo "Protection projection store ownership gate passed (${#source_files[@]} Go/SQL files checked)."
