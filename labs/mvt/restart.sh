#!/bin/bash

# MVT 海量空间数据快显系统 - 一键启动/重启脚本
# 作者: ADDP Labs
# 说明: 支持一键拉起或重启后端、前端服务

# 注意: 不使用 set -e，允许部分失败（如停止不存在的进程）

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# PID 文件
BACKEND_PID_FILE=".backend.pid"
FRONTEND_PID_FILE=".frontend.pid"

# 日志文件
LOG_DIR="logs"
mkdir -p "$LOG_DIR"
BACKEND_LOG="$LOG_DIR/backend.log"
FRONTEND_LOG="$LOG_DIR/frontend.log"

# 函数：打印带颜色的消息
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 函数：检查端口占用
check_port() {
    local PORT=$1
    local SERVICE_NAME=$2

    if lsof -Pi :$PORT -sTCP:LISTEN -t >/dev/null 2>&1; then
        print_warning "$SERVICE_NAME 端口 $PORT 已被占用"
        local OLD_PID=$(lsof -Pi :$PORT -sTCP:LISTEN -t)
        print_info "正在清理占用进程 (PID: $OLD_PID)..."
        kill -9 $OLD_PID 2>/dev/null || true
        sleep 1

        # 再次检查
        if lsof -Pi :$PORT -sTCP:LISTEN -t >/dev/null 2>&1; then
            print_error "无法清理端口 $PORT，请手动处理: lsof -ti:$PORT | xargs kill -9"
            exit 1
        fi
        print_success "端口 $PORT 已清理"
    fi
}

# 函数：检查依赖
check_dependencies() {
    print_info "检查依赖..."

    # 检查 Go
    if ! command -v go &> /dev/null; then
        print_error "未安装 Go，请先安装 Go 1.21+"
        exit 1
    fi

    # 检查 Node.js
    if ! command -v node &> /dev/null; then
        print_error "未安装 Node.js，请先安装 Node.js 18+"
        exit 1
    fi

    # 检查 npm
    if ! command -v npm &> /dev/null; then
        print_error "未安装 npm，请先安装 npm"
        exit 1
    fi

    print_success "依赖检查通过"
}

# 函数：检查配置文件（YAML 优先，其次 .env 兼容）
check_env() {
    print_info "检查配置文件..."

    # 仅检查 YAML 配置
    if [ ! -f "backend/config/app.yaml" ]; then
        if [ -f "backend/config/app.yaml.example" ]; then
            print_warning "未找到 backend/config/app.yaml，正在从示例创建..."
            cp backend/config/app.yaml.example backend/config/app.yaml
            print_warning "已创建 backend/config/app.yaml，请填写你的 PostGIS 与 Redis 配置"
        else
            print_error "未提供 backend/config/app.yaml(.example)，请先创建该文件"
            exit 1
        fi
    fi

    print_success "配置文件检查完成"
}

# 函数：停止进程
stop_process() {
    local PID_FILE=$1
    local SERVICE_NAME=$2

    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p "$PID" > /dev/null 2>&1; then
            print_info "停止 $SERVICE_NAME (PID: $PID)..."
            kill "$PID" 2>/dev/null || true
            sleep 2

            # 强制杀死
            if ps -p "$PID" > /dev/null 2>&1; then
                print_warning "强制停止 $SERVICE_NAME..."
                kill -9 "$PID" 2>/dev/null || true
            fi

            print_success "$SERVICE_NAME 已停止"
        else
            print_warning "$SERVICE_NAME 进程不存在 (PID: $PID)"
        fi
        rm -f "$PID_FILE"
    else
        print_info "$SERVICE_NAME 未运行"
    fi
}

# 函数：停止所有服务
stop_all() {
    print_info "停止所有服务..."
    stop_process "$BACKEND_PID_FILE" "后端服务"
    stop_process "$FRONTEND_PID_FILE" "前端服务"
    print_success "所有服务已停止"
}

