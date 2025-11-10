#!/bin/bash

# ABS Restart Script
# 1. 停止当前运行的 backend / frontend
# 2. 编译 Go 后端
# 3. 启动后端与 Vite 前端

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="${PROJECT_ROOT}/backend"
cd "$PROJECT_ROOT"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

BACKEND_PID=""
FRONTEND_PID=""

resolve_dir_path() {
    local target="$1"
    if [[ -z "$target" ]]; then
        return
    fi
    if [[ "$target" = /* ]]; then
        printf '%s\n' "$target"
    else
        (cd "$BACKEND_DIR" && cd "$target" && pwd)
    fi
}

resolve_file_path() {
    local target="$1"
    if [[ -z "$target" ]]; then
        return
    fi
    if [[ "$target" = /* ]]; then
        printf '%s\n' "$target"
    else
        local dir
        dir=$(dirname "$target")
        local file
        file=$(basename "$target")
        (cd "$BACKEND_DIR" && cd "$dir" && printf '%s/%s\n' "$(pwd)" "$file")
    fi
}

log_step() {
    echo -e "${YELLOW}$1${NC}"
}

wait_for_port() {
    local host="$1"
    local port="$2"
    local retries="${3:-30}"

    for ((i = 1; i <= retries; i++)); do
        if (echo >/dev/tcp/"$host"/"$port") >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done

    return 1
}

stop_active_services() {
    log_step "🛑 正在停止已运行的 ABS 服务..."

    local stopped=false
    local patterns=("cmd/server/main.go" "backend/bin/server" "npm run dev" "vite --host" "vite --port")
    for pattern in "${patterns[@]}"; do
        if pkill -f "$pattern" >/dev/null 2>&1; then
            echo -e "${GREEN}  ⮑ 已根据模式 '${pattern}' 结束进程${NC}"
            stopped=true
        fi
    done

    local ports=(8090 5180)
    for port in "${ports[@]}"; do
        local pids
        pids=$(lsof -ti:$port 2>/dev/null || true)
        if [ -n "$pids" ]; then
            echo -e "${GREEN}  ⮑ 释放端口 ${port}${NC}"
            kill $pids >/dev/null 2>&1 || true
            stopped=true
        fi
    done

    if [ "$stopped" = false ]; then
        echo -e "${GREEN}✅ 没有检测到已运行的 ABS 实例${NC}"
    else
        echo -e "${GREEN}✅ 已停止旧实例${NC}"
    fi
}

ensure_env_ready() {
    if [ ! -f "backend/.env" ]; then
        echo -e "${YELLOW}⚠️  未找到 backend/.env，正在从模板创建...${NC}"
        cp backend/.env.example backend/.env
        echo -e "${YELLOW}⚠️  请编辑 backend/.env 并配置 CODE_GENERATOR 和相应的 API 密钥${NC}"
    fi

    # set -a 确保 .env 中的变量自动 export，供子进程使用
    set -a
    # shellcheck disable=SC1091
    source backend/.env
    set +a

    # Normalize relative paths defined in backend/.env to ensure they resolve
    # correctly even when this script runs outside backend/.
    if [ -n "${WORKSPACE_DIR:-}" ]; then
        RESOLVED_WORKSPACE_DIR=$(resolve_dir_path "$WORKSPACE_DIR")
        if [ -n "$RESOLVED_WORKSPACE_DIR" ]; then
            export WORKSPACE_DIR="$RESOLVED_WORKSPACE_DIR"
            echo -e "${GREEN}✅ WORKSPACE_DIR -> ${WORKSPACE_DIR}${NC}"
        fi
    fi

    if [ -n "${APPS_DATA_FILE:-}" ]; then
        RESOLVED_APPS_DATA_FILE=$(resolve_file_path "$APPS_DATA_FILE")
        if [ -n "$RESOLVED_APPS_DATA_FILE" ]; then
            export APPS_DATA_FILE="$RESOLVED_APPS_DATA_FILE"
            echo -e "${GREEN}✅ APPS_DATA_FILE -> ${APPS_DATA_FILE}${NC}"
        fi
    fi

    # 检查 CODE_GENERATOR 配置
    CODE_GEN="${CODE_GENERATOR:-codex_cli}"

    case "$CODE_GEN" in
        codex_cli)
            # Codex CLI 模式：检查 codex 命令是否可用
            if ! command -v "${CODEX_CLI_PATH:-codex}" &> /dev/null; then
                echo -e "${YELLOW}⚠️  警告: 未找到 codex 命令（${CODEX_CLI_PATH:-codex}）${NC}"
                echo -e "${YELLOW}   请确保已安装 Codex CLI 或修改 CODEX_CLI_PATH 配置${NC}"
            else
                echo -e "${GREEN}✅ 使用 Codex CLI 模式（${CODEX_CLI_PATH:-codex}）${NC}"
            fi
            ;;
        codex)
            # Codex API 模式：检查 API Key
            if [ -z "${CODEX_API_KEY:-}" ] || [ "$CODEX_API_KEY" = "your-codex-api-key-here" ]; then
                echo -e "${RED}❌ backend/.env 中未配置有效的 CODEX_API_KEY${NC}"
                exit 1
            fi
            echo -e "${GREEN}✅ 使用 Codex API 模式${NC}"
            ;;
        claude)
            # Claude 模式：检查 API Key
            if [ -z "${ANTHROPIC_API_KEY:-}" ] || [ "$ANTHROPIC_API_KEY" = "your-api-key-here" ]; then
                echo -e "${RED}❌ backend/.env 中未配置有效的 ANTHROPIC_API_KEY${NC}"
                exit 1
            fi
            echo -e "${GREEN}✅ 使用 Claude 模式${NC}"
            ;;
        *)
            echo -e "${YELLOW}⚠️  未识别的 CODE_GENERATOR: $CODE_GEN，默认使用 codex_cli${NC}"
            ;;
    esac
}

install_frontend_deps() {
    if [ ! -d "frontend/node_modules" ]; then
        log_step "📦 安装前端依赖..."
        (cd frontend && npm install)
    fi
}

build_backend() {
    log_step "🛠️  编译 Go 后端..."
    (cd backend && mkdir -p bin && go build -o bin/server ./cmd/server)
}

start_backend() {
    log_step "🚀 启动后端（http://localhost:8090）..."
    backend/bin/server &
    BACKEND_PID=$!
}

start_frontend() {
    log_step "🎨 启动前端（http://localhost:5180）..."
    (cd frontend && npm run dev) &
    FRONTEND_PID=$!
}

cleanup() {
    echo -e "${YELLOW}\n🧹 收尾：停止当前会话启动的服务...${NC}"
    if [ -n "${BACKEND_PID}" ] && ps -p "${BACKEND_PID}" >/dev/null 2>&1; then
        kill "${BACKEND_PID}" >/dev/null 2>&1 || true
    fi
    if [ -n "${FRONTEND_PID}" ] && ps -p "${FRONTEND_PID}" >/dev/null 2>&1; then
        kill "${FRONTEND_PID}" >/dev/null 2>&1 || true
    fi
    echo -e "${GREEN}✅ 已停止${NC}"
}

trap cleanup EXIT INT TERM

echo -e "${GREEN}🤖 Restarting ABS - AI Bootstrapping System${NC}\n"

stop_active_services
ensure_env_ready
install_frontend_deps
build_backend
start_backend
log_step "⏳ 等待后端就绪..."
if wait_for_port "localhost" "${PORT:-8090}" 30; then
    echo -e "${GREEN}✅ 后端已准备就绪${NC}"
else
    echo -e "${RED}❌ 在超时时间内无法连接到后端端口 ${PORT:-8090}${NC}"
    exit 1
fi

start_frontend

echo -e "\n${GREEN}✅ ABS 已重新启动！${NC}"
echo "  Frontend: http://localhost:5180"
echo "  Backend : http://localhost:8090"
echo "  WebSocket: ws://localhost:8090/ws"
echo -e "${YELLOW}按 Ctrl+C 可停止由本脚本启动的所有服务${NC}"

wait
