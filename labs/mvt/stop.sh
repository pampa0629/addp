#!/bin/bash

# MVT 海量空间数据快显系统 - 停止服务脚本
# 停止前后端服务，但保留数据库（PostgreSQL 和 Redis）运行

set -e

echo "=========================================="
echo "  MVT 停止服务脚本"
echo "=========================================="
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 停止后端服务
echo -e "${YELLOW}==> 停止后端服务...${NC}"
BACKEND_PIDS=$(ps aux | grep "go run cmd/server/main.go" | grep -v grep | awk '{print $2}')
if [ -z "$BACKEND_PIDS" ]; then
    echo -e "${GREEN}    ✓ 后端服务未运行${NC}"
else
    for PID in $BACKEND_PIDS; do
        echo -e "    停止后端进程: PID $PID"
        kill $PID 2>/dev/null || true
    done
    # 等待进程完全停止
    sleep 1
    # 检查是否还有残留进程，强制杀死
    REMAINING=$(ps aux | grep "go run cmd/server/main.go" | grep -v grep | awk '{print $2}')
    if [ ! -z "$REMAINING" ]; then
        for PID in $REMAINING; do
            echo -e "    强制停止后端进程: PID $PID"
            kill -9 $PID 2>/dev/null || true
        done
    fi
    echo -e "${GREEN}    ✓ 后端服务已停止${NC}"
fi

# 停止前端服务
echo -e "${YELLOW}==> 停止前端服务...${NC}"
FRONTEND_PIDS=$(ps aux | grep "node.*vite" | grep -v grep | awk '{print $2}')
if [ -z "$FRONTEND_PIDS" ]; then
    echo -e "${GREEN}    ✓ 前端服务未运行${NC}"
else
    for PID in $FRONTEND_PIDS; do
        echo -e "    停止前端进程: PID $PID"
        kill $PID 2>/dev/null || true
    done
    # 等待进程完全停止
    sleep 1
    # 检查是否还有残留进程，强制杀死
    REMAINING=$(ps aux | grep "node.*vite" | grep -v grep | awk '{print $2}')
    if [ ! -z "$REMAINING" ]; then
        for PID in $REMAINING; do
            echo -e "    强制停止前端进程: PID $PID"
            kill -9 $PID 2>/dev/null || true
        done
    fi
    echo -e "${GREEN}    ✓ 前端服务已停止${NC}"
fi

echo ""
echo -e "${GREEN}=========================================="
echo "  服务停止完成"
echo "==========================================${NC}"
echo ""
echo "注意: 数据库服务（PostgreSQL 和 Redis）仍在运行"
echo ""
echo "查看数据库状态: docker-compose ps"
echo "停止数据库服务: make down"
echo ""
