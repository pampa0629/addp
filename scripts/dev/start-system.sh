#!/bin/bash
set -e

echo "🚀 启动 ADDP System 模块（单独模式）"
echo ""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

# 1. 启动基础设施
echo "Step 1/3: 启动基础设施"
bash scripts/infra/up.sh
echo ""

# 2. 启动 System Backend
echo "Step 2/3: 启动 System Backend"
export PROJECT_ROOT="${ROOT_DIR}"
mkdir -p logs .dev-pids .dev-bins

# 加载环境变量
if [ -f ".env" ]; then
    set -a
    source .env
    set +a
fi

# 编译
cd system/backend
go build -o "${PROJECT_ROOT}/.dev-bins/addp-system" cmd/server/main.go
cd "${PROJECT_ROOT}"

# 启动
.dev-bins/addp-system > logs/system-backend.log 2> logs/system-backend-stderr.log &
SYSTEM_PID=$!
echo $SYSTEM_PID > .dev-pids/system.pid
echo "System Backend started (PID: $SYSTEM_PID)"

# 等待就绪
echo -n "等待 System Backend 就绪..."
for i in {1..60}; do
    if curl -f http://localhost:8080/health > /dev/null 2>&1; then
        echo " ✓"
        break
    fi
    echo -n "."
    sleep 1
done
echo ""

# 3. 启动 System Frontend  
echo "Step 3/3: 启动 System Frontend"
cd system/frontend

# 安装依赖(如果需要)
if [ ! -d "node_modules" ]; then
    npm install
fi

# 启动前端
npm run dev -- --host 0.0.0.0 --port 5173 > ../../logs/system-frontend.log 2>&1 &
SYSTEM_FE_PID=$!
echo $SYSTEM_FE_PID > ../../.dev-pids/system-frontend.pid
cd "${PROJECT_ROOT}"

echo -n "等待 System Frontend 就绪..."
for i in {1..60}; do
    if curl -f http://localhost:5173 > /dev/null 2>&1; then
        echo " ✓"
        break
    fi
    echo -n "."
    sleep 1
done
echo ""

echo "========================================"
echo "✓ System 模块启动完成！"
echo "========================================"
echo ""
echo "访问地址:"
echo "  Backend:  http://localhost:8080"
echo "  Frontend: http://localhost:5173"
echo ""
echo "日志文件:"
echo "  Backend:  logs/system-backend.log"
echo "  Frontend: logs/system-frontend.log"
echo ""
echo "停止服务: bash scripts/dev/stop.sh"
