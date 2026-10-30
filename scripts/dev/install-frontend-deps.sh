#!/bin/bash
# Install every registered frontend's locked dependencies; never starts services.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "$ROOT_DIR/scripts/dev/node-dependencies.sh"
addp_require_node_runtime || exit 1

for manifest in "$ROOT_DIR"/*/frontend/package.json; do
  [ -f "$manifest" ] || continue
  directory="$(dirname "$manifest")"
  echo "Installing ${directory#"$ROOT_DIR"/} dependencies..."
  addp_install_node_dependencies "$directory"
done