# 函数：启动后端
start_backend() {
    print_info "启动后端服务..."

    # 检查端口占用
    check_port 8090 "后端服务"

    cd backend

    # 配置 Go 模块代理，避免网络到 sum.golang.google.cn 失败
    export GOPROXY="https://goproxy.cn,https://goproxy.io,https://proxy.golang.org,direct"
    export GOSUMDB="sum.golang.org"
    export GIT_TERMINAL_PROMPT=0

    # 安装依赖
    if [ ! -d "vendor" ]; then
        print_info "下载 Go 依赖..."
        go mod download
    fi

    # 启动后端
    nohup go run cmd/server/main.go > "../$BACKEND_LOG" 2>&1 &
    BACKEND_PID=$!
    echo $BACKEND_PID > "../$BACKEND_PID_FILE"

    cd ..

    print_success "后端服务已启动 (PID: $BACKEND_PID)"
    print_info "日志文件: $BACKEND_LOG"

    # 等待后端启动
    print_info "等待后端服务启动..."
    sleep 3

    # 检查进程是否存活
    if ! ps -p $BACKEND_PID > /dev/null 2>&1; then
        print_error "后端服务启动失败，进程已退出"
        print_info "查看日志: tail -20 $BACKEND_LOG"
        tail -20 "$BACKEND_LOG"
        exit 1
    fi

    # 检查健康
    MAX_RETRIES=15
    RETRY=0
    while [ $RETRY -lt $MAX_RETRIES ]; do
        if curl -s http://localhost:8090/health > /dev/null 2>&1; then
            print_success "后端服务健康检查通过"
            return 0
        fi
        RETRY=$((RETRY+1))
        echo -n "."
        sleep 1
    done

    print_error "后端服务健康检查超时"
    print_info "查看日志: tail -f $BACKEND_LOG"
    tail -20 "$BACKEND_LOG"
    exit 1
}

# 函数：启动前端
start_frontend() {
    print_info "启动前端服务..."

    # 检查端口占用
    check_port 5180 "前端服务"

    cd frontend

    # 强制检查并安装依赖
    if [ ! -d "node_modules" ] || [ ! -f "node_modules/.package-lock.json" ]; then
        print_info "安装 npm 依赖..."
        npm install
    else
        # 即使 node_modules 存在，也检查关键依赖
        if ! npm ls vite > /dev/null 2>&1; then
            print_warning "检测到依赖缺失，重新安装..."
            npm install
        fi
    fi

    # 启动前端
    nohup npm run dev > "../$FRONTEND_LOG" 2>&1 &
    FRONTEND_PID=$!
    echo $FRONTEND_PID > "../$FRONTEND_PID_FILE"

    cd ..

    print_success "前端服务已启动 (PID: $FRONTEND_PID)"
    print_info "日志文件: $FRONTEND_LOG"

    # 等待前端启动
    print_info "等待前端服务启动..."
    sleep 3

    # 检查进程是否存活
    if ! ps -p $FRONTEND_PID > /dev/null 2>&1; then
        print_error "前端服务启动失败，进程已退出"
        print_info "查看日志: tail -20 $FRONTEND_LOG"
        tail -20 "$FRONTEND_LOG"
        exit 1
    fi

    # 等待 Vite 服务就绪（检查日志中的 ready 信号）
    MAX_RETRIES=15
    RETRY=0
    while [ $RETRY -lt $MAX_RETRIES ]; do
        if grep -q "Local:.*5180" "$FRONTEND_LOG" 2>/dev/null; then
            print_success "前端服务已就绪"
            return 0
        fi
        RETRY=$((RETRY+1))
        echo -n "."
        sleep 1
    done

    print_warning "前端服务启动超时，但进程仍在运行"
    print_info "请访问 http://localhost:5180 验证"
}

