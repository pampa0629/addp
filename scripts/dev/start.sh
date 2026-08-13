#!/bin/bash
set -e

# 使用说明
show_usage() {
  echo "用法: $0 [选项]"
  echo ""
  echo "选项:"
  echo "  无参数        启动所有模块"
  echo "  -system       启动 System 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -manager      启动 Manager 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console + Transfer + Model3D/PointCloud Workflow Engine)"
  echo "  -meta         启动 Meta 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -transfer     启动 Transfer 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -orchestrator 启动 Orchestrator 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -develop      启动 Develop 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console + DuckDB + Python/Math/Spark Workflow Engine + Jupyter)"
  echo "  -service      启动 Service 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console + DuckDB)"
  echo "  -monitor      启动 Monitor 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -copilot      启动 Copilot 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console + Develop)"
  echo "  -agent        启动 Agent 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -inference    启动 Inference 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -standard     启动 Standard 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -model        启动 Model 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console + Standard)"
  echo "  -quality      启动 Quality 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console + Standard)"
  echo "  -asset        启动 Asset 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -portal       启动 Portal 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console + Asset)"
  echo "  -graph        启动 Graph 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -python-workflow    启动 GeoPython Workflow Engine (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -math-workflow      启动 Math Workflow Engine (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -model3d-workflow   启动 Model3D Workflow Engine (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -pointcloud-workflow 启动 PointCloud Workflow Engine (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -supermap-workflow  启动 SuperMap Workflow Engine (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console，需先构建 C++ 基础镜像)"
  echo "  -spark-workflow     启动 Spark 工作流引擎 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -jupyter      启动 Jupyter Engine (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -duckdb       启动 DuckDB Federated Query Runtime (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
  echo "  -gateway      启动 Gateway (依赖: 所有后端模块)"
  echo "  -console      启动 Console (依赖: 所有模块)"
  echo ""
  echo "说明:"
  echo "  - 指定模块时,会自动启动其依赖的模块"
  echo "  - 单模块启动统一带上 System Backend、Meta Backend/Worker、Gateway 和 Console"
  echo "  - 基础设施(PostgreSQL/Redis/MinIO/Meilisearch)总是会启动"
  echo ""
  echo "示例:"
  echo "  $0                # 启动所有模块"
  echo "  $0 -system        # 启动 System Backend/Frontend + Meta Backend/Worker + Gateway + Console"
  echo "  $0 -manager       # 启动 Manager + 公共依赖 + Transfer + 三维/点云工作流"
  echo "  $0 -develop       # 启动 Develop + 公共依赖 + 工作流引擎"
  echo "  $0 -python-workflow     # 启动 GeoPython Workflow Engine + 公共依赖"
  echo "  $0 -math-workflow       # 启动 Math Workflow Engine + 公共依赖"
  echo "  $0 -model3d-workflow    # 启动 Model3D Workflow Engine + 公共依赖"
  echo "  $0 -pointcloud-workflow # 启动 PointCloud Workflow Engine + 公共依赖"
  echo "  $0 -supermap-workflow  # 启动 SuperMap Workflow Engine + 公共依赖"
  echo "  $0 -spark-workflow      # 启动 Spark 工作流引擎 + 公共依赖"
  echo "  $0 -jupyter       # 启动 Jupyter Engine + 公共依赖"
  exit 1
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

cd "${ROOT_DIR}"

source "${SCRIPT_DIR}/lifecycle-lock.sh"
addp_acquire_lifecycle_lock start "$@"

export PROJECT_ROOT="${ROOT_DIR}"
mkdir -p logs

# 加载 .env 配置
if [ -f ".env" ]; then
    set -a
    source .env
    set +a
fi

export MODEL3D_WORKFLOW_PORT="${MODEL3D_WORKFLOW_PORT:-8101}"
export POINTCLOUD_WORKFLOW_PORT="${POINTCLOUD_WORKFLOW_PORT:-8102}"
export SUPERMAP_WORKFLOW_PORT="${SUPERMAP_WORKFLOW_PORT:-8103}"
export DUCKDB_RUNTIME_PORT="${DUCKDB_RUNTIME_PORT:-8104}"
if [ -z "${DUCKDB_RUNTIME_URL:-}" ]; then
  export DUCKDB_RUNTIME_URL="http://${SERVICE_HOST:-localhost}:${DUCKDB_RUNTIME_PORT}"
fi

ensure_model3d_node_dependencies() {
  local dir="engines/model3d-workflow"
  if [ ! -f "$dir/package.json" ]; then
    return 0
  fi
  if [ -d "$dir/node_modules/@mkkellogg/gaussian-splats-3d" ]; then
    echo "Model3D Workflow Node 依赖已存在，跳过安装"
    return 0
  fi
  if ! command -v npm >/dev/null 2>&1; then
    echo -e "${RED}✗ Model3D Workflow 需要 npm 安装高斯泼溅 KSplat 转换依赖${NC}"
    exit 1
  fi
  echo "安装 Model3D Workflow Node 依赖..."
  if [ -f "$dir/package-lock.json" ]; then
    (cd "$dir" && npm ci --omit=dev)
  else
    (cd "$dir" && npm install --omit=dev)
  fi
  echo -e "${GREEN}✓ Model3D Workflow Node 依赖安装完成${NC}"
}

# 自动生成服务 URL（基于 SERVICE_HOST + XXX_BACKEND_PORT）
generate_service_urls() {
    local services=(system manager meta transfer orchestrator develop service copilot monitor standard model quality asset portal agent graph inference)
    for svc in "${services[@]}"; do
        local port_var="$(echo ${svc} | tr '[:lower:]' '[:upper:]')_BACKEND_PORT"
        local url_var="$(echo ${svc} | tr '[:lower:]' '[:upper:]')_URL"
        local port_val="${!port_var}"
        if [ -n "$port_val" ]; then
            export ${url_var}="http://${SERVICE_HOST}:${port_val}"
        fi
    done

    # 特殊服务
    [ -n "$MEILISEARCH_PORT" ] && export MEILISEARCH_URL="http://${SERVICE_HOST}:${MEILISEARCH_PORT}"
    [ -n "$PYTHON_WORKFLOW_PORT" ] && export PYTHON_WORKFLOW_URL="http://${SERVICE_HOST}:${PYTHON_WORKFLOW_PORT}"
    [ -n "$MODEL3D_WORKFLOW_PORT" ] && export MODEL3D_WORKFLOW_URL="http://${SERVICE_HOST}:${MODEL3D_WORKFLOW_PORT}"
    [ -n "$POINTCLOUD_WORKFLOW_PORT" ] && export POINTCLOUD_WORKFLOW_URL="http://${SERVICE_HOST}:${POINTCLOUD_WORKFLOW_PORT}"
    [ -n "$SUPERMAP_WORKFLOW_PORT" ] && export SUPERMAP_WORKFLOW_URL="http://${SERVICE_HOST}:${SUPERMAP_WORKFLOW_PORT}"
    [ -n "$SPARK_WORKFLOW_PORT" ] && export SPARK_WORKFLOW_URL="http://${SERVICE_HOST}:${SPARK_WORKFLOW_PORT}"
}

generate_service_urls

export RASTER_MOSAIC_RUNTIME_PORT="${RASTER_MOSAIC_RUNTIME_PORT:-8291}"
if [ -z "$RASTER_MOSAIC_RUNTIME_URL" ]; then
    export RASTER_MOSAIC_RUNTIME_URL="http://${SERVICE_HOST:-localhost}:${RASTER_MOSAIC_RUNTIME_PORT}"
fi

# 导出 Python pip 配置（pip 会自动识别这些环境变量）
if [ -n "$PIP_INDEX_URL" ]; then
    export PIP_INDEX_URL
    export PIP_TRUSTED_HOST
fi
mkdir -p .dev-pids
mkdir -p .dev-bins

MAX_WAIT=${MAX_WAIT:-60}

# 本地 Go 构建缓存，避免写入系统 GOPATH，并优先使用本机 Go 工具链
export GOMODCACHE="${PROJECT_ROOT}/.gomodcache"
export GOPATH="${PROJECT_ROOT}/.gopath"
export GOTOOLCHAIN="local"

# 加载颜色定义（提前加载，供检查函数使用）
source "${SCRIPT_DIR}/../utils/colors.sh"
source "${SCRIPT_DIR}/jupyter-env.sh"
source "${SCRIPT_DIR}/build-identity.sh"

# ============================================================
# Python 版本选择函数
# ============================================================
# 智能选择 Python 版本：优先 Python 3.11（兼容所有依赖）
# 避免 Python 3.13（NumPy/pydantic 等包兼容性问题）
select_python() {
  # 1. 优先：python3.12（最广泛兼容 pydantic-core 等依赖）
  if command -v python3.12 &> /dev/null; then
    echo "python3.12"
    return
  fi

  # 2. 其次：python3.11
  if command -v python3.11 &> /dev/null; then
    echo "python3.11"
    return
  fi

  # 3. 检查系统 python3 版本（仅接受 3.11/3.12/3.13）
  if command -v python3 &> /dev/null; then
    PYTHON_VERSION=$(python3 -c 'import sys; print(".".join(map(str, sys.version_info[:2])))')
    case $PYTHON_VERSION in
      3.11*|3.12*|3.13*)
        echo "python3"
        return
        ;;
    esac
    # 3.14+ 不兼容 pydantic-core
    echo -e "${YELLOW}⚠️  警告：Python $PYTHON_VERSION 不兼容部分依赖（pydantic-core 等）${NC}" >&2
    echo -e "${YELLOW}   推荐安装 Python 3.12: brew install python@3.12${NC}" >&2
    exit 1
  fi

  # 4. 失败：未找到 Python
  echo -e "${RED}❌ 未找到 Python 3.11+，请先安装 Python${NC}" >&2
  exit 1
}

configure_spark_workflow_java() {
  local candidate
  local java_major
  local candidates=(
    "${JAVA_HOME:-}"
    "/opt/homebrew/opt/openjdk@11/libexec/openjdk.jdk/Contents/Home"
    "/usr/local/opt/openjdk@11/libexec/openjdk.jdk/Contents/Home"
    /usr/lib/jvm/java-11-openjdk-*
    "/usr/lib/jvm/java-11-openjdk"
  )

  for candidate in "${candidates[@]}"; do
    [ -x "${candidate}/bin/java" ] || continue
    java_major=$("${candidate}/bin/java" -version 2>&1 | awk -F'[\".]' '/version/ { print $2; exit }')
    if [ "$java_major" = "11" ]; then
      export JAVA_HOME="$candidate"
      export PATH="${JAVA_HOME}/bin:${PATH}"
      echo "Spark Workflow 使用 JDK 11: ${JAVA_HOME}"
      return 0
    fi
  done

  echo -e "${RED}✗ Spark Workflow 需要 JDK 11，当前未找到可用安装${NC}"
  echo -e "${YELLOW}macOS 请运行: brew install openjdk@11${NC}"
  return 1
}

detect_spark_workflow_shared_host() {
  local interface
  local host

  if command -v route >/dev/null 2>&1 && command -v ipconfig >/dev/null 2>&1; then
    interface=$(route -n get default 2>/dev/null | awk '/interface:/ { print $2; exit }')
    [ -n "$interface" ] && host=$(ipconfig getifaddr "$interface" 2>/dev/null || true)
  elif command -v ip >/dev/null 2>&1; then
    host=$(ip route get 1.1.1.1 2>/dev/null | awk '{ for (i = 1; i <= NF; i++) if ($i == "src") { print $(i + 1); exit } }')
  fi

  [ -n "$host" ] || return 1
  printf '%s\n' "$host"
}

# ============================================================
# 模块选择逻辑
# ============================================================

# 解析命令行参数
SELECTED_MODULE=""
START_ALL=true

for arg in "$@"; do
  case $arg in
    -h|--help)
      show_usage
      ;;
    -system|-manager|-meta|-transfer|-orchestrator|-develop|-service|-monitor|-copilot|-agent|-inference|-standard|-model|-quality|-asset|-portal|-graph|-python-workflow|-math-workflow|-model3d-workflow|-pointcloud-workflow|-supermap-workflow|-spark-workflow|-jupyter|-duckdb|-gateway|-console)
      SELECTED_MODULE="${arg#-}"
      START_ALL=false
      ;;
    *)
      echo -e "${RED}❌ 未知参数: $arg${NC}"
      show_usage
      ;;
  esac
done

# 定义模块启动标志(默认不启动)
START_SYSTEM_BACKEND=false
START_SYSTEM_FRONTEND=false
START_MANAGER_BACKEND=false
START_MANAGER_FRONTEND=false
START_META_BACKEND=false
START_META_FRONTEND=false
START_META_WORKER=false
START_TRANSFER_BACKEND=false
START_TRANSFER_FRONTEND=false
START_TRANSFER_BOUNDED_WORKER=false
START_TRANSFER_CONTINUOUS_WORKER=false
START_ORCHESTRATOR_BACKEND=false
START_ORCHESTRATOR_FRONTEND=false
START_DEVELOP_BACKEND=false
START_DEVELOP_FRONTEND=false
START_SERVICE_BACKEND=false
START_SERVICE_FRONTEND=false
START_MONITOR_BACKEND=false
START_MONITOR_FRONTEND=false
START_COPILOT_BACKEND=false
START_AGENT_BACKEND=false
START_AGENT_FRONTEND=false
START_INFERENCE_BACKEND=false
START_INFERENCE_FRONTEND=false
START_STANDARD_BACKEND=false
START_STANDARD_FRONTEND=false
START_MODEL_BACKEND=false
START_MODEL_FRONTEND=false
START_QUALITY_BACKEND=false
START_QUALITY_FRONTEND=false
START_ASSET_BACKEND=false
START_ASSET_FRONTEND=false
START_PORTAL_BACKEND=false
START_PORTAL_FRONTEND=false
START_GRAPH_BACKEND=false
START_GRAPH_FRONTEND=false
START_GATEWAY=false
START_CONSOLE=false
START_PYTHON_WORKFLOW=false
START_MATH_WORKFLOW=false
START_MODEL3D_WORKFLOW=false
START_POINTCLOUD_WORKFLOW=false
START_SUPERMAP_WORKFLOW=false
START_SPARK_WORKFLOW=false
START_JUPYTER=false
START_DUCKDB=false

enable_single_module_common_dependencies() {
  START_SYSTEM_BACKEND=true
  START_META_BACKEND=true
  START_META_WORKER=true
  START_GATEWAY=true
  START_CONSOLE=true
}

# 根据选择的模块设置启动标志
if [ "$START_ALL" = true ]; then
  # 启动所有模块
  START_SYSTEM_BACKEND=true
  START_SYSTEM_FRONTEND=true
  START_MANAGER_BACKEND=true
  START_MANAGER_FRONTEND=true
  START_META_BACKEND=true
  START_META_FRONTEND=true
  START_META_WORKER=true
  START_TRANSFER_BACKEND=true
  START_TRANSFER_FRONTEND=true
  START_TRANSFER_BOUNDED_WORKER=true
  START_TRANSFER_CONTINUOUS_WORKER=true
  START_ORCHESTRATOR_BACKEND=true
  START_ORCHESTRATOR_FRONTEND=true
  START_DEVELOP_BACKEND=true
  START_DEVELOP_FRONTEND=true
  START_SERVICE_BACKEND=true
  START_SERVICE_FRONTEND=true
  START_MONITOR_BACKEND=true
  START_MONITOR_FRONTEND=true
  START_COPILOT_BACKEND=true
  START_AGENT_BACKEND=true
  START_AGENT_FRONTEND=true
  START_INFERENCE_BACKEND=true
  START_INFERENCE_FRONTEND=true
  START_STANDARD_BACKEND=true
  START_STANDARD_FRONTEND=true
  START_MODEL_BACKEND=true
  START_MODEL_FRONTEND=true
  START_QUALITY_BACKEND=true
  START_QUALITY_FRONTEND=true
  START_ASSET_BACKEND=true
  START_ASSET_FRONTEND=true
  START_PORTAL_BACKEND=true
  START_PORTAL_FRONTEND=true
  START_GRAPH_BACKEND=true
  START_GRAPH_FRONTEND=true
  START_GATEWAY=true
  START_CONSOLE=true
  START_PYTHON_WORKFLOW=true
  START_MATH_WORKFLOW=true
  START_MODEL3D_WORKFLOW=true
  START_POINTCLOUD_WORKFLOW=true
  START_SUPERMAP_WORKFLOW=true
  START_SPARK_WORKFLOW=true
  START_JUPYTER=true
  START_DUCKDB=true
