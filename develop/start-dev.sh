#!/bin/bash

# Develop 模块快速启动脚本

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="${SCRIPT_DIR}/backend"
FRONTEND_DIR="${SCRIPT_DIR}/frontend"

echo "🚀 启动 Develop 模块"

# 启动后端
echo "1. 启动后端 (端口 8085)..."
cd "${BACKEND_DIR}"
go run cmd/server/main.go > /tmp/develop-backend.log 2>&1 &
BACKEND_PID=$!
echo "   后端 PID: ${BACKEND_PID}"
echo "   日志: tail -f /tmp/develop-backend.log"

# 等待后端就绪
echo "2. 等待后端就绪..."
sleep 3
for i in {1..30}; do
  if curl -sf http://localhost:8085/health > /dev/null; then
    echo "   ✓ 后端就绪"
    break
  fi
  echo -n "."
  sleep 1
done

# 启动前端
echo "3. 启动前端 (端口 5177)..."
cd "${FRONTEND_DIR}"
npm run dev > /tmp/develop-frontend.log 2>&1 &
FRONTEND_PID=$!
echo "   前端 PID: ${FRONTEND_PID}"
echo "   日志: tail -f /tmp/develop-frontend.log"

echo ""
echo "✅ Develop 模块启动完成!"
echo ""
echo "访问地址:"
echo "  - 后端健康检查: http://localhost:8085/health"
echo "  - 前端界面: http://localhost:5177"
echo ""
echo "停止服务:"
echo "  kill ${BACKEND_PID} ${FRONTEND_PID}"
echo ""
echo "PIDs 已保存到:"
echo "  echo ${BACKEND_PID} > /tmp/develop-backend.pid"
echo "  echo ${FRONTEND_PID} > /tmp/develop-frontend.pid"

echo ${BACKEND_PID} > /tmp/develop-backend.pid
echo ${FRONTEND_PID} > /tmp/develop-frontend.pid
