#!/bin/bash

# Node dependency installation policy shared by development lifecycle scripts.
# A checked-in lockfile is an immutable build input, so lifecycle commands use
# one installation route only: npm ci. Lockfile creation belongs to explicit
# dependency-maintenance workflows, never service startup.

addp_install_node_dependencies() {
  local directory=$1
  shift

  if [ ! -f "$directory/package.json" ]; then
    echo "Node 依赖目录缺少 package.json: $directory" >&2
    return 1
  fi
  if [ ! -f "$directory/package-lock.json" ]; then
    echo "Node 依赖目录缺少 package-lock.json，开发生命周期拒绝生成锁文件: $directory" >&2
    return 1
  fi
  if ! command -v npm >/dev/null 2>&1; then
    echo "缺少 npm，无法安装 Node 依赖: $directory" >&2
    return 1
  fi

  (cd "$directory" && npm ci "$@")
}