else
  # 根据选择的模块设置依赖
  case $SELECTED_MODULE in
    system)
      START_SYSTEM_FRONTEND=true
      ;;
    manager)
      START_MANAGER_BACKEND=true
      START_MANAGER_FRONTEND=true
      START_INFERENCE_BACKEND=true
      START_TRANSFER_BACKEND=true
      START_TRANSFER_BOUNDED_WORKER=true
      START_TRANSFER_CONTINUOUS_WORKER=true
      START_MODEL3D_WORKFLOW=true
      START_POINTCLOUD_WORKFLOW=true
      # Manager 预览/Transfer 读取 SuperMap SDX+ for PostgreSQL
      # 时必须通过绑定的 SuperMap Workflow Runtime 访问私有 geometry。
      START_SUPERMAP_WORKFLOW=true
      ;;
    meta)
      START_META_FRONTEND=true
      ;;
    transfer)
      START_TRANSFER_BACKEND=true
      START_TRANSFER_FRONTEND=true
      START_TRANSFER_BOUNDED_WORKER=true
      START_TRANSFER_CONTINUOUS_WORKER=true
      ;;
    orchestrator)
      START_ORCHESTRATOR_BACKEND=true
      START_ORCHESTRATOR_FRONTEND=true
      ;;
    develop)
      START_DEVELOP_BACKEND=true
      START_DEVELOP_FRONTEND=true
      START_PYTHON_WORKFLOW=true
      START_MATH_WORKFLOW=true
      START_SPARK_WORKFLOW=true
      START_JUPYTER=true
      START_DUCKDB=true
      ;;
    service)
      START_SERVICE_BACKEND=true
      START_SERVICE_FRONTEND=true
      START_DUCKDB=true
      ;;
    monitor)
      START_MONITOR_BACKEND=true
      START_MONITOR_FRONTEND=true
      ;;
    copilot)
      START_DEVELOP_BACKEND=true
      START_PYTHON_WORKFLOW=true
      START_JUPYTER=true
      START_DUCKDB=true
      START_COPILOT_BACKEND=true
      START_INFERENCE_BACKEND=true
      ;;
    agent)
      START_AGENT_BACKEND=true
      START_AGENT_FRONTEND=true
      START_INFERENCE_BACKEND=true
      ;;
    inference)
      START_INFERENCE_BACKEND=true
      START_INFERENCE_FRONTEND=true
      ;;
    standard)
      START_STANDARD_BACKEND=true
      START_STANDARD_FRONTEND=true
      ;;
    model)
      START_STANDARD_BACKEND=true
      START_STANDARD_FRONTEND=true
      START_MODEL_BACKEND=true
      START_MODEL_FRONTEND=true
      ;;
    quality)
      START_STANDARD_BACKEND=true
      START_QUALITY_BACKEND=true
      START_QUALITY_FRONTEND=true
      ;;
    asset)
      START_ASSET_BACKEND=true
      START_ASSET_FRONTEND=true
      ;;
    portal)
      START_ASSET_BACKEND=true
      START_PORTAL_BACKEND=true
      START_PORTAL_FRONTEND=true
      ;;
    graph)
      START_GRAPH_BACKEND=true
      START_GRAPH_FRONTEND=true
      ;;
    python-workflow)
      START_PYTHON_WORKFLOW=true
      ;;
    math-workflow)
      START_MATH_WORKFLOW=true
      ;;
    model3d-workflow)
      START_MODEL3D_WORKFLOW=true
      ;;
    pointcloud-workflow)
      START_POINTCLOUD_WORKFLOW=true
      ;;
    supermap-workflow)
      START_SUPERMAP_WORKFLOW=true
      ;;
    spark-workflow)
      START_SPARK_WORKFLOW=true
      ;;
    jupyter)
      START_JUPYTER=true
      ;;
    duckdb)
      START_DUCKDB=true
      ;;
    gateway)
      START_MANAGER_BACKEND=true
      START_TRANSFER_BACKEND=true
      START_TRANSFER_BOUNDED_WORKER=true
      START_TRANSFER_CONTINUOUS_WORKER=true
      START_ORCHESTRATOR_BACKEND=true
      START_DEVELOP_BACKEND=true
      START_SERVICE_BACKEND=true
      START_MONITOR_BACKEND=true
      START_COPILOT_BACKEND=true
      START_INFERENCE_BACKEND=true
      START_STANDARD_BACKEND=true
      START_MODEL_BACKEND=true
      START_PYTHON_WORKFLOW=true
      START_MODEL3D_WORKFLOW=true
      START_POINTCLOUD_WORKFLOW=true
      START_SPARK_WORKFLOW=true
      START_JUPYTER=true
      START_DUCKDB=true
      START_GATEWAY=true
      ;;
    console)
      START_SYSTEM_FRONTEND=true
      START_MANAGER_BACKEND=true
      START_MANAGER_FRONTEND=true
      START_META_FRONTEND=true
      START_TRANSFER_BACKEND=true
      START_TRANSFER_FRONTEND=true
      START_TRANSFER_BOUNDED_WORKER=true
      START_TRANSFER_CONTINUOUS_WORKER=true
      START_ORCHESTRATOR_BACKEND=true
      START_ORCHESTRATOR_FRONTEND=true
      START_DEVELOP_BACKEND=true
      START_DEVELOP_FRONTEND=true
      START_SERVICE_BACKEND=true
      START_SERVICE_FRONTEND=true
      START_MONITOR_BACKEND=true
      START_MONITOR_FRONTEND=true
      START_COPILOT_BACKEND=true
      START_INFERENCE_BACKEND=true
      START_INFERENCE_FRONTEND=true
      START_STANDARD_BACKEND=true
      START_STANDARD_FRONTEND=true
      START_MODEL_BACKEND=true
      START_MODEL_FRONTEND=true
      START_QUALITY_BACKEND=true
      START_QUALITY_FRONTEND=true
      START_GATEWAY=true
      START_CONSOLE=true
      START_PYTHON_WORKFLOW=true
      START_MODEL3D_WORKFLOW=true
      START_POINTCLOUD_WORKFLOW=true
      START_SPARK_WORKFLOW=true
      START_JUPYTER=true
      START_DUCKDB=true
      ;;
  esac

  # 单模块开发也统一保留 ADDP 基础服务和 Console 入口。
  # 各模块前端和 Console 的 /api 代理都经由 Gateway；资源、任务和审计等通用能力依赖 System/Meta。
  enable_single_module_common_dependencies
fi

# 显示启动计划
if [ "$START_ALL" = true ]; then
  echo "🚀 启动 ADDP 开发环境 (所有模块)"
else
  echo "🚀 启动 ADDP 开发环境 (模块: ${SELECTED_MODULE} + 依赖)"
fi
echo ""

# ============================================================
# 服务运行状态检查函数
# ============================================================

# 检查服务是否已在运行
# 参数: $1=服务名称 $2=端口号（可选，不传则只检查PID）
# 返回: 0=未运行(可以启动), 1=已运行(跳过启动)
check_service_running() {
    local service_name=$1
    local port=$2
    local pidfile=".dev-pids/${service_name}.pid"

    # 检查 PID 文件
    if [ -f "$pidfile" ]; then
        local pid=$(cat "$pidfile" 2>/dev/null)
        if [ -n "$pid" ] && ps -p "$pid" > /dev/null 2>&1; then
            echo -e "${YELLOW}⚠️  ${service_name} 已在运行 (PID: $pid)，跳过启动${NC}"
            return 1
        fi
    fi

    # 检查端口占用（如果提供了端口参数）
    # 注意：必须加 -sTCP:LISTEN，只检查真正监听该端口的进程
    # 不加此标志会匹配所有 TCP 连接（包括浏览器等作为客户端的临时出站连接），导致误报
    if [ -n "$port" ]; then
        if lsof -ti :$port -sTCP:LISTEN > /dev/null 2>&1; then
            local occupying_pid=$(lsof -ti :$port -sTCP:LISTEN)
            local proc_cmd=$(ps -p $occupying_pid -o command= 2>/dev/null || echo "")

            # 检查是否是 ADDP 相关进程
            if echo "$proc_cmd" | grep -qE "(addp-|go run|vite|api_server\.py|uvicorn|jupyter.*lab|agent/backend/main\.py|copilot/backend/main\.py)"; then
                echo -e "${RED}✗ 端口 $port 已被 ADDP 进程占用 (PID: $occupying_pid)${NC}"
                echo -e "${YELLOW}  进程: $(echo "$proc_cmd" | cut -c1-80)${NC}"
                echo -e "${RED}✗ 无法启动 ${service_name}，可能是旧进程未清理${NC}"
                echo -e "${YELLOW}提示: 运行 'bash scripts/dev/stop.sh' 停止所有服务${NC}"
            else
                echo -e "${RED}✗ 端口 $port 已被非 ADDP 进程占用 (PID: $occupying_pid)${NC}"
                echo -e "${YELLOW}  进程: $(echo "$proc_cmd" | cut -c1-80)${NC}"
                echo -e "${RED}✗ 无法启动 ${service_name}${NC}"
                echo -e "${YELLOW}解决方案:${NC}"
                echo -e "${YELLOW}  选项 A: 关闭占用端口的进程 (kill $occupying_pid)${NC}"
                echo -e "${YELLOW}  选项 B: 修改 .env 中的端口配置并重新启动${NC}"
            fi
            return 1
        fi
    fi

    return 0
}

# ============================================================
# 编译函数: 快速编译服务为指定名称的二进制
# ============================================================

# 编译 Server 二进制
# 参数: $1=服务名称 $2=源码路径
build_service() {
    local name=$1
    local src_dir=$2
    local output_dir=".dev-bins"

    local binary_name="addp-${name}"
    local binary_path="${output_dir}/${binary_name}"

    # 检查是否需要重新编译（增量编译）
    local build_args=()
    if [[ "$name" == "transfer" ]]; then
        build_args+=(-tags sqlite_load_extension)
    fi
    build_args+=(./cmd/server)

    if addp_go_build_is_current "$src_dir" "$binary_path" "${build_args[@]}" &&
        { [[ "$name" != "duckdb" ]] || addp_go_build_is_current "$src_dir" "${output_dir}/addp-duckdb-prepare" ./cmd/prepare-extensions; }; then
        echo "  ✓ ${binary_name} 已是最新"
        return 0
    fi

    echo "  🔨 编译 ${binary_name}..."

    addp_atomic_go_build "$name" "$src_dir" "$binary_path" "${build_args[@]}" || {
        echo "  ✗ 编译失败: ${name}"
        return 1
    }

    if [[ "$name" == "duckdb" ]]; then
        addp_atomic_go_build "duckdb-prepare" "$src_dir" "${output_dir}/addp-duckdb-prepare" ./cmd/prepare-extensions || {
            echo "  ✗ 编译失败: DuckDB extension preparer"
            return 1
        }
    fi

    echo "  ✓ ${binary_name} 编译完成"
}

# 编译 Worker 二进制
# 参数: $1=服务名称 $2=源码路径
build_worker() {
    local name=$1
    local src_dir=$2
    local output_dir=".dev-bins"

    local binary_name="addp-${name}-worker"
    local binary_path="${output_dir}/${binary_name}"

    # 检查是否需要重新编译（增量编译）
    if addp_go_build_is_current "$src_dir" "$binary_path" ./cmd/worker; then
        echo "  ✓ ${binary_name} 已是最新"
        return 0
    fi

    echo "  🔨 编译 ${binary_name}..."

    addp_atomic_go_build "${name}-worker" "$src_dir" "$binary_path" ./cmd/worker || {
        echo "  ✗ 编译失败: ${name} worker"
        return 1
    }

    echo "  ✓ ${binary_name} 编译完成"
}

build_transfer_bounded_worker() {
    local binary_path=".dev-bins/addp-transfer-bounded-worker"
    if addp_go_build_is_current "transfer/backend" "$binary_path" -tags sqlite_load_extension ./cmd/worker; then
        echo "  ✓ addp-transfer-bounded-worker 已是最新"
        return 0
    fi
    echo "  🔨 编译 addp-transfer-bounded-worker..."
    addp_atomic_go_build "transfer-bounded-worker" "transfer/backend" "$binary_path" -tags sqlite_load_extension ./cmd/worker || {
        echo "  ✗ 编译失败: transfer bounded worker"
        return 1
    }
    echo "  ✓ addp-transfer-bounded-worker 编译完成"
}

build_transfer_continuous_worker() {
    local binary_path=".dev-bins/addp-transfer-continuous-worker"
    if addp_go_build_is_current "transfer/backend" "$binary_path" ./cmd/continuous-worker; then
        echo "  ✓ addp-transfer-continuous-worker 已是最新"
        return 0
    fi
    echo "  🔨 编译 addp-transfer-continuous-worker..."
    addp_atomic_go_build "transfer-continuous-worker" "transfer/backend" "$binary_path" ./cmd/continuous-worker || {
        echo "  ✗ 编译失败: transfer continuous worker"
        return 1
    }
    echo "  ✓ addp-transfer-continuous-worker 编译完成"
}

# 编译 Gateway 二进制
build_gateway() {
    local output_dir=".dev-bins"
    local binary_name="addp-gateway"
    local binary_path="${output_dir}/${binary_name}"

    # 检查是否需要重新编译（增量编译）
    if addp_go_build_is_current "gateway" "$binary_path" ./cmd/gateway; then
        echo "  ✓ ${binary_name} 已是最新"
        return 0
    fi

    echo "  🔨 编译 ${binary_name}..."

    addp_atomic_go_build "gateway" "gateway" "$binary_path" ./cmd/gateway || {
        echo "  ✗ 编译失败: gateway"
        return 1
    }

    echo "  ✓ ${binary_name} 编译完成"
}

# Honor GOPROXY from environment/.env to avoid module i/o timeouts
if [ -n "${GOPROXY}" ]; then
  export GOPROXY
  echo "Go module proxy set: ${GOPROXY}"
fi

if [ -f "${ROOT_DIR}/.env" ]; then
  set -a
  # shellcheck source=/dev/null
  source "${ROOT_DIR}/.env"
  set +a
fi

# 验证和提示 SpatiaLite 扩展配置（用于 Transfer 模块）
if [ -n "${SPATIALITE_EXTENSION_PATH}" ]; then
  # 用户在 .env 中配置了路径，验证是否有效
  if [ -f "${SPATIALITE_EXTENSION_PATH}" ]; then
    echo "✓ 使用 SpatiaLite 扩展: ${SPATIALITE_EXTENSION_PATH}"
  else
    echo "⚠️  警告: .env 中配置的 SPATIALITE_EXTENSION_PATH 文件不存在: ${SPATIALITE_EXTENSION_PATH}"
    echo "   Transfer 模块将尝试使用代码中的兜底路径"
  fi
else
  # 用户未配置，检测系统中是否存在，并给出提示
  detected_path=""
  for candidate_path in \
    "/opt/homebrew/lib/mod_spatialite.dylib" \
    "/usr/local/lib/mod_spatialite.dylib" \
    "/usr/lib/x86_64-linux-gnu/mod_spatialite.so" \
    "/usr/lib64/mod_spatialite.so"; do
    if [ -f "$candidate_path" ]; then
      detected_path="$candidate_path"
      break
    fi
  done

  if [ -n "$detected_path" ]; then
    echo "💡 提示: 检测到 SpatiaLite 扩展在 ${detected_path}"
    echo "   Transfer 模块将使用代码兜底路径自动加载"
    echo "   如需明确指定，可在 .env 中添加: SPATIALITE_EXTENSION_PATH=${detected_path}"
  else
    echo "ℹ️  未检测到 SpatiaLite 扩展，Transfer 模块的 SpatiaLite 读取功能可能不可用"
    echo "   如需支持 SpatiaLite，请安装: brew install libspatialite (macOS)"
  fi
fi

echo "🚀 启动 ADDP 开发环境"
echo ""

