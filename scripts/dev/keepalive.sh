#!/bin/bash
# keepalive.sh - 在托管命令环境中前台保活 ADDP 开发服务
#
# 功能:
#   1. 调用现有 start.sh 或 restart.sh 启动开发服务
#   2. 以前台阻塞进程承载后台服务生命周期
#   3. 退出时调用 stop.sh 清理开发服务
#
# 用法:
#   bash scripts/dev/keepalive.sh start [start.sh options]
#   bash scripts/dev/keepalive.sh restart [restart.sh options]
#
# 示例:
#   bash scripts/dev/keepalive.sh restart -orchestrator
#   bash scripts/dev/keepalive.sh start -system

set -euo pipefail

show_usage() {
  echo "用法: $0 {start|restart} [options]"
  echo ""
  echo "说明:"
  echo "  start      调用 scripts/dev/start.sh 后前台保活"
  echo "  restart    调用 scripts/dev/restart.sh 后前台保活"
  echo ""
  echo "示例:"
  echo "  $0 restart -orchestrator"
  echo "  $0 restart -all"
  echo "  $0 start -system"
}

if [ $# -lt 1 ]; then
  show_usage
  exit 1
fi

MODE="$1"
shift

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

cd "${ROOT_DIR}"

source "${SCRIPT_DIR}/lifecycle-lock.sh"
addp_acquire_lifecycle_lock keepalive "${MODE}" "$@"

case "${MODE}" in
  -h|--help)
    show_usage
    exit 0
    ;;
  start)
    "${SCRIPT_DIR}/start.sh" "$@"
    ;;
  restart)
    "${SCRIPT_DIR}/restart.sh" "$@"
    ;;
  *)
    echo "❌ 未知模式: ${MODE}"
    echo ""
    show_usage
    exit 1
    ;;
esac

cleanup() {
  local exit_code=$?
  trap - INT TERM
  echo ""
  echo "🛑 keepalive 退出，停止 ADDP 开发环境..."
  "${SCRIPT_DIR}/stop.sh" || true
  addp_release_lifecycle_lock || true
  exit "$exit_code"
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo ""
echo "✅ ADDP 开发环境已启动，keepalive 正在前台保活。"
echo "   适用于 Codex 等命令结束后会回收后台进程的托管执行环境。"
echo "   按 Ctrl+C 退出并停止开发服务。"

while true; do
  sleep 3600 &
  wait $!
done
