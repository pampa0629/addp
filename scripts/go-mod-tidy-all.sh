#!/usr/bin/env bash
set -euo pipefail

# Resolve repository root relative to this script.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "Collecting Go modules under ${REPO_ROOT}"
modules=()
while IFS= read -r mod_file; do
  modules+=("${mod_file}")
done < <(find "${REPO_ROOT}" \
  -path "${REPO_ROOT}/.git" -prune -o \
  -path "${REPO_ROOT}/.gomodcache" -prune -o \
  -path "*/vendor" -prune -o \
  -name go.mod -print | LC_ALL=C sort)

if [[ ${#modules[@]} -eq 0 ]]; then
  echo "未找到任何 go.mod 文件"
  exit 0
fi

for mod_file in "${modules[@]}"; do
  mod_dir="$(dirname "${mod_file}")"
  rel_dir="${mod_dir#"${REPO_ROOT}/"}"
  echo ""
  echo "==> 进入 ${rel_dir}"
  (cd "${mod_dir}" && go mod tidy)
done

echo ""
echo "所有模块的 go mod tidy 已完成"
