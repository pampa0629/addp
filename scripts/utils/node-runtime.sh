#!/bin/bash
# Sole owner of the repository's stable Node major-version policy.

addp_node_required_major() {
  local repository required
  repository=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P) || return 1
  if [ ! -f "$repository/.node-version" ]; then
    echo "Node.js version file is missing: $repository/.node-version" >&2
    return 1
  fi
  required=$(cat "$repository/.node-version") || return 1
  if ! [[ "$required" =~ ^[1-9][0-9]*$ ]]; then
    echo "Invalid Node.js stable major version in $repository/.node-version: ${required:-empty}" >&2
    return 1
  fi
  printf '%s\n' "$required"
}

addp_require_node_runtime() {
  local required executable actual major
  required=$(addp_node_required_major) || return 1
  executable=$(command -v node) || {
    echo "Node.js $required is required by .node-version; node is missing from PATH" >&2
    return 1
  }
  actual=$(node --version) || {
    echo "Cannot read Node.js version from $executable (required by .node-version: $required)" >&2
    return 1
  }
  if [[ "$actual" =~ ^v([0-9]+)\.[0-9]+\.[0-9]+$ ]]; then
    major=${BASH_REMATCH[1]}
    [ "$major" != "$required" ] || return 0
  fi
  echo "Node.js $required is required, found $actual ($executable; source: .node-version). Select the required stable runtime before retrying." >&2
  return 1
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  addp_require_node_runtime
fi