# 端口配置
CONSOLE_FE_PORT=${CONSOLE_FE_PORT:-5170}
SYSTEM_FE_PORT=${SYSTEM_FE_PORT:-5173}
MANAGER_FE_PORT=${MANAGER_FE_PORT:-5174}
META_FE_PORT=${META_FE_PORT:-5175}
TRANSFER_FE_PORT=${TRANSFER_FE_PORT:-5176}
ORCHESTRATOR_FE_PORT=${ORCHESTRATOR_FE_PORT:-5177}
DEVELOP_FE_PORT=${DEVELOP_FE_PORT:-5178}
SERVICE_FE_PORT=${SERVICE_FE_PORT:-5180}
MONITOR_FE_PORT=${MONITOR_FE_PORT:-5179}
STANDARD_FE_PORT=${STANDARD_FE_PORT:-5181}
MODEL_FE_PORT=${MODEL_FE_PORT:-5182}
QUALITY_FE_PORT=${QUALITY_FE_PORT:-5183}
ASSET_FE_PORT=${ASSET_FE_PORT:-5184}
PORTAL_FE_PORT=${PORTAL_FE_PORT:-5185}
AGENT_FE_PORT=${AGENT_FE_PORT:-5186}
INFERENCE_FE_PORT=${INFERENCE_FE_PORT:-5188}

# 0. 检查 Go 模块依赖
echo -e "${YELLOW}Step 0: 检查 Go 模块依赖${NC}"
echo ""

if [[ "${SKIP_MODTIDY:-0}" != "1" ]]; then
  if bash "${SCRIPT_DIR}/modtidy.sh"; then
    echo -e "${GREEN}✓ Go 依赖检查完成${NC}"
  else
    echo -e "${YELLOW}⚠️  Go 依赖检查失败,继续启动(可能需手动修复)${NC}"
  fi
else
  echo -e "${YELLOW}ℹ️  跳过 Go 依赖检查(设置了 SKIP_MODTIDY=1)${NC}"
fi
echo ""

# 1. 启动基础设施
echo -e "${YELLOW}Step 1/7: 启动基础设施（PostgreSQL, Redis, MinIO, Meilisearch）${NC}"
echo ""

# 检查基础设施是否已运行 - 通过端口检查，不依赖 Docker CLI
# （docker inspect / docker compose ps 在某些 Docker Desktop 环境下会挂起）
INFRA_RUNNING=false
RUNNING_COUNT=0
for svc_port in \
  "PostgreSQL:${POSTGRES_PORT:-15432}" \
  "Redis:${REDIS_PORT:-16379}" \
  "MinIO:${MINIO_API_PORT:-19000}" \
  "Meilisearch:${MEILISEARCH_PORT:-17700}"; do
  svc="${svc_port%%:*}"
  port="${svc_port##*:}"
  if nc -z -w1 localhost "$port" 2>/dev/null; then
    echo -e "  ${GREEN}✓ $svc${NC}"
    RUNNING_COUNT=$((RUNNING_COUNT + 1))
  else
    echo -e "  ${YELLOW}○ $svc 未就绪${NC}"
  fi
done
if [ "$RUNNING_COUNT" -eq 4 ]; then
  INFRA_RUNNING=true
  echo -e "${GREEN}✓ 基础设施已在运行,跳过启动${NC}"
  echo -e "${YELLOW}  (如需重启基础设施,请运行: bash scripts/infra/down.sh && bash scripts/infra/up.sh)${NC}"
fi

# 如果基础设施未完全运行,则启动
if [ "$INFRA_RUNNING" = false ]; then
  echo -e "${YELLOW}启动基础设施服务...${NC}"
  # 调用基础设施启动脚本(单一职责原则)
  if ! bash "${ROOT_DIR}/scripts/infra/up.sh"; then
    echo -e "${RED}✗ 基础设施启动失败,请检查 Docker 是否运行${NC}"
    echo -e "${YELLOW}提示: 运行 'docker info' 检查 Docker 状态${NC}"
    exit 1
  fi
  echo -e "${GREEN}✓ 基础设施启动完成${NC}"
fi

echo ""

# 2. 启动 System Backend
if [ "$START_SYSTEM_BACKEND" = true ]; then
  echo -e "${YELLOW}Step 2/7: 启动 System Backend${NC}"

  # 获取端口配置（从环境变量读取，默认 8180）
  SYSTEM_PORT=${SYSTEM_BACKEND_PORT:-8180}

  # 检查服务是否已在运行
  if check_service_running "system" "$SYSTEM_PORT"; then
    build_service "system" "system/backend"
    .dev-bins/addp-system > logs/system-backend.log 2> logs/system-backend-stderr.log &
    SYSTEM_PID=$!
    echo $SYSTEM_PID > .dev-pids/system.pid

    # 等待 System Backend 就绪
    echo "等待 System Backend 就绪..."
    MAX_WAIT=60
    WAIT_COUNT=0
    until curl -f http://localhost:${SYSTEM_PORT}/health > /dev/null 2>&1; do
      echo -n "."
      sleep 1
      WAIT_COUNT=$((WAIT_COUNT + 1))
      if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
        echo -e "${RED}✗ System Backend 启动超时${NC}"
        echo "查看日志: tail -f logs/system-backend.log"
        exit 1
      fi
    done
    echo -e "${GREEN}✓ System Backend 就绪 (PID: $SYSTEM_PID, 端口: $SYSTEM_PORT)${NC}"
  else
    # 服务已运行，从 PID 文件读取 PID
    SYSTEM_PID=$(cat .dev-pids/system.pid 2>/dev/null)
    echo -e "${GREEN}✓ System Backend 已在运行 (PID: $SYSTEM_PID)${NC}"
  fi
  echo ""
else
  echo -e "${YELLOW}Step 2/7: 跳过 System Backend${NC}"
  echo ""
fi

# 3. 并行启动所有后端服务 + Workers (System 已就绪)
# 跳过检查：如果没有任何后端模块需要启动
if [ "$START_MANAGER_BACKEND" = true ] || [ "$START_META_BACKEND" = true ] || [ "$START_TRANSFER_BACKEND" = true ] || [ "$START_ORCHESTRATOR_BACKEND" = true ] || [ "$START_DEVELOP_BACKEND" = true ] || [ "$START_SERVICE_BACKEND" = true ] || [ "$START_QUALITY_BACKEND" = true ] || [ "$START_STANDARD_BACKEND" = true ] || [ "$START_MONITOR_BACKEND" = true ] || [ "$START_MODEL_BACKEND" = true ] || [ "$START_ASSET_BACKEND" = true ] || [ "$START_PORTAL_BACKEND" = true ] || [ "$START_GRAPH_BACKEND" = true ] || [ "$START_INFERENCE_BACKEND" = true ] || [ "$START_DUCKDB" = true ]; then
  echo -e "${YELLOW}Step 3/5: 并行启动后端服务和选定 Worker${NC}"

  # ============================================================
  # Phase 1: 并行编译所有 Go 服务
  # ============================================================
  echo "  [1/3] 并行编译后端服务和选定 Worker..."

  # 并行编译后端服务(仅编译需要启动的)
  BUILD_PIDS=()

  if [ "$START_DUCKDB" = true ]; then
    build_service "duckdb" "engines/duckdb" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_MANAGER_BACKEND" = true ]; then
    build_service "manager" "manager/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_META_BACKEND" = true ]; then
    build_service "meta" "meta/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_TRANSFER_BACKEND" = true ]; then
    build_service "transfer" "transfer/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_ORCHESTRATOR_BACKEND" = true ]; then
    build_service "orchestrator" "orchestrator/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_DEVELOP_BACKEND" = true ]; then
    build_service "develop" "develop/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_SERVICE_BACKEND" = true ]; then
    build_service "service" "service/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_MONITOR_BACKEND" = true ]; then
    build_service "monitor" "monitor/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_STANDARD_BACKEND" = true ]; then
    build_service "standard" "standard/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_MODEL_BACKEND" = true ]; then
    build_service "model" "model/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_QUALITY_BACKEND" = true ]; then
    build_service "quality" "quality/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_ASSET_BACKEND" = true ]; then
    build_service "asset" "asset/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_PORTAL_BACKEND" = true ]; then
    build_service "portal" "portal/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_GRAPH_BACKEND" = true ]; then
    build_service "graph" "graph/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_INFERENCE_BACKEND" = true ]; then
    build_service "inference" "inference/backend" &
    BUILD_PIDS+=($!)
  fi

  # 并行编译 Workers(仅编译需要启动的)
  if [ "$START_META_WORKER" = true ]; then
    build_worker "meta" "meta/backend" &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_TRANSFER_BOUNDED_WORKER" = true ]; then
    build_transfer_bounded_worker &
    BUILD_PIDS+=($!)
  fi

  if [ "$START_TRANSFER_CONTINUOUS_WORKER" = true ]; then
    build_transfer_continuous_worker &
    BUILD_PIDS+=($!)
  fi

  # 等待所有编译完成
  for pid in "${BUILD_PIDS[@]}"; do
    wait "$pid" || true
  done

  echo "  ${GREEN}✓ 所有服务编译完成${NC}"
else
  echo -e "${YELLOW}Step 3/5: 跳过后端服务启动${NC}"
fi

# ============================================================
# Phase 2: 并行启动所有 Backend 服务
# ============================================================
if [ "$START_MANAGER_BACKEND" = true ] || [ "$START_META_BACKEND" = true ] || [ "$START_TRANSFER_BACKEND" = true ] || [ "$START_ORCHESTRATOR_BACKEND" = true ] || [ "$START_DEVELOP_BACKEND" = true ] || [ "$START_SERVICE_BACKEND" = true ] || [ "$START_QUALITY_BACKEND" = true ] || [ "$START_STANDARD_BACKEND" = true ] || [ "$START_MONITOR_BACKEND" = true ] || [ "$START_MODEL_BACKEND" = true ] || [ "$START_ASSET_BACKEND" = true ] || [ "$START_PORTAL_BACKEND" = true ] || [ "$START_GRAPH_BACKEND" = true ] || [ "$START_INFERENCE_BACKEND" = true ]; then
  echo "  [2/3] 并行启动 Backends..."

  # 启动 Manager Backend（带检查）
  if [ "$START_MANAGER_BACKEND" = true ]; then
    if check_service_running "manager" "$MANAGER_BACKEND_PORT"; then
      .dev-bins/addp-manager > logs/manager-backend.log 2> logs/manager-backend-stderr.log &
      MANAGER_PID=$!
      echo $MANAGER_PID > .dev-pids/manager.pid
    else
      MANAGER_PID=$(cat .dev-pids/manager.pid 2>/dev/null)
    fi
  fi

  # 启动 Meta Backend（带检查）
  if [ "$START_META_BACKEND" = true ]; then
    if check_service_running "meta" "$META_BACKEND_PORT"; then
      .dev-bins/addp-meta > logs/meta-backend.log 2> logs/meta-backend-stderr.log &
      META_PID=$!
      echo $META_PID > .dev-pids/meta.pid
    else
      META_PID=$(cat .dev-pids/meta.pid 2>/dev/null)
    fi
  fi

  # 启动 Transfer Backend（带检查）
  if [ "$START_TRANSFER_BACKEND" = true ]; then
    if check_service_running "transfer" "$TRANSFER_BACKEND_PORT"; then
      .dev-bins/addp-transfer > logs/transfer-backend.log 2> logs/transfer-backend-stderr.log &
      TRANSFER_PID=$!
      echo $TRANSFER_PID > .dev-pids/transfer.pid
    else
      TRANSFER_PID=$(cat .dev-pids/transfer.pid 2>/dev/null)
    fi
  fi

  # 启动 Orchestrator Backend（带检查）
  if [ "$START_ORCHESTRATOR_BACKEND" = true ]; then
    if check_service_running "orchestrator" "$ORCHESTRATOR_BACKEND_PORT"; then
      .dev-bins/addp-orchestrator > logs/orchestrator-backend.log 2> logs/orchestrator-backend-stderr.log &
      ORCHESTRATOR_PID=$!
      echo $ORCHESTRATOR_PID > .dev-pids/orchestrator.pid
    else
      ORCHESTRATOR_PID=$(cat .dev-pids/orchestrator.pid 2>/dev/null)
    fi
  fi

  # 启动 Develop Backend（带检查）
  if [ "$START_DEVELOP_BACKEND" = true ]; then
    if check_service_running "develop" "$DEVELOP_BACKEND_PORT"; then
      .dev-bins/addp-develop > logs/develop-backend.log 2> logs/develop-backend-stderr.log &
      DEVELOP_PID=$!
      echo $DEVELOP_PID > .dev-pids/develop.pid
    else
      DEVELOP_PID=$(cat .dev-pids/develop.pid 2>/dev/null)
    fi
  fi

  # 启动 Service Backend（带检查）
  if [ "$START_SERVICE_BACKEND" = true ]; then
    if check_service_running "service" "$SERVICE_BACKEND_PORT"; then
      .dev-bins/addp-service > logs/service-backend.log 2> logs/service-backend-stderr.log &
      SERVICE_PID=$!
      echo $SERVICE_PID > .dev-pids/service.pid
    else
      SERVICE_PID=$(cat .dev-pids/service.pid 2>/dev/null)
    fi
  fi

  # 启动 Monitor Backend（带检查）
  if [ "$START_MONITOR_BACKEND" = true ]; then
    if check_service_running "monitor" "$MONITOR_BACKEND_PORT"; then
      .dev-bins/addp-monitor > logs/monitor-backend.log 2> logs/monitor-backend-stderr.log &
      MONITOR_PID=$!
      echo $MONITOR_PID > .dev-pids/monitor.pid
    else
      MONITOR_PID=$(cat .dev-pids/monitor.pid 2>/dev/null)
    fi
  fi

  # 启动 Standard Backend（带检查）
  if [ "$START_STANDARD_BACKEND" = true ]; then
    if check_service_running "standard" "$STANDARD_BACKEND_PORT"; then
      .dev-bins/addp-standard > logs/standard-backend.log 2> logs/standard-backend-stderr.log &
      STANDARD_PID=$!
      echo $STANDARD_PID > .dev-pids/standard.pid
    else
      STANDARD_PID=$(cat .dev-pids/standard.pid 2>/dev/null)
    fi
  fi

  # 启动 Model Backend（带检查）
  if [ "$START_MODEL_BACKEND" = true ]; then
    if check_service_running "model" "$MODEL_BACKEND_PORT"; then
      .dev-bins/addp-model > logs/model-backend.log 2> logs/model-backend-stderr.log &
      MODEL_PID=$!
      echo $MODEL_PID > .dev-pids/model.pid
    else
      MODEL_PID=$(cat .dev-pids/model.pid 2>/dev/null)
    fi
  fi

  # 启动 Quality Backend（带检查）
  if [ "$START_QUALITY_BACKEND" = true ]; then
    if check_service_running "quality" "$QUALITY_BACKEND_PORT"; then
      .dev-bins/addp-quality > logs/quality-backend.log 2> logs/quality-backend-stderr.log &
      QUALITY_PID=$!
      echo $QUALITY_PID > .dev-pids/quality.pid
    else
      QUALITY_PID=$(cat .dev-pids/quality.pid 2>/dev/null)
    fi
  fi

  # 启动 Asset Backend（带检查）
  if [ "$START_ASSET_BACKEND" = true ]; then
    if check_service_running "asset" "$ASSET_BACKEND_PORT"; then
      .dev-bins/addp-asset > logs/asset-backend.log 2> logs/asset-backend-stderr.log &
      ASSET_PID=$!
      echo $ASSET_PID > .dev-pids/asset.pid
    else
      ASSET_PID=$(cat .dev-pids/asset.pid 2>/dev/null)
    fi
  fi

  # 启动 Portal Backend（带检查）
  if [ "$START_PORTAL_BACKEND" = true ]; then
    if check_service_running "portal" "$PORTAL_BACKEND_PORT"; then
      .dev-bins/addp-portal > logs/portal-backend.log 2> logs/portal-backend-stderr.log &
      PORTAL_PID=$!
      echo $PORTAL_PID > .dev-pids/portal.pid
    else
      PORTAL_PID=$(cat .dev-pids/portal.pid 2>/dev/null)
    fi
  fi

  # 启动 Graph Backend（带检查）
  if [ "$START_GRAPH_BACKEND" = true ]; then
    if check_service_running "graph" "8186"; then
      .dev-bins/addp-graph > logs/graph-backend.log 2> logs/graph-backend-stderr.log &
      GRAPH_PID=$!
      echo $GRAPH_PID > .dev-pids/graph.pid
    else
      GRAPH_PID=$(cat .dev-pids/graph.pid 2>/dev/null)
    fi
  fi

  if [ "$START_INFERENCE_BACKEND" = true ]; then
    if check_service_running "inference" "${INFERENCE_BACKEND_PORT:-8191}"; then
      .dev-bins/addp-inference > logs/inference-backend.log 2> logs/inference-backend-stderr.log &
      INFERENCE_PID=$!
      echo $INFERENCE_PID > .dev-pids/inference.pid
    else
      INFERENCE_PID=$(cat .dev-pids/inference.pid 2>/dev/null)
    fi
  fi

  # 并行启动 Workers
  if [ "$START_META_WORKER" = true ]; then
    if check_service_running "meta-worker" ""; then
      .dev-bins/addp-meta-worker > logs/meta-worker.log 2>&1 &
      META_WORKER_PID=$!
      echo $META_WORKER_PID > .dev-pids/meta-worker.pid
    else
      META_WORKER_PID=$(cat .dev-pids/meta-worker.pid 2>/dev/null)
    fi
  fi

  if [ "$START_TRANSFER_BOUNDED_WORKER" = true ]; then
    if check_service_running "transfer-bounded-worker" ""; then
      .dev-bins/addp-transfer-bounded-worker > logs/transfer-bounded-worker.log 2>&1 &
      TRANSFER_BOUNDED_WORKER_PID=$!
      echo $TRANSFER_BOUNDED_WORKER_PID > .dev-pids/transfer-bounded-worker.pid
    else
      TRANSFER_BOUNDED_WORKER_PID=$(cat .dev-pids/transfer-bounded-worker.pid 2>/dev/null)
    fi
  fi

  if [ "$START_TRANSFER_CONTINUOUS_WORKER" = true ]; then
    if check_service_running "transfer-continuous-worker" ""; then
      .dev-bins/addp-transfer-continuous-worker > logs/transfer-continuous-worker.log 2>&1 &
      TRANSFER_CONTINUOUS_WORKER_PID=$!
      echo $TRANSFER_CONTINUOUS_WORKER_PID > .dev-pids/transfer-continuous-worker.pid
    else
      TRANSFER_CONTINUOUS_WORKER_PID=$(cat .dev-pids/transfer-continuous-worker.pid 2>/dev/null)
    fi
  fi

  echo "  ${GREEN}✓ 所有服务已启动，等待健康检查...${NC}"