# 函数：显示状态
show_status() {
    echo ""
    print_info "================================"
    print_info "服务状态"
    print_info "================================"

    local ALL_OK=true

    # 后端状态
    if [ -f "$BACKEND_PID_FILE" ]; then
        BACKEND_PID=$(cat "$BACKEND_PID_FILE")
        if ps -p "$BACKEND_PID" > /dev/null 2>&1; then
            # 进程存在，检查健康
            if curl -s http://localhost:8090/health > /dev/null 2>&1; then
                print_success "后端服务: 运行中 (PID: $BACKEND_PID) ✓"
                print_info "  - API: http://localhost:8090/api/datasources"
                print_info "  - 健康检查: http://localhost:8090/health"
            else
                print_warning "后端服务: 运行中但健康检查失败 (PID: $BACKEND_PID)"
                ALL_OK=false
            fi
        else
            print_error "后端服务: 已停止 (PID 文件存在但进程不存在)"
            ALL_OK=false
        fi
    else
        print_error "后端服务: 未启动"
        ALL_OK=false
    fi

    # 前端状态
    if [ -f "$FRONTEND_PID_FILE" ]; then
        FRONTEND_PID=$(cat "$FRONTEND_PID_FILE")
        if ps -p "$FRONTEND_PID" > /dev/null 2>&1; then
            # 进程存在，检查端口监听
            if lsof -Pi :5180 -sTCP:LISTEN -t >/dev/null 2>&1; then
                print_success "前端服务: 运行中 (PID: $FRONTEND_PID) ✓"
                print_info "  - 访问地址: http://localhost:5180"
            else
                print_warning "前端服务: 运行中但端口未监听 (PID: $FRONTEND_PID)"
                ALL_OK=false
            fi
        else
            print_error "前端服务: 已停止 (PID 文件存在但进程不存在)"
            ALL_OK=false
        fi
    else
        print_error "前端服务: 未启动"
        ALL_OK=false
    fi

    echo ""
    print_info "日志文件:"
    print_info "  - 后端: tail -f $BACKEND_LOG"
    print_info "  - 前端: tail -f $FRONTEND_LOG"
    print_info "================================"

    if [ "$ALL_OK" = false ]; then
        return 1
    fi
    return 0
}

# 函数：显示帮助
show_help() {
    echo "MVT 海量空间数据快显系统 - 一键启动/重启脚本"
    echo ""
    echo "用法: ./restart.sh [命令]"
    echo ""
    echo "命令:"
    echo "  start       启动所有服务（默认）"
    echo "  stop        停止所有服务"
    echo "  restart     重启所有服务"
    echo "  status      显示服务状态"
    echo "  logs        查看实时日志"
    echo "  help        显示帮助信息"
    echo ""
    echo "示例:"
    echo "  ./restart.sh            # 启动所有服务"
    echo "  ./restart.sh restart    # 重启所有服务"
    echo "  ./restart.sh stop       # 停止所有服务"
    echo "  ./restart.sh status     # 查看状态"
}

# 主逻辑
main() {
    local CMD=${1:-start}

    case "$CMD" in
        start)
            check_dependencies
            check_env
            stop_all
            start_backend
            start_frontend
            show_status
            print_success "所有服务已启动成功！"
            print_info "访问 http://localhost:5180 查看前端"
            ;;
        stop)
            stop_all
            ;;
        restart)
            print_info "重启所有服务..."
            stop_all
            sleep 2
            check_dependencies
            check_env
            start_backend
            start_frontend
            show_status
            print_success "所有服务已重启成功！"
            ;;
        status)
            show_status
            ;;
        logs)
            print_info "查看实时日志 (Ctrl+C 退出)..."
            tail -f "$BACKEND_LOG" "$FRONTEND_LOG"
            ;;
        help|--help|-h)
            show_help
            ;;
        *)
            print_error "未知命令: $CMD"
            show_help
            exit 1
            ;;
    esac
}

# 执行主函数
main "$@"
