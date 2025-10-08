#!/usr/bin/env bash

# 启动 System 前端开发服务器，默认监听 0.0.0.0:5173。
# 将日志写入 /tmp/system-frontend.log，并把进程号记录到 .frontend-dev.pid。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRONTEND_DIR="$ROOT_DIR/system/frontend"
PID_FILE="$FRONTEND_DIR/.frontend-dev.pid"
LOG_FILE="${LOG_FILE:-/tmp/system-frontend.log}"
PORT="${PORT:-5173}"
HOST="${HOST:-0.0.0.0}"

echo "准备启动 System 前端 (host=${HOST} port=${PORT})"

cd "$FRONTEND_DIR"

if [[ ! -d node_modules ]]; then
  echo "检测到缺少依赖，执行 npm install ..."
  npm install
fi

if [[ -f "$PID_FILE" ]]; then
  EXISTING_PID="$(cat "$PID_FILE")"
  if ps -p "$EXISTING_PID" > /dev/null 2>&1; then
    echo "已有开发服务器在运行 (PID: $EXISTING_PID)。若需重启请先手动结束该进程。"
    exit 0
  else
    echo "检测到遗留的 PID 文件，进程 $EXISTING_PID 已不存在，继续启动。"
    rm -f "$PID_FILE"
  fi
fi

echo "日志输出到: $LOG_FILE"

nohup npm run dev -- --host "$HOST" --port "$PORT" >>"$LOG_FILE" 2>&1 &
PID=$!

echo "$PID" > "$PID_FILE"

echo "System 前端已启动。"
echo "PID: $PID"
echo "日志: $LOG_FILE"
