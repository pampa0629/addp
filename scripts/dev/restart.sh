#!/bin/bash
set -e

echo "🔄 重启 ADDP 开发环境"
echo ""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

cd "${ROOT_DIR}"

if "${SCRIPT_DIR}/stop.sh"; then
  echo ""
  echo "✅ 已停止现有服务"
else
  echo ""
  echo "⚠️ 停止脚本返回非零状态，继续执行启动流程"
fi

# 强制标记所有 Go 源码为已修改，确保 go run 重新编译
echo "🔨 标记源码已修改（触发重新编译）..."
# Touch 所有 .go 文件（包括 internal/service, common 等）
find . -type f -name "*.go" -path "*/backend/*" -exec touch {} \; 2>/dev/null || true
find . -type f -name "*.go" -path "*/common/*" -exec touch {} \; 2>/dev/null || true
find . -type f -name "*.go" -path "*/gateway/*" -exec touch {} \; 2>/dev/null || true
echo "✅ 源码时间戳已更新"
echo ""

# stop.sh 已经等待进程退出，不需要额外 sleep
exec "${SCRIPT_DIR}/start.sh"