fi

if [ "$START_MANAGER_BACKEND" = true ]; then
  echo "启动 Raster Mosaic Runtime..."
  RASTER_MOSAIC_RUNTIME_DIR="manager/raster-mosaic-runtime"
  RASTER_MOSAIC_RUNTIME_VENV="${RASTER_MOSAIC_RUNTIME_DIR}/venv"
  RASTER_MOSAIC_RUNTIME_STARTED=false
  RASTER_MOSAIC_RUNTIME_NEED_INSTALL=false
  RASTER_MOSAIC_RUNTIME_CAN_START=true
  if check_service_running "raster-mosaic-runtime" "$RASTER_MOSAIC_RUNTIME_PORT"; then
    SELECTED_PYTHON=$(select_python)
    if [ -d "$RASTER_MOSAIC_RUNTIME_VENV" ] &&
       ! "$RASTER_MOSAIC_RUNTIME_VENV/bin/python" - <<'PY' &> /dev/null
import flask, numpy, PIL
from osgeo import gdal
version = tuple(int(part) for part in numpy.__version__.split(".")[:2])
if version >= (2, 3):
    raise SystemExit(1)
PY
    then
      if "$SELECTED_PYTHON" -c "from osgeo import gdal" &> /dev/null; then
        echo "Raster Mosaic Runtime venv 缺少匹配的 GDAL/NumPy，重建为可继承系统 GDAL 的虚拟环境..."
        rm -rf "$RASTER_MOSAIC_RUNTIME_VENV"
      else
        RASTER_MOSAIC_RUNTIME_NEED_INSTALL=true
      fi
    fi
    if [ ! -d "$RASTER_MOSAIC_RUNTIME_VENV" ]; then
      $SELECTED_PYTHON -m venv --system-site-packages "$RASTER_MOSAIC_RUNTIME_VENV"
      RASTER_MOSAIC_RUNTIME_NEED_INSTALL=true
    elif ! "$RASTER_MOSAIC_RUNTIME_VENV/bin/python" -c "import flask, numpy, PIL" &> /dev/null; then
      RASTER_MOSAIC_RUNTIME_NEED_INSTALL=true
    fi
    if [ "$RASTER_MOSAIC_RUNTIME_NEED_INSTALL" = true ]; then
      PIP_CMD="$RASTER_MOSAIC_RUNTIME_VENV/bin/python -m pip install"
      if [ -n "$PIP_INDEX_URL" ]; then
        PIP_CMD="$PIP_CMD -i $PIP_INDEX_URL"
        if [ -n "$PIP_TRUSTED_HOST" ]; then
          PIP_CMD="$PIP_CMD --trusted-host $PIP_TRUSTED_HOST"
        fi
      fi
      if ! $PIP_CMD --upgrade pip || ! $PIP_CMD -r "$RASTER_MOSAIC_RUNTIME_DIR/requirements.txt"; then
        echo -e "${YELLOW}⚠️  Raster Mosaic Runtime 依赖安装失败，已跳过启动；Manager 主服务继续启动。${NC}"
        RASTER_MOSAIC_RUNTIME_CAN_START=false
      fi
    fi
    if [ "$RASTER_MOSAIC_RUNTIME_CAN_START" = true ] &&
       ! "$RASTER_MOSAIC_RUNTIME_VENV/bin/python" -c "from osgeo import gdal" &> /dev/null; then
      echo -e "${YELLOW}⚠️  Raster Mosaic Runtime 缺少 GDAL Python 绑定(osgeo.gdal)，已跳过启动；Manager 主服务继续启动。${NC}"
      echo -e "${YELLOW}   提示：macOS 可先执行 brew install gdal，或使用 manager/raster-mosaic-runtime/Dockerfile。${NC}"
      RASTER_MOSAIC_RUNTIME_CAN_START=false
    fi
    if [ "$RASTER_MOSAIC_RUNTIME_CAN_START" = true ]; then
      PORT="$RASTER_MOSAIC_RUNTIME_PORT" \
      SYSTEM_URL="${SYSTEM_URL:-http://localhost:${SYSTEM_BACKEND_PORT:-8180}}" \
        "$RASTER_MOSAIC_RUNTIME_VENV/bin/python" "$RASTER_MOSAIC_RUNTIME_DIR/app.py" \
        > logs/raster-mosaic-runtime.log 2> logs/raster-mosaic-runtime-stderr.log &
      RASTER_MOSAIC_RUNTIME_PID=$!
      echo $RASTER_MOSAIC_RUNTIME_PID > .dev-pids/raster-mosaic-runtime.pid
      RASTER_MOSAIC_RUNTIME_STARTED=true
    fi
  else
    RASTER_MOSAIC_RUNTIME_PID=$(cat .dev-pids/raster-mosaic-runtime.pid 2>/dev/null)
    RASTER_MOSAIC_RUNTIME_STARTED=true
  fi
fi

# ============================================================
# Phase 3: 并行等待所有 Backends 健康检查
# ============================================================
if [ "$START_MANAGER_BACKEND" = true ] || [ "$START_META_BACKEND" = true ] || [ "$START_TRANSFER_BACKEND" = true ] || [ "$START_ORCHESTRATOR_BACKEND" = true ] || [ "$START_DEVELOP_BACKEND" = true ] || [ "$START_SERVICE_BACKEND" = true ] || [ "$START_STANDARD_BACKEND" = true ] || [ "$START_MONITOR_BACKEND" = true ] || [ "$START_MODEL_BACKEND" = true ] || [ "$START_ASSET_BACKEND" = true ] || [ "$START_PORTAL_BACKEND" = true ] || [ "$START_GRAPH_BACKEND" = true ] || [ "$START_INFERENCE_BACKEND" = true ]; then
  echo "  [3/3] 并行健康检查..."

  HEALTH_CHECK_PIDS=()

  # 并发等待 Manager Backend
  if [ "$START_MANAGER_BACKEND" = true ]; then
    (
      WAIT_COUNT=0
      until curl -f http://localhost:${MANAGER_BACKEND_PORT}/health > /dev/null 2>&1; do
        sleep 1
        WAIT_COUNT=$((WAIT_COUNT + 1))
        if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
          echo -e "${RED}✗ Manager Backend 启动超时${NC}"
          echo "查看日志: tail -f logs/manager-backend.log"
          exit 1
        fi
      done
    ) &
    HEALTH_CHECK_PIDS+=($!)
    if [ "$RASTER_MOSAIC_RUNTIME_STARTED" = true ]; then
      (
        WAIT_COUNT=0
        until curl -f http://localhost:${RASTER_MOSAIC_RUNTIME_PORT}/health > /dev/null 2>&1; do
          sleep 1
          WAIT_COUNT=$((WAIT_COUNT + 1))
          if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
            echo -e "${RED}✗ Raster Mosaic Runtime 启动超时${NC}"
            echo "查看日志: tail -f logs/raster-mosaic-runtime.log"
            exit 1
          fi
        done
      ) &
      HEALTH_CHECK_PIDS+=($!)
    fi
  fi

  # 并发等待 Meta Backend
  if [ "$START_META_BACKEND" = true ]; then
    (
      WAIT_COUNT=0
      until curl -f http://localhost:${META_BACKEND_PORT}/health > /dev/null 2>&1; do
        sleep 1
        WAIT_COUNT=$((WAIT_COUNT + 1))
        if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
          echo -e "${RED}✗ Meta Backend 启动超时${NC}"
          echo "查看日志: tail -f logs/meta-backend.log"
          exit 1
        fi
      done
    ) &
    HEALTH_CHECK_PIDS+=($!)
  fi

  # 并发等待 Transfer Backend
  if [ "$START_TRANSFER_BACKEND" = true ]; then
    (
      WAIT_COUNT=0
      until curl -f http://localhost:${TRANSFER_BACKEND_PORT}/health > /dev/null 2>&1; do
        sleep 1
        WAIT_COUNT=$((WAIT_COUNT + 1))
        if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
          echo -e "${RED}✗ Transfer Backend 启动超时${NC}"
          echo "查看日志: tail -f logs/transfer-backend-stderr.log"
          exit 1
        fi
      done
    ) &
    HEALTH_CHECK_PIDS+=($!)
  fi

  # 并发等待 Orchestrator Backend
  if [ "$START_ORCHESTRATOR_BACKEND" = true ]; then
    (
      WAIT_COUNT=0
      until curl -f http://localhost:${ORCHESTRATOR_BACKEND_PORT}/health > /dev/null 2>&1; do
        sleep 1
        WAIT_COUNT=$((WAIT_COUNT + 1))
        if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
          echo -e "${RED}✗ Orchestrator Backend 启动超时${NC}"
          echo "查看日志: tail -f logs/orchestrator-backend-stderr.log"
          exit 1
        fi
      done
    ) &
    HEALTH_CHECK_PIDS+=($!)
  fi

  # 并发等待 Develop Backend
  if [ "$START_DEVELOP_BACKEND" = true ]; then
    (
      WAIT_COUNT=0
      until curl -f http://localhost:${DEVELOP_BACKEND_PORT}/health > /dev/null 2>&1; do
        sleep 1
        WAIT_COUNT=$((WAIT_COUNT + 1))
        if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
          echo -e "${RED}✗ Develop Backend 启动超时${NC}"
          echo "查看日志: tail -f logs/develop-backend.log"
          exit 1
        fi
      done
    ) &
    HEALTH_CHECK_PIDS+=($!)
  fi

  # 并发等待 Service Backend
  if [ "$START_SERVICE_BACKEND" = true ]; then
    (
      WAIT_COUNT=0
      until curl -f http://localhost:${SERVICE_BACKEND_PORT}/health > /dev/null 2>&1; do
        sleep 1
        WAIT_COUNT=$((WAIT_COUNT + 1))
        if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
          echo -e "${RED}✗ Service Backend 启动超时${NC}"
          echo "查看日志: tail -f logs/service-backend.log"
          exit 1
        fi
      done
    ) &
    HEALTH_CHECK_PIDS+=($!)
  fi

  if [ "$START_INFERENCE_BACKEND" = true ]; then
    (
      WAIT_COUNT=0
      until curl -f "http://localhost:${INFERENCE_BACKEND_PORT:-8191}/health" > /dev/null 2>&1; do
        sleep 1
        WAIT_COUNT=$((WAIT_COUNT + 1))
        if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
          echo -e "${RED}✗ Inference Backend 启动超时${NC}"
          echo "查看日志: tail -f logs/inference-backend-stderr.log"
          exit 1
        fi
      done
    ) &
    HEALTH_CHECK_PIDS+=($!)
  fi

  # 等待所有并发的 health check 完成
  for pid in "${HEALTH_CHECK_PIDS[@]}"; do
    wait "$pid" || true
  done

  echo ""
  echo -e "${GREEN}✓ 后端服务和选定 Worker 全部就绪${NC}"
fi
echo "  Manager Backend:    PID $MANAGER_PID (http://localhost:${MANAGER_BACKEND_PORT})"
echo "  Meta Backend:       PID $META_PID (http://localhost:${META_BACKEND_PORT})"
echo "  Transfer Backend:   PID $TRANSFER_PID (http://localhost:${TRANSFER_BACKEND_PORT})"
echo "  Orchestrator Backend: PID $ORCHESTRATOR_PID (http://localhost:${ORCHESTRATOR_BACKEND_PORT})"
echo "  Develop Backend:    PID $DEVELOP_PID (http://localhost:${DEVELOP_BACKEND_PORT})"
echo "  Service Backend:    PID $SERVICE_PID (http://localhost:${SERVICE_BACKEND_PORT})"
echo "  Meta Worker:        PID $META_WORKER_PID"
echo "  Transfer Bounded Worker: PID $TRANSFER_BOUNDED_WORKER_PID"
echo "  Transfer Continuous Worker: PID $TRANSFER_CONTINUOUS_WORKER_PID"
echo ""

# DuckDB Runtime 必须在 System 和 Meta 就绪后启动。扩展只在启动准备阶段
# 下载并校验，请求处理阶段仅允许从本地目录 LOAD。
if [ "$START_DUCKDB" = true ]; then
  echo -e "${YELLOW}Step 3.5/5: 准备并启动 DuckDB Federated Query Runtime${NC}"
  if curl -fsS "http://localhost:${DUCKDB_RUNTIME_PORT}/health" > /dev/null 2>&1; then
    DUCKDB_LISTENER_PID=$(lsof -ti :"${DUCKDB_RUNTIME_PORT}" -sTCP:LISTEN 2>/dev/null | head -1)
    DUCKDB_MANAGED_PID=$(cat .dev-pids/duckdb.pid 2>/dev/null || true)
    DUCKDB_LISTENER_COMMAND=$(ps -p "${DUCKDB_LISTENER_PID}" -o command= 2>/dev/null || true)
    if [ -n "$DUCKDB_MANAGED_PID" ] && [ "$DUCKDB_LISTENER_PID" = "$DUCKDB_MANAGED_PID" ] &&
      echo "$DUCKDB_LISTENER_COMMAND" | grep -qE '(^|/)addp-duckdb([[:space:]]|$)'; then
      DUCKDB_PID=$DUCKDB_MANAGED_PID
      echo "复用已就绪的本地 DuckDB Runtime (PID: $DUCKDB_PID)"
    else
      echo -e "${RED}✗ 端口 ${DUCKDB_RUNTIME_PORT} 上存在非 dev 脚本管理的 DuckDB Runtime${NC}"
      echo -e "${YELLOW}  监听进程: ${DUCKDB_LISTENER_COMMAND:-unknown} (PID: ${DUCKDB_LISTENER_PID:-unknown})${NC}"
      echo -e "${YELLOW}  开发模式只启动本地二进制；如为镜像实例，请先执行: docker compose stop duckdb-engine${NC}"
      exit 1
    fi
  else
    DUCKDB_EXTENSION_DIRECTORY="${DUCKDB_EXTENSION_DIRECTORY:-.cache/duckdb/extensions}"
    if [[ "$DUCKDB_EXTENSION_DIRECTORY" != /* ]]; then
      DUCKDB_EXTENSION_DIRECTORY="${PROJECT_ROOT}/${DUCKDB_EXTENSION_DIRECTORY}"
    fi
    export DUCKDB_EXTENSION_DIRECTORY

    echo "准备 DuckDB 扩展: ${DUCKDB_EXTENSION_DIRECTORY}"
    .dev-bins/addp-duckdb-prepare --output "${DUCKDB_EXTENSION_DIRECTORY}"

    if ! check_service_running "duckdb" "$DUCKDB_RUNTIME_PORT"; then
      echo -e "${RED}✗ DuckDB Runtime 端口已占用但健康检查失败${NC}"
      exit 1
    fi
    .dev-bins/addp-duckdb > logs/duckdb-runtime.log 2> logs/duckdb-runtime-stderr.log &
    DUCKDB_PID=$!
    echo "$DUCKDB_PID" > .dev-pids/duckdb.pid
  fi

  echo -n "等待 DuckDB Runtime 就绪"
  WAIT_COUNT=0
  until curl -f "http://localhost:${DUCKDB_RUNTIME_PORT}/health" > /dev/null 2>&1; do
    echo -n "."
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ "$WAIT_COUNT" -ge "$MAX_WAIT" ]; then
      echo ""
      echo -e "${RED}✗ DuckDB Runtime 启动超时${NC}"
      echo "查看日志: tail -f logs/duckdb-runtime-stderr.log"
      exit 1
    fi
  done
  echo ""
  echo -e "${GREEN}✓ DuckDB Runtime 就绪 (PID: ${DUCKDB_PID}, 端口: ${DUCKDB_RUNTIME_PORT})${NC}"
  echo ""
fi

# ============================================================
# Step 4: Start GeoPython Workflow Engine (Python service)
# ============================================================
if [ "$START_PYTHON_WORKFLOW" = true ]; then
  echo -e "${YELLOW}Step 4/5: 启动 GeoPython Workflow Engine...${NC}"

  # 检查 Python 3 是否安装
  if ! command -v python3 &> /dev/null; then
      echo -e "${RED}✗ Python 3 未安装，请先安装 Python 3.11+${NC}"
      exit 1
  fi

  # 检查 Python 版本（需要 3.11+）
  PYTHON_VERSION=$(python3 -c 'import sys; print(".".join(map(str, sys.version_info[:2])))')
  REQUIRED_VERSION="3.11"
  if [ "$(printf '%s\n' "$REQUIRED_VERSION" "$PYTHON_VERSION" | sort -V | head -n1)" != "$REQUIRED_VERSION" ]; then
      echo -e "${RED}✗ Python 版本过低 ($PYTHON_VERSION)，需要 3.11+${NC}"
      exit 1
  fi

# 检查并创建虚拟环境（幂等）
NEED_INSTALL=false
if [ ! -d "engines/python-workflow/venv" ]; then
    echo "首次启动，创建 Python 虚拟环境..."
    cd engines/python-workflow
    # 使用智能 Python 选择（优先 3.11）
    SELECTED_PYTHON=$(select_python)
    PYTHON_VER=$($SELECTED_PYTHON --version)
    echo "  使用 $PYTHON_VER"
    $SELECTED_PYTHON -m venv --system-site-packages venv
    NEED_INSTALL=true
else
    if ! ./engines/python-workflow/venv/bin/python - <<'PY' &> /dev/null
import flask, geopandas, pyarrow, pyproj, addp_common
from osgeo import gdal
import numpy
version = tuple(int(part) for part in numpy.__version__.split(".")[:2])
if version >= (2, 3):
    raise SystemExit(1)
PY
    then
        SELECTED_PYTHON=$(select_python)
        if "$SELECTED_PYTHON" -c "from osgeo import gdal" &> /dev/null; then
            echo "GeoPython Workflow venv 缺少匹配的 GDAL/NumPy，重建为可继承系统 GDAL 的虚拟环境..."
            rm -rf engines/python-workflow/venv
            cd engines/python-workflow
            PYTHON_VER=$($SELECTED_PYTHON --version)
            echo "  使用 $PYTHON_VER"
            $SELECTED_PYTHON -m venv --system-site-packages venv
            NEED_INSTALL=true
        fi
    fi
    # 检查关键依赖是否已安装
    if [ "$NEED_INSTALL" = false ] && ! ./engines/python-workflow/venv/bin/python -c "import flask, geopandas, pyarrow, pyproj, addp_common" &> /dev/null; then
        echo "检测到虚拟环境缺少依赖，重新安装..."
        cd engines/python-workflow
        NEED_INSTALL=true
    elif [ "$NEED_INSTALL" = false ]; then
        echo "虚拟环境已存在且依赖完整，跳过安装"
    fi
fi

if [ "$NEED_INSTALL" = true ]; then
    # 使用 pip 安装依赖（更稳定，避免 uv 虚拟环境识别问题）
    echo "使用 pip 安装依赖（首次安装可能需要 1-2 分钟）..."

    # 构建 pip 安装命令（支持镜像源配置）
    PIP_CMD="./venv/bin/python -m pip install"
    if [ -n "$PIP_INDEX_URL" ]; then
        echo "  使用镜像源: $PIP_INDEX_URL"
        PIP_CMD="$PIP_CMD -i $PIP_INDEX_URL"
        if [ -n "$PIP_TRUSTED_HOST" ]; then
            PIP_CMD="$PIP_CMD --trusted-host $PIP_TRUSTED_HOST"
        fi
    else
        echo "  使用官方源（国外可能较慢，建议在 .env 中配置 PIP_INDEX_URL）"
    fi

    # 升级 pip 并安装依赖
    $PIP_CMD --upgrade pip
    $PIP_CMD -r requirements.txt
    $PIP_CMD -e ../../common-python

    # 检查安装是否成功
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Python 依赖安装完成${NC}"
        if ! ./venv/bin/python -c "from osgeo import gdal" &> /dev/null; then
            echo -e "${YELLOW}⚠️  GeoPython Workflow 缺少 GDAL Python 绑定(osgeo.gdal)，栅格 mosaic 生成算子将不可用。${NC}"
            echo -e "${YELLOW}   提示：macOS 可先执行 brew install gdal，或使用 GeoPython Workflow 容器镜像。${NC}"
        fi
    else
        echo -e "${RED}✗ Python 依赖安装失败，请检查错误信息${NC}"
        echo -e "${YELLOW}提示：某些依赖可能需要系统库支持（如 GDAL）${NC}"
        echo -e "${YELLOW}macOS: brew install gdal${NC}"
        echo -e "${YELLOW}Ubuntu: sudo apt-get install libgdal-dev${NC}"
        cd ..
        exit 1
    fi
    cd ..
fi

# 启动 GeoPython Workflow Engine
if check_service_running "python-workflow-engine" "$PYTHON_WORKFLOW_PORT"; then
  echo "启动 GeoPython Workflow Engine..."
  cd engines/python-workflow

  # 设置环境变量
  export PORT=$PYTHON_WORKFLOW_PORT
  # SYSTEM_URL 已由 generate_service_urls() 自动生成
  export GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET=${GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET:-""}
  export POSTGRES_HOST=localhost
  export POSTGRES_PORT=15432
  export POSTGRES_USER=addp
  export POSTGRES_PASSWORD=addp_password
  export POSTGRES_DB=addp
  export DB_SCHEMA=develop

  # 直接使用虚拟环境的 Python（无需 activate）
  ./venv/bin/python api_server.py > ../../logs/python-workflow-engine.log 2> ../../logs/python-workflow-engine-stderr.log &
  PYTHON_WORKFLOW_PID=$!
  echo $PYTHON_WORKFLOW_PID > ../../.dev-pids/python-workflow-engine.pid
  cd ../..

  echo -e "${GREEN}✓ GeoPython Workflow Engine 已启动 (PID: $PYTHON_WORKFLOW_PID)${NC}"

  # 等待健康检查通过
  echo -n "等待 GeoPython Workflow Engine 就绪..."
  WAIT_COUNT=0
  MAX_WAIT=60
  until curl -f http://localhost:${PYTHON_WORKFLOW_PORT}/health > /dev/null 2>&1; do
    echo -n "."
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e " ${RED}✗${NC}"
      echo -e "${RED}✗ GeoPython Workflow Engine 启动超时（60秒）${NC}"
      echo -e "${YELLOW}查看日志: tail -f logs/python-workflow-engine.log${NC}"
      echo -e "${YELLOW}或检查错误: tail -f logs/python-workflow-engine-stderr.log${NC}"
      exit 1
    fi
  done
  echo -e " ${GREEN}✓${NC}"
  echo -e "${GREEN}✓ GeoPython Workflow Engine 就绪 (http://localhost:${PYTHON_WORKFLOW_PORT})${NC}"
else
  PYTHON_WORKFLOW_PID=$(cat .dev-pids/python-workflow-engine.pid 2>/dev/null)
  echo -e "${GREEN}✓ GeoPython Workflow Engine 已在运行 (PID: $PYTHON_WORKFLOW_PID)${NC}"
fi
  echo ""
else
  echo -e "${YELLOW}Step 4/5: 跳过 GeoPython Workflow Engine${NC}"
  echo ""
fi

# ============================================================
# Step 4.2: Start Math Workflow Engine (Python service)
# ============================================================

if [ "$START_MATH_WORKFLOW" = true ]; then
  echo -e "${BLUE}Step 4.2/5: 启动 Math Workflow Engine${NC}"

# 检查并创建虚拟环境（幂等）
NEED_INSTALL=false
if [ ! -d "engines/math-workflow/venv" ]; then
    echo "首次启动，创建 Python 虚拟环境..."
    cd engines/math-workflow
    # 使用智能 Python 选择（优先 3.11）
    SELECTED_PYTHON=$(select_python)
    PYTHON_VER=$($SELECTED_PYTHON --version)
    echo "  使用 $PYTHON_VER"
    $SELECTED_PYTHON -m venv venv
    NEED_INSTALL=true
else
    # 检查关键依赖是否已安装
    if ! ./engines/math-workflow/venv/bin/python -c "import flask, addp_common.workflow_runtime" &> /dev/null; then
        echo "检测到虚拟环境缺少依赖，重新安装..."
        cd engines/math-workflow
        NEED_INSTALL=true
    else
        echo "虚拟环境已存在且依赖完整，跳过安装"
    fi
fi

if [ "$NEED_INSTALL" = true ]; then
    # 使用 pip 安装依赖
    echo "使用 pip 安装依赖..."

    # 构建 pip 安装命令（支持镜像源配置）
    PIP_CMD="./venv/bin/python -m pip install"
    if [ -n "$PIP_INDEX_URL" ]; then
        echo "  使用镜像源: $PIP_INDEX_URL"
        PIP_CMD="$PIP_CMD -i $PIP_INDEX_URL"
        if [ -n "$PIP_TRUSTED_HOST" ]; then
            PIP_CMD="$PIP_CMD --trusted-host $PIP_TRUSTED_HOST"
        fi
    fi

    # 升级 pip 并安装依赖
    $PIP_CMD --upgrade pip
    $PIP_CMD -r requirements.txt
    $PIP_CMD -e ../../common-python

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Python 依赖安装完成${NC}"
    else
        echo -e "${RED}✗ Python 依赖安装失败${NC}"
        exit 1
    fi
    cd ../..
fi

# 启动 Math Workflow Engine
if check_service_running "math-workflow-engine" "$MATH_WORKFLOW_PORT"; then
  echo "启动 Math Workflow Engine..."
  cd engines/math-workflow

  # 设置环境变量
  export PORT=$MATH_WORKFLOW_PORT
  # 直接使用虚拟环境的 Python
  ./venv/bin/python api_server.py > ../../logs/math-workflow-engine.log 2> ../../logs/math-workflow-engine-stderr.log &
  MATH_WORKFLOW_PID=$!
  echo $MATH_WORKFLOW_PID > ../../.dev-pids/math-workflow-engine.pid
  cd ../..

  echo -e "${GREEN}✓ Math Workflow Engine 已启动 (PID: $MATH_WORKFLOW_PID)${NC}"

  # 等待服务就绪
  echo -n "  等待服务就绪"
  MAX_WAIT=60
  WAIT_COUNT=0
  while ! curl -s http://localhost:${MATH_WORKFLOW_PORT}/health > /dev/null 2>&1; do
    sleep 1
    echo -n "."
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e " ${RED}✗${NC}"
      echo -e "${RED}✗ Math Workflow Engine 启动超时（60秒）${NC}"
      echo -e "${YELLOW}查看日志: tail -f logs/math-workflow-engine.log${NC}"
      echo -e "${YELLOW}或检查错误: tail -f logs/math-workflow-engine-stderr.log${NC}"
      exit 1
    fi
  done
  echo -e " ${GREEN}✓${NC}"
  echo -e "${GREEN}✓ Math Workflow Engine 就绪 (http://localhost:${MATH_WORKFLOW_PORT})${NC}"
else
  MATH_WORKFLOW_PID=$(cat .dev-pids/math-workflow-engine.pid 2>/dev/null)
  echo -e "${GREEN}✓ Math Workflow Engine 已在运行 (PID: $MATH_WORKFLOW_PID)${NC}"
fi
  echo ""
else
  echo -e "${YELLOW}Step 4.2/5: 跳过 Math Workflow Engine${NC}"
  echo ""
fi

# ============================================================
# Step 4.3: Start Model3D Workflow Engine (Python service)
# ============================================================

if [ "$START_MODEL3D_WORKFLOW" = true ]; then
  echo -e "${BLUE}Step 4.3/5: 启动 Model3D Workflow Engine${NC}"

NEED_INSTALL=false
if [ ! -d "engines/model3d-workflow/venv" ]; then
    echo "首次启动，创建 Python 虚拟环境..."
    cd engines/model3d-workflow
    SELECTED_PYTHON=$(select_python)
    PYTHON_VER=$($SELECTED_PYTHON --version)
    echo "  使用 $PYTHON_VER"
    $SELECTED_PYTHON -m venv venv
    NEED_INSTALL=true
else
    if ! ./engines/model3d-workflow/venv/bin/python -c "import flask, addp_common.workflow_runtime" &> /dev/null; then
        echo "检测到虚拟环境缺少依赖，重新安装..."
        cd engines/model3d-workflow
        NEED_INSTALL=true
    else
        echo "虚拟环境已存在且依赖完整，跳过安装"
    fi
fi

if [ "$NEED_INSTALL" = true ]; then
    echo "使用 pip 安装依赖..."
    PIP_CMD="./venv/bin/python -m pip install"
    if [ -n "$PIP_INDEX_URL" ]; then
        echo "  使用镜像源: $PIP_INDEX_URL"
        PIP_CMD="$PIP_CMD -i $PIP_INDEX_URL"
        if [ -n "$PIP_TRUSTED_HOST" ]; then
            PIP_CMD="$PIP_CMD --trusted-host $PIP_TRUSTED_HOST"
        fi
    fi

    $PIP_CMD --upgrade pip
    $PIP_CMD -r requirements.txt
    $PIP_CMD -e ../../common-python

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Python 依赖安装完成${NC}"
    else
        echo -e "${RED}✗ Python 依赖安装失败${NC}"
        exit 1
    fi
    cd ../..
fi

ensure_model3d_node_dependencies

start_model3d_workflow_engine_process() {
  echo "启动 Model3D Workflow Engine..."
  cd engines/model3d-workflow

  export PORT=$MODEL3D_WORKFLOW_PORT
  export MODEL3D_WORKFLOW_SERVICE_CLIENT_SECRET=${MODEL3D_WORKFLOW_SERVICE_CLIENT_SECRET:-""}

  ./venv/bin/python api_server.py > ../../logs/model3d-workflow-engine.log 2> ../../logs/model3d-workflow-engine-stderr.log &
  MODEL3D_WORKFLOW_PID=$!
  echo $MODEL3D_WORKFLOW_PID > ../../.dev-pids/model3d-workflow-engine.pid
  cd ../..

  echo -e "${GREEN}✓ Model3D Workflow Engine 已启动 (PID: $MODEL3D_WORKFLOW_PID)${NC}"

  echo -n "  等待服务就绪"
  MAX_WAIT=60
  WAIT_COUNT=0
  while ! curl -s http://localhost:${MODEL3D_WORKFLOW_PORT}/health > /dev/null 2>&1; do
    sleep 1
    echo -n "."
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e " ${RED}✗${NC}"
      echo -e "${RED}✗ Model3D Workflow Engine 启动超时（60秒）${NC}"
      echo -e "${YELLOW}查看日志: tail -f logs/model3d-workflow-engine.log${NC}"
      echo -e "${YELLOW}或检查错误: tail -f logs/model3d-workflow-engine-stderr.log${NC}"
      exit 1
    fi
  done
  echo -e " ${GREEN}✓${NC}"
  echo -e "${GREEN}✓ Model3D Workflow Engine 就绪 (http://localhost:${MODEL3D_WORKFLOW_PORT})${NC}"
}

if curl -s "http://localhost:${MODEL3D_WORKFLOW_PORT}/health" 2>/dev/null | grep -q '"service":"model3d-workflow-engine"'; then
  MODEL3D_WORKFLOW_PID=$(cat .dev-pids/model3d-workflow-engine.pid 2>/dev/null || true)
  if [ -n "$MODEL3D_WORKFLOW_PID" ] && ps -p "$MODEL3D_WORKFLOW_PID" > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Model3D Workflow Engine 已在运行 (PID: $MODEL3D_WORKFLOW_PID)${NC}"
  elif docker ps --filter "name=^/model3d-workflow-engine$" --format '{{.Names}}' 2>/dev/null | grep -qx "model3d-workflow-engine"; then
    echo -e "${YELLOW}⚠️  检测到 Docker 版 Model3D Workflow Engine 正占用 ${MODEL3D_WORKFLOW_PORT}${NC}"
    echo -e "${YELLOW}   dev 模式需要宿主机 Python runtime，以便与 Manager 统一访问 infra MinIO localhost:${MINIO_API_PORT:-19000}${NC}"
    echo "  停止 Docker 版 Model3D Workflow Engine..."
    docker rm -f model3d-workflow-engine >/dev/null
    rm -f .dev-pids/model3d-workflow-engine.pid
    echo -e "${GREEN}✓ Docker 版 Model3D Workflow Engine 已停止，继续启动宿主机 runtime${NC}"
    start_model3d_workflow_engine_process
  else
    echo -e "${GREEN}✓ Model3D Workflow Engine 已在运行 (http://localhost:${MODEL3D_WORKFLOW_PORT})${NC}"
  fi
elif check_service_running "model3d-workflow-engine" "$MODEL3D_WORKFLOW_PORT"; then
  start_model3d_workflow_engine_process
else
  MODEL3D_WORKFLOW_PID=$(cat .dev-pids/model3d-workflow-engine.pid 2>/dev/null)
  echo -e "${GREEN}✓ Model3D Workflow Engine 已在运行 (PID: $MODEL3D_WORKFLOW_PID)${NC}"
fi
  echo ""
else
  echo -e "${YELLOW}Step 4.3/5: 跳过 Model3D Workflow Engine${NC}"
  echo ""
fi

# ============================================================
# Step 4.4: Start PointCloud Workflow Engine (Docker runtime)
# ============================================================

if [ "$START_POINTCLOUD_WORKFLOW" = true ]; then
  echo -e "${BLUE}Step 4.4/5: 启动 PointCloud Workflow Engine${NC}"

pointcloud_workflow_source_fingerprint() {
  {
    printf '%s\n' "pointcloud-workflow-image-v1"
    while IFS= read -r file; do
      printf '%s %s\n' "$file" "$(git hash-object "$file")"
    done < <(
      {
        printf '%s\n' \
          engines/pointcloud-workflow/Dockerfile \
          engines/pointcloud-workflow/requirements.txt \
          engines/pointcloud-workflow/api_server.py \
          engines/pointcloud-workflow/operators.py \
          common-python/pyproject.toml
        printf '%s\n' \
          common-python/README.md \
          common-python/addp_common/__init__.py \
          common-python/addp_common/workflow_access.py
        find common-python/addp_common/client common-python/addp_common/workflow_runtime \
          -type f ! -path '*/__pycache__/*' ! -name '*.pyc'
      } | LC_ALL=C sort
    )
  } | git hash-object --stdin
}

ensure_pointcloud_workflow_image() {
  local image="$1"
  local fingerprint
  local current_fingerprint
  fingerprint="$(pointcloud_workflow_source_fingerprint)"
  current_fingerprint="$(docker image inspect \
    -f '{{ index .Config.Labels "addp.pointcloud.source-fingerprint" }}' \
    "$image" 2>/dev/null || true)"

  if [ "$current_fingerprint" = "$fingerprint" ]; then
    echo "PointCloud Workflow Engine 镜像构建输入未变化，复用现有镜像: $image"
    return 0
  fi

  echo "构建 PointCloud Workflow Engine 镜像（构建输入已变化或镜像不存在）..."
  docker build \
    --label "addp.pointcloud.source-fingerprint=${fingerprint}" \
    -f engines/pointcloud-workflow/Dockerfile \
    -t "$image" \
    .
}

start_pointcloud_workflow_engine_process() {
  if ! command -v docker >/dev/null 2>&1; then
    echo -e "${RED}✗ PointCloud Workflow Engine 需要 Docker runtime 承载 PDAL${NC}"
    exit 1
  fi

  local image="${POINTCLOUD_WORKFLOW_IMAGE:-addp-pointcloud-workflow-engine:dev}"
  local source_dir="${POINTCLOUD_DATA_HOST_PATH:-${ROOT_DIR}/business/nfs/data}"
  local container_source_dir="${POINTCLOUD_DATA_CONTAINER_PATH:-${ROOT_DIR}/business/nfs/data}"
  local work_dir="${POINTCLOUD_WORK_HOST_PATH:-${ROOT_DIR}/data/pointcloud-work}"
  local system_port="${SYSTEM_BACKEND_PORT:-8180}"
  local minio_port="${MINIO_API_PORT:-19000}"

  ensure_pointcloud_workflow_image "$image"

  echo "启动 PointCloud Workflow Engine Docker runtime..."
  docker rm -f pointcloud-workflow-engine >/dev/null 2>&1 || true
  pkill -9 -f "engines/pointcloud-workflow/api_server.py" 2>/dev/null || true
  mkdir -p "${work_dir}"
  mkdir -p .dev-pids
  POINTCLOUD_WORKFLOW_PID=$(
    docker run -d \
      --name pointcloud-workflow-engine \
      --label com.docker.compose.project=addp-app \
      --label com.docker.compose.service=pointcloud-workflow-engine \
      --label com.docker.compose.project.working_dir="${ROOT_DIR}" \
      --add-host=host.docker.internal:host-gateway \
      -p "${POINTCLOUD_WORKFLOW_PORT}:8102" \
      -e PORT=8102 \
      -e SYSTEM_URL="http://host.docker.internal:${system_port}" \
      -e POINTCLOUD_WORKFLOW_SERVICE_CLIENT_SECRET="${POINTCLOUD_WORKFLOW_SERVICE_CLIENT_SECRET:-}" \
      -e POINTCLOUD_PDAL_BIN=/opt/conda/bin/pdal \
      -e POINTCLOUD_WORK_DIR=/work/pointcloud \
      -e CPL_TMPDIR=/work/pointcloud \
      -e RUNTIME_HOST=localhost \
      -e POINTCLOUD_OBJECT_STORE_LOCALHOST_ENDPOINT="host.docker.internal:${minio_port}" \
      -v "${ROOT_DIR}/logs:/app/logs" \
      -v "${work_dir}:/work/pointcloud" \
      -v "${ROOT_DIR}/engines/pointcloud-workflow/api_server.py:/app/api_server.py:ro" \
      -v "${ROOT_DIR}/engines/pointcloud-workflow/operators.py:/app/operators.py:ro" \
      -v "${source_dir}:${container_source_dir}:ro" \
      "$image"
  )
  echo "$POINTCLOUD_WORKFLOW_PID" > .dev-pids/pointcloud-workflow-engine.pid

  echo -e "${GREEN}✓ PointCloud Workflow Engine 容器已启动 (${POINTCLOUD_WORKFLOW_PID})${NC}"

  echo -n "  等待服务就绪"
  MAX_WAIT=60
  WAIT_COUNT=0
  while ! curl -s "http://localhost:${POINTCLOUD_WORKFLOW_PORT}/health" | grep -q '"status":"healthy"'; do
    if ! docker ps --filter "name=^/pointcloud-workflow-engine$" --format '{{.Names}}' | grep -qx "pointcloud-workflow-engine"; then
      echo -e " ${RED}✗${NC}"
      echo -e "${RED}✗ PointCloud Workflow Engine 容器已退出${NC}"
      docker logs --tail 100 pointcloud-workflow-engine 2>&1 || true
      exit 1
    fi
    sleep 1
    echo -n "."
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e " ${RED}✗${NC}"
      echo -e "${RED}✗ PointCloud Workflow Engine 启动超时（60秒）${NC}"
      echo -e "${YELLOW}查看日志: docker logs pointcloud-workflow-engine${NC}"
      exit 1
    fi
  done
  echo -e " ${GREEN}✓${NC}"
  echo -e "${GREEN}✓ PointCloud Workflow Engine 就绪 (http://localhost:${POINTCLOUD_WORKFLOW_PORT})${NC}"
}

if curl -s "http://localhost:${POINTCLOUD_WORKFLOW_PORT}/health" 2>/dev/null | grep -q '"service":"pointcloud-workflow-engine"'; then
  POINTCLOUD_WORKFLOW_PID=$(cat .dev-pids/pointcloud-workflow-engine.pid 2>/dev/null || true)
  if docker ps --filter "name=^/pointcloud-workflow-engine$" --format '{{.Names}}' 2>/dev/null | grep -qx "pointcloud-workflow-engine"; then
    if curl -s "http://localhost:${POINTCLOUD_WORKFLOW_PORT}/health" | grep -q '"status":"healthy"'; then
      echo -e "${GREEN}✓ PointCloud Workflow Engine Docker runtime 已在运行 (${POINTCLOUD_WORKFLOW_PID:-pointcloud-workflow-engine})${NC}"
    else
      echo -e "${YELLOW}⚠️  PointCloud Workflow Engine 当前不是 healthy，重建 Docker runtime${NC}"
      start_pointcloud_workflow_engine_process
    fi
  else
    echo -e "${YELLOW}⚠️  检测到非容器 PointCloud Workflow Engine 正占用 ${POINTCLOUD_WORKFLOW_PORT}，切换到 Docker runtime${NC}"
    start_pointcloud_workflow_engine_process
  fi
elif check_service_running "pointcloud-workflow-engine" "$POINTCLOUD_WORKFLOW_PORT"; then
  start_pointcloud_workflow_engine_process
else
  occupying_pid=$(lsof -ti :${POINTCLOUD_WORKFLOW_PORT} -sTCP:LISTEN 2>/dev/null || true)
  occupying_cmd=$(ps -p "$occupying_pid" -o command= 2>/dev/null || true)
  if echo "$occupying_cmd" | grep -qE "engines/pointcloud-workflow|api_server\\.py"; then
    echo -e "${YELLOW}⚠️  清理旧 PointCloud Workflow Engine 进程并切换到 Docker runtime${NC}"
    start_pointcloud_workflow_engine_process
  else
    echo -e "${RED}✗ PointCloud Workflow Engine 端口 ${POINTCLOUD_WORKFLOW_PORT} 被占用，无法启动 Docker runtime${NC}"
    echo -e "${YELLOW}  进程: $(echo "$occupying_cmd" | cut -c1-80)${NC}"
    exit 1
  fi
fi
  echo ""
else
  echo -e "${YELLOW}Step 4.4/5: 跳过 PointCloud Workflow Engine${NC}"
  echo ""
fi

# ============================================================
# Step 4.45: Start SuperMap Workflow Engine (Docker runtime)
# ============================================================

if [ "$START_SUPERMAP_WORKFLOW" = true ]; then
  echo -e "${BLUE}Step 4.45/5: 启动 SuperMap Workflow Engine${NC}"

start_supermap_workflow_engine_process() {
  bash "${SCRIPT_DIR}/supermap-workflow.sh"
  SUPERMAP_WORKFLOW_PID=$(cat .dev-pids/supermap-workflow-engine.pid 2>/dev/null || echo supermap-workflow-engine)
}

if curl -s "http://localhost:${SUPERMAP_WORKFLOW_PORT}/health" 2>/dev/null | grep -q '"service":"supermap-workflow-engine"'; then
  if docker ps --filter "name=^/supermap-workflow-engine$" --format '{{.Names}}' 2>/dev/null | grep -qx "supermap-workflow-engine"; then
    if curl -s "http://localhost:${SUPERMAP_WORKFLOW_PORT}/health" | grep -q '"status":"healthy"'; then
      SUPERMAP_WORKFLOW_PID=$(cat .dev-pids/supermap-workflow-engine.pid 2>/dev/null || echo supermap-workflow-engine)
      echo -e "${GREEN}✓ SuperMap Workflow Engine Docker runtime 已在运行 (${SUPERMAP_WORKFLOW_PID})${NC}"
    else
      echo -e "${YELLOW}⚠️  SuperMap Workflow Engine 当前不是 healthy，重建 Docker runtime${NC}"
      start_supermap_workflow_engine_process
    fi
  else
    echo -e "${YELLOW}⚠️  检测到非容器 SuperMap Workflow Engine 正占用 ${SUPERMAP_WORKFLOW_PORT}，切换到 Docker runtime${NC}"
    start_supermap_workflow_engine_process
  fi
elif check_service_running "supermap-workflow-engine" "$SUPERMAP_WORKFLOW_PORT"; then
  start_supermap_workflow_engine_process
else
  occupying_pid=$(lsof -ti :${SUPERMAP_WORKFLOW_PORT} -sTCP:LISTEN 2>/dev/null || true)
  occupying_cmd=$(ps -p "$occupying_pid" -o command= 2>/dev/null || true)
  echo -e "${RED}✗ SuperMap Workflow Engine 端口 ${SUPERMAP_WORKFLOW_PORT} 被占用，无法启动 Docker runtime${NC}"
  echo -e "${YELLOW}  进程: $(echo "$occupying_cmd" | cut -c1-80)${NC}"
  exit 1
fi
  echo ""
else
  echo -e "${YELLOW}Step 4.45/5: 跳过 SuperMap Workflow Engine${NC}"
  echo ""
fi

# ============================================================
# Step 4.5: Start Spark 工作流引擎 (Python service)
# ============================================================
if [ "$START_SPARK_WORKFLOW" = true ]; then
  echo -e "${YELLOW}Step 4.5/5: 启动 Spark 工作流引擎...${NC}"

  # 检查 Python 3 是否安装
  if ! command -v python3 &> /dev/null; then
      echo -e "${RED}✗ Python 3 未安装，请先安装 Python 3.11+${NC}"
      exit 1
  fi

  # 检查 Python 版本（需要 3.11+）
  PYTHON_VERSION=$(python3 -c 'import sys; print(".".join(map(str, sys.version_info[:2])))')
  REQUIRED_VERSION="3.11"
  if [ "$(printf '%s\n' "$REQUIRED_VERSION" "$PYTHON_VERSION" | sort -V | head -n1)" != "$REQUIRED_VERSION" ]; then
      echo -e "${RED}✗ Python 版本过低 ($PYTHON_VERSION)，需要 3.11+${NC}"
      exit 1
  fi

# 检查并创建虚拟环境（幂等）
NEED_INSTALL=false
if [ ! -d "engines/spark-workflow/venv" ]; then
    echo "首次启动，创建 Python 虚拟环境..."
    cd engines/spark-workflow
    # 优先使用兼容性好的 Python 版本（避免新版本兼容性问题）
    # 优先级: 3.12 > 3.13 > 3.11 > 系统默认
    if command -v /opt/homebrew/bin/python3.12 &> /dev/null; then
        echo "  使用 Homebrew Python $(/opt/homebrew/bin/python3.12 --version)"
        /opt/homebrew/bin/python3.12 -m venv venv
    elif command -v /opt/homebrew/bin/python3.13 &> /dev/null; then
        echo "  使用 Homebrew Python $(/opt/homebrew/bin/python3.13 --version)"
        /opt/homebrew/bin/python3.13 -m venv venv
    elif command -v /opt/homebrew/bin/python3.11 &> /dev/null; then
        echo "  使用 Homebrew Python $(/opt/homebrew/bin/python3.11 --version)"
        /opt/homebrew/bin/python3.11 -m venv venv
    elif command -v /opt/homebrew/bin/python3 &> /dev/null; then
        echo "  使用 Homebrew Python $(/opt/homebrew/bin/python3 --version)"
        /opt/homebrew/bin/python3 -m venv venv
    else
        python3 -m venv venv
    fi
    NEED_INSTALL=true
else
    # 检查关键依赖是否已安装
    if ! ./engines/spark-workflow/venv/bin/python -c "import pyspark, addp_common.workflow_runtime" &> /dev/null; then
        echo "检测到虚拟环境缺少依赖，重新安装..."
        cd engines/spark-workflow
        NEED_INSTALL=true
    else
        echo "虚拟环境已存在且依赖完整，跳过安装"
    fi
fi

if [ "$NEED_INSTALL" = true ]; then
    # 使用 pip 安装依赖（更稳定，避免 uv 虚拟环境识别问题）
    echo "使用 pip 安装依赖（首次安装可能需要 1-2 分钟）..."

    # 构建 pip 安装命令（支持镜像源配置）
    PIP_CMD="./venv/bin/python -m pip install"
    if [ -n "$PIP_INDEX_URL" ]; then
        echo "  使用镜像源: $PIP_INDEX_URL"
        PIP_CMD="$PIP_CMD -i $PIP_INDEX_URL"
        if [ -n "$PIP_TRUSTED_HOST" ]; then
            PIP_CMD="$PIP_CMD --trusted-host $PIP_TRUSTED_HOST"
        fi
    else
        echo "  使用官方源（国外可能较慢，建议在 .env 中配置 PIP_INDEX_URL）"
    fi

    # 升级 pip 并安装依赖
    $PIP_CMD --upgrade pip
    $PIP_CMD -r requirements.txt
    $PIP_CMD -e ../../common-python

    # 检查安装是否成功
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Python 依赖安装完成${NC}"
    else
        echo -e "${RED}✗ Python 依赖安装失败，请检查错误信息${NC}"
        echo -e "${YELLOW}提示：某些依赖可能需要系统库支持（如 PySpark）${NC}"
        cd ../..
        exit 1
    fi
    cd ../..
fi

# 启动 Spark 工作流引擎
if check_service_running "spark-workflow-engine" "$SPARK_WORKFLOW_PORT"; then
  echo "启动 Spark 工作流引擎..."
  configure_spark_workflow_java
  cd engines/spark-workflow

  # 设置环境变量
  export PORT=$SPARK_WORKFLOW_PORT
  # SYSTEM_URL 已由 generate_service_urls() 自动生成
  export SPARK_WORKFLOW_SERVICE_CLIENT_SECRET=${SPARK_WORKFLOW_SERVICE_CLIENT_SECRET:-""}
  export SPARK_WORKFLOW_SHARED_HOST="${SPARK_WORKFLOW_SHARED_HOST:-$(detect_spark_workflow_shared_host)}"

  # 直接使用虚拟环境的 Python（无需 activate）
  ./venv/bin/python api_server.py > ../../logs/spark-workflow-engine.log 2> ../../logs/spark-workflow-engine-stderr.log &
  SPARK_WORKFLOW_PID=$!
  echo $SPARK_WORKFLOW_PID > ../../.dev-pids/spark-workflow-engine.pid
  cd ../..

  echo -e "${GREEN}✓ Spark 工作流引擎 已启动 (PID: $SPARK_WORKFLOW_PID)${NC}"

  # 等待健康检查通过
  echo -n "等待 Spark 工作流引擎 就绪..."
  WAIT_COUNT=0
  MAX_WAIT=60
  until curl -f http://localhost:${SPARK_WORKFLOW_PORT}/health > /dev/null 2>&1; do
    echo -n "."
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e " ${RED}✗${NC}"
      echo -e "${RED}✗ Spark 工作流引擎 启动超时（60秒）${NC}"
      echo -e "${YELLOW}查看日志: tail -f logs/spark-workflow-engine.log${NC}"
      echo -e "${YELLOW}或检查错误: tail -f logs/spark-workflow-engine-stderr.log${NC}"
      exit 1
    fi
  done
  echo -e " ${GREEN}✓${NC}"
  echo -e "${GREEN}✓ Spark 工作流引擎 就绪 (http://localhost:${SPARK_WORKFLOW_PORT})${NC}"
else
  SPARK_WORKFLOW_PID=$(cat .dev-pids/spark-workflow-engine.pid 2>/dev/null)
  echo -e "${GREEN}✓ Spark 工作流引擎 已在运行 (PID: $SPARK_WORKFLOW_PID)${NC}"
fi
  echo ""
else
  echo -e "${YELLOW}Step 4.5/5: 跳过 Spark 工作流引擎${NC}"
  echo ""
fi

# ============================================================
# Step 4.6: Start Jupyter Engine (Python service)
# ============================================================
if [ "$START_JUPYTER" = true ]; then
  echo -e "${YELLOW}Step 4.6/5: 启动 Jupyter Engine...${NC}"

  ensure_jupyter_python_env "$ROOT_DIR"

# 启动 Jupyter Engine
if check_service_running "jupyter-engine" "$JUPYTER_API_PORT"; then
  echo "启动 Jupyter Notebook Runtime..."
  cd engines/jupyter

  # 设置环境变量
  export API_PORT=$JUPYTER_API_PORT

  # 启动 API Server（后台）
  ./venv/bin/python api_server.py > ../../logs/jupyter-api-server.log 2> ../../logs/jupyter-api-server-stderr.log &
  API_SERVER_PID=$!
  echo $API_SERVER_PID > ../../.dev-pids/jupyter-api-server.pid

  cd ../..

  echo -e "${GREEN}✓ Jupyter Notebook Runtime 已启动:${NC}"
  echo -e "  - API Server (PID: $API_SERVER_PID, Port: $JUPYTER_API_PORT)"

  # 等待 API Server 健康检查通过
  echo -n "等待 API Server 就绪..."
  WAIT_COUNT=0
  MAX_WAIT=60
  until curl -f http://localhost:${JUPYTER_API_PORT}/health > /dev/null 2>&1; do
    echo -n "."
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e " ${RED}✗${NC}"
      echo -e "${RED}✗ API Server 启动超时（60秒）${NC}"
      echo -e "${YELLOW}查看日志: tail -f logs/jupyter-api-server.log${NC}"
      echo -e "${YELLOW}或检查错误: tail -f logs/jupyter-api-server-stderr.log${NC}"
      exit 1
    fi
  done
  echo -e " ${GREEN}✓${NC}"

  echo -e "${GREEN}✓ Jupyter Notebook Runtime 就绪:${NC}"
  echo -e "  - API Server: http://localhost:${JUPYTER_API_PORT}"
else
  API_SERVER_PID=$(cat .dev-pids/jupyter-api-server.pid 2>/dev/null)
  echo -e "${GREEN}✓ Jupyter Notebook Runtime 已在运行:${NC}"
  echo -e "  - API Server (PID: $API_SERVER_PID)"
fi
  echo ""
else
  echo -e "${YELLOW}Step 4.6/5: 跳过 Jupyter Engine${NC}"
  echo ""
fi

# ============================================================
# Step 5: Start Copilot Backend (Python/FastAPI service)
# ============================================================
if [ "$START_COPILOT_BACKEND" = true ]; then
  echo -e "${YELLOW}Step 5/6: 启动 Copilot Backend...${NC}"

  # 检查并创建虚拟环境（幂等）
  NEED_INSTALL=false
  if [ ! -d "copilot/backend/venv" ]; then
    echo "首次启动 Copilot，创建 Python 虚拟环境..."
    cd copilot/backend
    # 使用智能 Python 选择（优先 3.11）
    SELECTED_PYTHON=$(select_python)
    PYTHON_VER=$($SELECTED_PYTHON --version)
    echo "  使用 $PYTHON_VER"
    $SELECTED_PYTHON -m venv venv
    NEED_INSTALL=true
else
    # 检查关键依赖是否已安装
    if ! ./copilot/backend/venv/bin/python -c "import fastapi" &> /dev/null; then
        echo "检测到虚拟环境缺少依赖，重新安装..."
        cd copilot/backend
        NEED_INSTALL=true
    else
        echo "虚拟环境已存在且依赖完整，跳过安装"
    fi
fi

if [ "$NEED_INSTALL" = true ]; then
    echo "使用 pip 安装 Copilot 依赖（首次安装可能需要 1-2 分钟）..."

    # 构建 pip 安装命令（支持镜像源配置）
    PIP_CMD="./venv/bin/python -m pip install"
    if [ -n "$PIP_INDEX_URL" ]; then
        echo "  使用镜像源: $PIP_INDEX_URL"
        PIP_CMD="$PIP_CMD -i $PIP_INDEX_URL"
        if [ -n "$PIP_TRUSTED_HOST" ]; then
            PIP_CMD="$PIP_CMD --trusted-host $PIP_TRUSTED_HOST"
        fi
    else
        echo "  使用官方源（国外可能较慢，建议在 .env 中配置 PIP_INDEX_URL）"
    fi

    # 升级 pip 并安装依赖
    $PIP_CMD --upgrade pip
    $PIP_CMD -r requirements.txt

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Copilot Python 依赖安装完成${NC}"
    else
        echo -e "${RED}✗ Copilot 依赖安装失败，请检查错误信息${NC}"
        cd ../..
        exit 1
    fi
    cd ../..
fi

# 启动 Copilot Backend
if check_service_running "copilot-backend" "$COPILOT_BACKEND_PORT"; then
  echo "启动 Copilot Backend..."
  cd copilot/backend

  # 设置环境变量
  export PORT=$COPILOT_BACKEND_PORT
  # SYSTEM_URL 已由 generate_service_urls() 自动生成
  export DATABASE_URL=postgresql://addp:addp_password@localhost:${POSTGRES_PORT}/addp

  # 直接使用虚拟环境的 Python
  ./venv/bin/python main.py > ../../logs/copilot-backend.log 2> ../../logs/copilot-backend-stderr.log &
  COPILOT_PID=$!
  echo $COPILOT_PID > ../../.dev-pids/copilot-backend.pid
  cd ../..

  echo -e "${GREEN}✓ Copilot Backend 已启动 (PID: $COPILOT_PID)${NC}"

  # 等待健康检查通过
  echo -n "等待 Copilot Backend 就绪..."
  WAIT_COUNT=0
  MAX_WAIT=60
  until curl -f http://localhost:${COPILOT_BACKEND_PORT}/health > /dev/null 2>&1; do
    echo -n "."
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e " ${RED}✗${NC}"
      echo -e "${RED}✗ Copilot Backend 启动超时（60秒）${NC}"
      echo -e "${YELLOW}查看日志: tail -f logs/copilot-backend.log${NC}"
      echo -e "${YELLOW}或检查错误: tail -f logs/copilot-backend-stderr.log${NC}"
      exit 1
    fi
  done
  echo -e " ${GREEN}✓${NC}"
    echo -e "${GREEN}✓ Copilot Backend 就绪 (http://localhost:${COPILOT_BACKEND_PORT})${NC}"
else
  COPILOT_PID=$(cat .dev-pids/copilot-backend.pid 2>/dev/null)
  echo -e "${GREEN}✓ Copilot Backend 已在运行 (PID: $COPILOT_PID)${NC}"
fi
  echo ""
else
  echo -e "${YELLOW}Step 5/6: 跳过 Copilot Backend${NC}"
  echo ""
fi

# ============================================================
# Step 5b: Start Agent Backend (Python/FastAPI service)
# ============================================================
if [ "$START_AGENT_BACKEND" = true ]; then
  echo -e "${YELLOW}Step 5b: 启动 Agent Backend...${NC}"

  NEED_INSTALL=false
  if [ ! -d "agent/backend/venv" ]; then
    echo "首次启动 Agent，创建 Python 虚拟环境..."
    cd agent/backend
    SELECTED_PYTHON=$(select_python)
    PYTHON_VER=$($SELECTED_PYTHON --version)
    echo "  使用 $PYTHON_VER"
    $SELECTED_PYTHON -m venv venv
    NEED_INSTALL=true
  else
    if ! ./agent/backend/venv/bin/python -c "import fastapi" &> /dev/null; then
      echo "检测到虚拟环境缺少依赖，重新安装..."
      cd agent/backend
      NEED_INSTALL=true
    else
      echo "虚拟环境已存在且依赖完整，跳过安装"
    fi
  fi

  if [ "$NEED_INSTALL" = true ]; then
    echo "使用 pip 安装 Agent 依赖（首次安装可能需要 1-2 分钟）..."
    PIP_CMD="./venv/bin/python -m pip install"
    if [ -n "$PIP_INDEX_URL" ]; then
      echo "  使用镜像源: $PIP_INDEX_URL"
      PIP_CMD="$PIP_CMD -i $PIP_INDEX_URL"
      if [ -n "$PIP_TRUSTED_HOST" ]; then
        PIP_CMD="$PIP_CMD --trusted-host $PIP_TRUSTED_HOST"
      fi
    fi
    $PIP_CMD --upgrade pip
    $PIP_CMD -r requirements.txt
    if [ $? -eq 0 ]; then
      echo -e "${GREEN}✓ Agent Python 依赖安装完成${NC}"
    else
      echo -e "${RED}✗ Agent 依赖安装失败，请检查错误信息${NC}"
      cd ../..
      exit 1
    fi
    cd ../..
  fi

  if check_service_running "agent-backend" "$AGENT_BACKEND_PORT"; then
    echo "启动 Agent Backend..."
    cd agent/backend
    export PORT=$AGENT_BACKEND_PORT
    # 使用 main.py 绝对路径启动，便于 pkill -f "agent/backend/main.py" 精确匹配残留进程
    ./venv/bin/python "${PROJECT_ROOT}/agent/backend/main.py" > ../../logs/agent-backend.log 2> ../../logs/agent-backend-stderr.log &
    AGENT_PID=$!
    echo $AGENT_PID > ../../.dev-pids/agent-backend.pid
    cd ../..

    echo -e "${GREEN}✓ Agent Backend 已启动 (PID: $AGENT_PID)${NC}"

    echo -n "等待 Agent Backend 就绪..."
    WAIT_COUNT=0
    MAX_WAIT=60
    until curl -f http://localhost:${AGENT_BACKEND_PORT}/health > /dev/null 2>&1; do
      echo -n "."
      sleep 1
      WAIT_COUNT=$((WAIT_COUNT + 1))
      if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
        echo -e " ${RED}✗${NC}"
        echo -e "${RED}✗ Agent Backend 启动超时（60秒）${NC}"
        echo -e "${YELLOW}查看日志: tail -f logs/agent-backend.log${NC}"
        echo -e "${YELLOW}或检查错误: tail -f logs/agent-backend-stderr.log${NC}"
        exit 1
      fi
    done
    echo -e " ${GREEN}✓${NC}"
    echo -e "${GREEN}✓ Agent Backend 就绪 (http://localhost:${AGENT_BACKEND_PORT})${NC}"
  else
    AGENT_PID=$(cat .dev-pids/agent-backend.pid 2>/dev/null)
    echo -e "${GREEN}✓ Agent Backend 已在运行 (PID: $AGENT_PID)${NC}"
  fi
  echo ""
else
  echo -e "${YELLOW}Step 5b: 跳过 Agent Backend${NC}"
  echo ""
fi

# 6. 启动 Gateway
if [ "$START_GATEWAY" = true ]; then
  echo -e "${YELLOW}Step 6/7: 启动 Gateway${NC}"

  if check_service_running "gateway" "$GATEWAY_PORT"; then
  build_gateway
  # 重置 PORT 环境变量为 Gateway 的端口
  export PORT=$GATEWAY_PORT
  .dev-bins/addp-gateway > logs/gateway.log 2> logs/gateway-stderr.log &
  GATEWAY_PID=$!
  echo $GATEWAY_PID > .dev-pids/gateway.pid

  # 等待 Gateway 就绪
  echo "等待 Gateway 就绪..."
  WAIT_COUNT=0
  until curl -f http://localhost:${GATEWAY_PORT}/health > /dev/null 2>&1; do
    echo -n "."
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
      echo -e "${RED}✗ Gateway 启动超时${NC}"
      echo "查看日志: tail -f logs/gateway.log"
      exit 1
    fi
  done
  echo -e "${GREEN}✓ Gateway 就绪 (PID: $GATEWAY_PID)${NC}"
else
  GATEWAY_PID=$(cat .dev-pids/gateway.pid 2>/dev/null)
  echo -e "${GREEN}✓ Gateway 已在运行 (PID: $GATEWAY_PID)${NC}"
fi
  echo ""
else
  echo -e "${YELLOW}Step 6/7: 跳过 Gateway${NC}"
  echo ""
fi

# 7. 启动前端服务（保持原有并行逻辑）
echo -e "${YELLOW}Step 7/7: 启动前端服务${NC}"

# 创建 PID 目录
mkdir -p .dev-pids

# 等待 HTTP 服务就绪的小工具函数
wait_for_http() {
  local name="$1"
  local url="$2"
  local max_wait="${3:-60}"
  local count=0
  echo "等待 ${name} 就绪 (${url})..."
  until curl -fsS "$url" > /dev/null 2>&1; do
    echo -n "."
    sleep 1
    count=$((count + 1))
    if [ "$count" -ge "$max_wait" ]; then
      echo -e "\n${RED}✗ ${name} 启动超时${NC}"
      return 1
    fi
  done
  echo -e "\n${GREEN}✓ ${name} 就绪${NC}"
}

# 缺依赖时自动安装
ensure_node_modules() {
  local dir="$1"
  local vite_bin="$dir/node_modules/.bin/vite"
  if [ ! -d "$dir/node_modules" ]; then
    echo "检测到 $dir 缺少依赖，执行 npm install ..."
    (cd "$dir" && npm install)
  fi
  # 某些情况下 node_modules 存在但 devDependencies 未装全，补装一次
  if [ ! -x "$vite_bin" ]; then
    echo "未发现 Vite，可执行文件缺失：$vite_bin"
    echo "在 $dir 重新安装依赖 (包含 devDependencies)..."
    (cd "$dir" && npm install)
  fi
  if [ ! -x "$vite_bin" ]; then
    echo -e "${RED}✗ 依赖安装后仍未找到 Vite: $vite_bin${NC}"
    echo "请检查网络代理或手动执行: (cd $dir && npm install)"
    exit 1
  fi
}

# ============================================================
# 并发启动所有前端服务（Bash 3.2 兼容）
# ============================================================
# 检查是否有任何前端需要启动
if [ "$START_CONSOLE" = true ] || [ "$START_SYSTEM_FRONTEND" = true ] || [ "$START_MANAGER_FRONTEND" = true ] || [ "$START_META_FRONTEND" = true ] || [ "$START_TRANSFER_FRONTEND" = true ] || [ "$START_ORCHESTRATOR_FRONTEND" = true ] || [ "$START_DEVELOP_FRONTEND" = true ] || [ "$START_SERVICE_FRONTEND" = true ] || [ "$START_MONITOR_FRONTEND" = true ] || [ "$START_STANDARD_FRONTEND" = true ] || [ "$START_MODEL_FRONTEND" = true ] || [ "$START_QUALITY_FRONTEND" = true ] || [ "$START_ASSET_FRONTEND" = true ] || [ "$START_PORTAL_FRONTEND" = true ] || [ "$START_AGENT_FRONTEND" = true ] || [ "$START_GRAPH_FRONTEND" = true ]; then
  echo -e "${YELLOW}Step 8/8: 并发启动前端服务${NC}"

  # 动态构建前端配置（格式：名称:端口:目录）
  # 使用普通数组而非关联数组（兼容 Bash 3.2）
  FRONTEND_CONFIGS=()

  if [ "$START_CONSOLE" = true ]; then
    FRONTEND_CONFIGS+=("console:${CONSOLE_FE_PORT}:console/frontend")
  fi

  if [ "$START_SYSTEM_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("system:${SYSTEM_FE_PORT}:system/frontend")
  fi

  if [ "$START_MANAGER_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("manager:${MANAGER_FE_PORT}:manager/frontend")
  fi

  if [ "$START_META_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("meta:${META_FE_PORT}:meta/frontend")
  fi

  if [ "$START_TRANSFER_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("transfer:${TRANSFER_FE_PORT}:transfer/frontend")
  fi

  if [ "$START_ORCHESTRATOR_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("orchestrator:${ORCHESTRATOR_FE_PORT}:orchestrator/frontend")
  fi

  if [ "$START_DEVELOP_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("develop:${DEVELOP_FE_PORT}:develop/frontend")
  fi

  if [ "$START_SERVICE_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("service:${SERVICE_FE_PORT}:service/frontend")
  fi

  if [ "$START_MONITOR_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("monitor:${MONITOR_FE_PORT}:monitor/frontend")
  fi

  if [ "$START_STANDARD_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("standard:${STANDARD_FE_PORT}:standard/frontend")
  fi

  if [ "$START_MODEL_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("model:${MODEL_FE_PORT}:model/frontend")
  fi

  if [ "$START_QUALITY_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("quality:${QUALITY_FE_PORT}:quality/frontend")
  fi

  if [ "$START_ASSET_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("asset:${ASSET_FE_PORT}:asset/frontend")
  fi

  if [ "$START_PORTAL_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("portal:${PORTAL_FE_PORT}:portal/frontend")
  fi

  if [ "$START_AGENT_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("agent:${AGENT_FE_PORT}:agent/frontend")
  fi

  if [ "$START_GRAPH_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("graph:5187:graph/frontend")
  fi

  if [ "$START_INFERENCE_FRONTEND" = true ]; then
    FRONTEND_CONFIGS+=("inference:${INFERENCE_FE_PORT}:inference/frontend")
  fi

  echo "并发启动所有前端..."

  # 存储 PIDs（使用临时文件）
  FRONTEND_PID_FILE="/tmp/addp-frontend-pids-$$"
  > "$FRONTEND_PID_FILE"  # 清空文件

# 并发启动所有前端
for config in "${FRONTEND_CONFIGS[@]}"; do
  IFS=':' read -r name port dir <<< "$config"

  # 检查前端服务是否已在运行
  if check_service_running "${name}-frontend" "$port"; then
    (
      ensure_node_modules "$dir"
      cd "$dir"
      npm run dev -- --host 0.0.0.0 --port "$port" > "../../logs/${name}-frontend.log" 2>&1
    ) &

    pid=$!
    echo "${name}:${pid}" >> "$FRONTEND_PID_FILE"
    echo "  启动 ${name} Frontend (PID: $pid, Port: $port)"
  else
    # 服务已运行，从 PID 文件读取
    existing_pid=$(cat ".dev-pids/${name}-frontend.pid" 2>/dev/null)
    if [ -n "$existing_pid" ]; then
      echo "${name}:${existing_pid}" >> "$FRONTEND_PID_FILE"
    fi
  fi
done

echo ""
echo "并发等待所有前端就绪..."

# 并发等待所有前端的健康检查
MAX_WAIT=60
HEALTH_CHECK_PIDS=()
for config in "${FRONTEND_CONFIGS[@]}"; do
  IFS=':' read -r name port dir <<< "$config"

  (
    WAIT_COUNT=0
    until curl -fsS "http://localhost:${port}" > /dev/null 2>&1; do
      sleep 1
      WAIT_COUNT=$((WAIT_COUNT + 1))
      if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
        echo -e "${RED}✗ ${name} Frontend 启动超时 (Port: ${port})${NC}"
        echo "查看日志: tail -f logs/${name}-frontend.log"
        exit 1
      fi
    done
    # 从临时文件中查找 PID
    pid=$(grep "^${name}:" "$FRONTEND_PID_FILE" | cut -d: -f2)
    echo -e "${GREEN}✓ ${name} Frontend 就绪 (PID: ${pid}, Port: ${port})${NC}"
  ) &
  HEALTH_CHECK_PIDS+=($!)
done

# 只等待健康检查进程完成（不等待前端 npm 进程）
for pid in "${HEALTH_CHECK_PIDS[@]}"; do
  wait "$pid"
done

echo ""
echo -e "${GREEN}✓ 所有前端服务已启动${NC}"

# 保存前端 PIDs 到 .dev-pids 目录
while IFS=: read -r name pid; do
  echo "$pid" > ".dev-pids/${name}-frontend.pid"
done < "$FRONTEND_PID_FILE"

# 为了兼容性，设置这些变量（从临时文件读取）
CONSOLE_PID=$(grep "^console:" "$FRONTEND_PID_FILE" | cut -d: -f2)
SYSTEM_FE_PID=$(grep "^system:" "$FRONTEND_PID_FILE" | cut -d: -f2)
MANAGER_FE_PID=$(grep "^manager:" "$FRONTEND_PID_FILE" | cut -d: -f2)
META_FE_PID=$(grep "^meta:" "$FRONTEND_PID_FILE" | cut -d: -f2)
TRANSFER_FE_PID=$(grep "^transfer:" "$FRONTEND_PID_FILE" | cut -d: -f2)
ORCHESTRATOR_FE_PID=$(grep "^orchestrator:" "$FRONTEND_PID_FILE" | cut -d: -f2)
DEVELOP_FE_PID=$(grep "^develop:" "$FRONTEND_PID_FILE" | cut -d: -f2)

# 清理临时文件
rm -f "$FRONTEND_PID_FILE"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ ADDP 开发环境启动完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "服务地址:"
echo "  Console FE:    http://localhost:${CONSOLE_FE_PORT}"
echo "  Gateway:  http://localhost:${GATEWAY_PORT}"
echo "  System:   http://localhost:${SYSTEM_BACKEND_PORT}"
echo "  Manager:  http://localhost:${MANAGER_BACKEND_PORT}"
echo "  Meta:     http://localhost:${META_BACKEND_PORT}"
echo "  Transfer: http://localhost:${TRANSFER_BACKEND_PORT}"
echo "  Orchestrator: http://localhost:${ORCHESTRATOR_BACKEND_PORT}"
echo "  Develop:  http://localhost:${DEVELOP_BACKEND_PORT}"
echo "  Service:  http://localhost:${SERVICE_BACKEND_PORT}"
echo "  Copilot:  http://localhost:${COPILOT_BACKEND_PORT}"
echo "  Monitor:  http://localhost:${MONITOR_BACKEND_PORT}"
echo "  Standard: http://localhost:${STANDARD_BACKEND_PORT}"
echo "  Model:    http://localhost:${MODEL_BACKEND_PORT}"
echo "  Quality:  http://localhost:${QUALITY_BACKEND_PORT}"
  echo "  Asset:    http://localhost:${ASSET_BACKEND_PORT}"
  echo "  Portal:   http://localhost:${PORTAL_BACKEND_PORT}"
echo "  Jupyter Runtime:     http://localhost:${JUPYTER_API_PORT}"
echo "  Spark 工作流引擎: http://localhost:${SPARK_WORKFLOW_PORT}"
echo "  GeoPython Workflow Engine:    http://localhost:${PYTHON_WORKFLOW_PORT}"
echo "  Model3D Workflow Engine:   http://localhost:${MODEL3D_WORKFLOW_PORT}"
echo "  PointCloud Workflow Engine: http://localhost:${POINTCLOUD_WORKFLOW_PORT}"
echo "  SuperMap Workflow Engine:  http://localhost:${SUPERMAP_WORKFLOW_PORT}"
echo "  DuckDB Runtime:            http://localhost:${DUCKDB_RUNTIME_PORT}"
echo "  Raster Mosaic Runtime:     http://localhost:${RASTER_MOSAIC_RUNTIME_PORT}"
echo "  System FE:    http://localhost:${SYSTEM_FE_PORT}"
echo "  Manager FE:   http://localhost:${MANAGER_FE_PORT}"
echo "  Meta FE:      http://localhost:${META_FE_PORT}"
echo "  Transfer FE:  http://localhost:${TRANSFER_FE_PORT}"
echo "  Orchestrator FE: http://localhost:${ORCHESTRATOR_FE_PORT}"
echo "  Develop FE:   http://localhost:${DEVELOP_FE_PORT}"
echo "  Service FE:   http://localhost:${SERVICE_FE_PORT}"
echo "  Monitor FE:   http://localhost:${MONITOR_FE_PORT}"
echo "  Standard FE:  http://localhost:${STANDARD_FE_PORT}"
echo "  Model FE:     http://localhost:${MODEL_FE_PORT}"
echo "  Quality FE:   http://localhost:${QUALITY_FE_PORT}"
  echo "  Asset FE:     http://localhost:${ASSET_FE_PORT}"
  echo "  Portal FE:    http://localhost:${PORTAL_FE_PORT}"
echo ""
echo "后端服务 PID:"
echo "  System Backend:       $SYSTEM_PID"
echo "  Manager Backend:      $MANAGER_PID"
echo "  Meta Backend:         $META_PID"
echo "  Transfer Backend:     $TRANSFER_PID"
echo "  Orchestrator Backend: $ORCHESTRATOR_PID"
echo "  Develop Backend:      $DEVELOP_PID"
echo "  Service Backend:      $SERVICE_PID"
echo "  DuckDB Runtime:       $DUCKDB_PID"
echo "  Raster Mosaic Runtime:      $RASTER_MOSAIC_RUNTIME_PID"
echo "  GeoPython Workflow Engine:     $PYTHON_WORKFLOW_PID"
echo "  Model3D Workflow Engine:    $MODEL3D_WORKFLOW_PID"
echo "  PointCloud Workflow Engine: $POINTCLOUD_WORKFLOW_PID"
echo "  SuperMap Workflow Engine:  $SUPERMAP_WORKFLOW_PID"
echo "  Spark 工作流引擎:  $SPARK_WORKFLOW_PID"
echo "  Jupyter Engine:       $JUPYTER_PID"
echo "  Copilot Backend:      $COPILOT_PID"
echo "  Monitor Backend:      $MONITOR_PID"
echo "  Standard Backend:     $STANDARD_PID"
echo "  Model Backend:        $MODEL_PID"
echo "  Quality Backend:      $QUALITY_PID"
echo "  Inference Backend:    $INFERENCE_PID"
echo "  Gateway:              $GATEWAY_PID"
echo ""
echo "Workers PID:"
echo "  Meta Worker:          $META_WORKER_PID"
echo "  Transfer Bounded Worker: $TRANSFER_BOUNDED_WORKER_PID"
echo "  Transfer Continuous Worker: $TRANSFER_CONTINUOUS_WORKER_PID"
echo ""
echo "日志文件:"
echo "  System:   logs/system-backend.log"
echo "  Manager:  logs/manager-backend.log"
echo "  Meta:     logs/meta-backend.log"
echo "  Transfer: logs/transfer-backend.log"
echo "  Orchestrator: logs/orchestrator-backend.log"
echo "  Develop:  logs/develop-backend.log"
echo "  Service:  logs/service-backend.log"
echo "  DuckDB Runtime: logs/duckdb-runtime.log"
echo "  Copilot:  logs/copilot-backend.log"
echo "  Agent:    logs/agent-backend.log"
echo "  Monitor:  logs/monitor-backend.log"
echo "  Standard: logs/standard-backend.log"
echo "  Model:    logs/model-backend.log"
echo "  Quality:  logs/quality-backend.log"
echo "  Inference: logs/inference-backend.log"
echo "  GeoPython Workflow Engine: logs/python-workflow-engine.log"
echo "  Math Workflow Engine: logs/math-workflow-engine.log (显式 -math-workflow 启动时)"
echo "  Model3D Workflow Engine: logs/model3d-workflow-engine.log"
echo "  PointCloud Workflow Engine: docker logs pointcloud-workflow-engine"
echo "  SuperMap Workflow Engine: docker logs supermap-workflow-engine"
echo "  Spark 工作流引擎: logs/spark-workflow-engine.log"
echo "  Jupyter Engine: logs/jupyter-engine.log"
echo "  Gateway:  logs/gateway.log"
echo "  Transfer Bounded Worker: logs/transfer-bounded-worker.log"
echo "  Transfer Continuous Worker: logs/transfer-continuous-worker.log"
echo "  Meta Worker: logs/meta-worker.log"
echo "  Meta FE:  logs/meta-frontend.log"
echo "  Transfer FE:  logs/transfer-frontend.log"
echo "  Develop FE:  logs/develop-frontend.log"
echo "  Service FE:  logs/service-frontend.log"
echo ""
echo "停止所有服务: make dev-stop 或 ./scripts/dev/stop.sh"
echo ""
else
  echo -e "${YELLOW}Step 8/8: 跳过前端服务启动${NC}"
  echo ""
  echo -e "${GREEN}========================================${NC}"
  echo -e "${GREEN}✓ ADDP 开发环境启动完成！${NC}"
  echo -e "${GREEN}========================================${NC}"
  echo ""
  echo "停止所有服务: make dev-stop 或 ./scripts/dev/stop.sh"
  echo ""
fi
