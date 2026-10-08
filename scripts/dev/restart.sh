#!/bin/bash
set -e

# 使用说明
show_usage() {
  echo "用法: $0 [-all] [-system] [-manager] [-meta] [-transfer] [-orchestrator] [-develop] [-service] [-monitor] [-gateway] [-model] [-quality] [-security] [-asset] [-catalog] [-ontology] [-workbench] [-portal] [-inference] [-geopython-workflow] [-math-workflow] [-model3d-workflow] [-pointcloud-workflow] [-document-workflow] [-supermap-workflow] [-copilot] [-agent] [-spark-workflow] [-jupyter] [-duckdb]"
  echo ""
  echo "选项:"
  echo "  无参数        等同 -all，同步文档并按完整构建指纹增量编译"
  echo "  -all         重启所有模块，按构建输入变化增量编译 Go 和容器运行时"
  echo "  -system      重启并按需编译 System 模块"
  echo "  -manager     重启并按需编译 Manager 模块"
  echo "  -meta        重启并按需编译 Meta 模块"
  echo "  -transfer    重启并按需编译 Transfer 模块"
  echo "  -orchestrator 重启并按需编译 Orchestrator 模块"
  echo "  -develop     重启并按需编译 Develop 模块"
  echo "  -service     重启并按需编译 Service 模块"
  echo "  -monitor     重启并按需编译 Monitor 模块"
  echo "  -gateway     重启并按需编译 Gateway 模块"
  echo "  -standard    重启并按需编译 Standard 模块"
  echo "  -model       重启并按需编译 Model 模块"
  echo "  -quality     重启并按需编译 Quality 模块"
  echo "  -security    重启并按需编译 Security 模块"
  echo "  -asset       重启并按需编译 Asset 模块"
  echo "  -catalog     重启并按需编译 Catalog 模块"
  echo "  -ontology    重启并按需编译 Ontology 模块"
  echo "  -workbench   重启并按需编译 Workbench 模块"
  echo "  -portal      重启并按需编译 Portal 模块"
  echo "  -graph       重启并按需编译 Graph 模块"
  echo "  -inference   重启并按需编译 Inference 模块"
  echo "  -geopython-workflow   重启 GeoPython Workflow (Python 服务)"
  echo "  -math-workflow     重启 Math Workflow Engine (Python 服务)"
  echo "  -model3d-workflow  重启 Model3D Workflow Engine (Python 服务)"
  echo "  -pointcloud-workflow 重启 PointCloud Workflow Engine (原生 Python/PDAL runtime)"
  echo "  -document-workflow 重启 Document Workflow Engine (原生 Python/LibreOffice runtime)"
  echo "  -supermap-workflow 重启 SuperMap Workflow Engine (C++ Docker runtime，需先构建基础镜像)"
  echo "  -copilot     重启 Copilot Backend (Python 服务)"
  echo "  -agent       重启 Agent Backend (Python 服务)"
  echo "  -spark-workflow 重启 Spark 工作流 Engine (原生 Python/JDK runtime)"
  echo "  -jupyter     重启 Jupyter Engine (Python 服务)"
  echo "  -duckdb      按需编译并重启 DuckDB Federated Query Runtime"
  echo ""
  echo "增量构建说明:"
  echo "  - 所有 Go 服务统一校验源码、共享依赖、工具链和编译参数"
  echo "  - 构建输入未变化时复用产物，变化或产物缺失时重新编译"
  echo "  - 无参数与 -all 均重新生成全部 Swagger，再校验构建指纹"
  echo "  - 指定模块时只重启所选模块，保留其他模块和运行中的公共依赖"
  echo "  - 支持多个模块参数；-all 不能与模块参数混用"
  echo ""
  echo "注意:"
  echo "  - GeoPython Workflow、Math Workflow Engine、Spark 工作流 Engine、PointCloud Workflow Engine、SuperMap Workflow Engine、Jupyter Engine、Copilot 和 Agent 支持局部重启"
  echo "  - 只有 Go 后端模块支持选择性编译"
  echo ""
  echo "示例:"
  echo "  $0                    # 同步文档 + 增量编译 + 重启"
  echo "  $0 -system -meta      # 重启并按需编译 system 和 meta"
  echo "  $0 -geopython-workflow         # 仅重启 GeoPython Workflow"
  echo "  $0 -all               # 重启并按需编译所有模块 (完整)"
  exit 1
}

echo "🔄 重启 ADDP 开发环境"
echo ""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

cd "${ROOT_DIR}"
source "${SCRIPT_DIR}/lifecycle-lock.sh"
source "${SCRIPT_DIR}/node-dependencies.sh"
addp_acquire_lifecycle_lock restart "$@"
source "${SCRIPT_DIR}/jupyter-env.sh"

# 加载 .env 配置
if [ -f ".env" ]; then
    set -a
    source .env
    set +a
fi
source "${ROOT_DIR}/scripts/infra/ports.sh"
if addp_infra_ready; then
    addp_infra_read_actual_ports
fi
source "${SCRIPT_DIR}/ports.sh"
if [ "${ADDP_ONLINE_HOST:-0}" != 1 ]; then
    addp_dev_load_saved_ports
fi
export MODEL3D_WORKFLOW_PORT="${MODEL3D_WORKFLOW_PORT:-8101}"
export POINTCLOUD_WORKFLOW_PORT="${POINTCLOUD_WORKFLOW_PORT:-8102}"
export DOCUMENT_WORKFLOW_PORT="${DOCUMENT_WORKFLOW_PORT:-8105}"
export SUPERMAP_WORKFLOW_PORT="${SUPERMAP_WORKFLOW_PORT:-8103}"

# 自动生成服务 URL（与 start.sh 保持一致）
generate_service_urls() {
    local services=(system manager meta transfer orchestrator develop service copilot monitor standard model quality security asset ontology catalog workbench portal agent inference)
    for svc in "${services[@]}"; do
        local port_var="$(echo ${svc} | tr '[:lower:]' '[:upper:]')_BACKEND_PORT"
        local url_var="$(echo ${svc} | tr '[:lower:]' '[:upper:]')_SERVICE_URL"
        local port_val="${!port_var}"
        if [ -n "$port_val" ]; then
            export ${url_var}="http://${SERVICE_HOST}:${port_val}"
        fi
    done
    [ -n "$MEILISEARCH_PORT" ] && export MEILISEARCH_URL="http://${SERVICE_HOST}:${MEILISEARCH_PORT}"
}

generate_service_urls

SWAGGER_MODULES=(system manager meta transfer orchestrator develop service monitor standard model quality security asset ontology catalog workbench portal graph inference)

is_swagger_module() {
  local module="$1"
  [[ " ${SWAGGER_MODULES[*]} " == *" $module "* ]]
}

run_swagger_generate() {
  local target="$*"
  local started=$SECONDS
  if bash "${SCRIPT_DIR}/../swagger/gen-swagger.sh" "$@"; then
    echo "⏱ Swagger 生成耗时: $((SECONDS - started)) 秒"
    return 0
  fi

  echo "❌ [$target] Swagger 文档生成失败，已中断重启"
  return 1
}

run_swagger_coverage_check() {
  bash "${SCRIPT_DIR}/../swagger/check-route-coverage.sh" "$@"
}

# 解析参数
RESTART_ALL=false
RESTART_MODULES=()
[ $# -eq 0 ] && RESTART_ALL=true

for arg in "$@"; do
  case $arg in
    -h|--help)
      show_usage
      ;;
    -all)
      RESTART_ALL=true
      ;;
    -system|-manager|-meta|-transfer|-orchestrator|-develop|-service|-monitor|-gateway|-standard|-model|-quality|-security|-asset|-ontology|-catalog|-workbench|-portal|-graph|-inference|-geopython-workflow|-math-workflow|-model3d-workflow|-pointcloud-workflow|-document-workflow|-supermap-workflow|-copilot|-agent|-spark-workflow|-jupyter|-duckdb)
      module="${arg#-}"  # 移除前导的 -
      [[ " ${RESTART_MODULES[*]} " == *" $module "* ]] || RESTART_MODULES+=("$module")
      ;;
    *)
      echo "❌ 未知参数: $arg"
      show_usage
      ;;
  esac
done

if [ "$RESTART_ALL" = true ] && [ ${#RESTART_MODULES[@]} -gt 0 ]; then
  echo "❌ -all 不能与模块参数同时使用" >&2
  exit 1
fi

is_python_service_module() {
    case "$1" in
        geopython-workflow|math-workflow|model3d-workflow|pointcloud-workflow|document-workflow|supermap-workflow|spark-workflow|jupyter|copilot|agent)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

only_python_service_params() {
    if [ "$RESTART_ALL" = true ] || [ ${#RESTART_MODULES[@]} -eq 0 ]; then
        return 1
    fi
    for module in "${RESTART_MODULES[@]}"; do
        if ! is_python_service_module "$module"; then
            return 1
        fi
    done
    return 0
}

only_duckdb_param() {
    [ "$RESTART_ALL" = false ] &&
      [ ${#RESTART_MODULES[@]} -eq 1 ] &&
      [ "${RESTART_MODULES[0]}" = "duckdb" ]
}

stop_pidfile_process() {
    local pidfile="$1"
    local label="$2"
    if [ ! -f "$pidfile" ]; then
        return 0
    fi
    local pid
    pid=$(cat "$pidfile" 2>/dev/null || true)
    if [ -n "$pid" ] && ps -p "$pid" > /dev/null 2>&1; then
        echo "  停止 $label (PID: $pid)"
        kill "$pid" 2>/dev/null || true
        for _ in 1 2 3 4 5 6 7 8 9 10; do
            if ! ps -p "$pid" > /dev/null 2>&1; then
                break
            fi
            sleep 0.2
        done
        if ps -p "$pid" > /dev/null 2>&1; then
            kill -9 "$pid" 2>/dev/null || true
        fi
    fi
    rm -f "$pidfile"
}

stop_matching_port_process() {
    local port="$1"
    local label="$2"
    local pattern="$3"
    if [ -z "$port" ]; then
        return 0
    fi
    local pids
    pids=$(lsof -ti ":$port" -sTCP:LISTEN 2>/dev/null || true)
    if [ -z "$pids" ]; then
        return 0
    fi
    for pid in $pids; do
        local proc_cmd
        proc_cmd=$(ps -p "$pid" -o command= 2>/dev/null || true)
        if echo "$proc_cmd" | grep -qE "$pattern"; then
            echo "  清理 $label 端口 $port 残留进程 (PID: $pid)"
            kill -9 "$pid" 2>/dev/null || true
        else
            echo "  ⚠️  端口 $port 被非 $label 进程占用 (PID: $pid)，跳过"
        fi
    done
}

require_service_python() {
    local service_dir="$1"
    local label="$2"
    if [ ! -x "$service_dir/venv/bin/python" ]; then
        echo "❌ $label 虚拟环境不存在或不可执行: $service_dir/venv/bin/python"
        echo "   请先运行: bash scripts/dev/start.sh -$3"
        return 1
    fi
    if ! "$service_dir/venv/bin/python" -c "import addp_common.workflow_runtime" >/dev/null 2>&1; then
        echo "❌ $label 虚拟环境缺少 common-python workflow runtime"
        echo "   请先运行: bash scripts/dev/start.sh -$3"
        return 1
    fi
}



wait_http_ready() {
    local label="$1"
    local url="$2"
    local max_wait="${3:-60}"
    local wait_count=0
    echo -n "  等待 $label 就绪"
    until curl -fsS "$url" > /dev/null 2>&1; do
        echo -n "."
        sleep 1
        wait_count=$((wait_count + 1))
        if [ $wait_count -ge $max_wait ]; then
            echo " ✗"
            echo "❌ $label 启动超时（${max_wait}秒）"
            return 1
        fi
    done
    echo " ✓"
}

start_background_process() {
    local service_dir="$1"
    local pidfile="$2"
    local stdout_log="$3"
    local stderr_log="$4"
    shift 4

    (
        cd "$service_dir"
        nohup "$@" > "${ROOT_DIR}/${stdout_log}" 2> "${ROOT_DIR}/${stderr_log}" < /dev/null &
        local pid=$!
        echo "$pid" > "${ROOT_DIR}/${pidfile}"
        disown "$pid" 2>/dev/null || true
    )
}

verify_pidfile_process_alive() {
    local pidfile="$1"
    local label="$2"
    local stdout_log="$3"
    local stderr_log="$4"
    local pid
    pid=$(cat "$pidfile" 2>/dev/null || true)
    if [ -z "$pid" ] || ! ps -p "$pid" > /dev/null 2>&1; then
        echo "❌ $label 启动后进程不存在"
        echo "   查看日志: ${stdout_log}"
        echo "   或检查错误: ${stderr_log}"
        return 1
    fi
}

restart_geopython_workflow_service() (
    source "${SCRIPT_DIR}/geopython-workflow.sh"
    addp_prepare_geopython_workflow || return 1
    stop_pidfile_process ".dev-pids/geopython-workflow-engine.pid" "GeoPython Workflow"
    addp_launch_geopython_workflow
)

restart_math_workflow_service() {
    local port="${MATH_WORKFLOW_PORT:-8089}"
    stop_pidfile_process ".dev-pids/math-workflow-engine.pid" "Math Workflow Engine"
    stop_matching_port_process "$port" "Math Workflow Engine" "python.*api_server\\.py|engines/math-workflow"
    require_service_python "engines/math-workflow" "Math Workflow Engine" "math-workflow"
    echo "  启动 Math Workflow Engine..."
    (
        cd engines/math-workflow
        export PORT="$port"
        start_background_process "." ".dev-pids/math-workflow-engine.pid" "logs/math-workflow-engine.log" "logs/math-workflow-engine-stderr.log" ./venv/bin/python api_server.py
    )
    wait_http_ready "Math Workflow Engine" "http://localhost:${port}/health"
    verify_pidfile_process_alive ".dev-pids/math-workflow-engine.pid" "Math Workflow Engine" "logs/math-workflow-engine.log" "logs/math-workflow-engine-stderr.log"
}

restart_model3d_workflow_service() {
    source "$ROOT_DIR/scripts/dev/model3d-workflow.sh"
    addp_prepare_model3d_workflow || return 1
    stop_pidfile_process ".dev-pids/model3d-workflow-engine.pid" "Model3D Workflow Engine"
    addp_launch_model3d_workflow
}

restart_pointcloud_workflow_service() {
    source "$ROOT_DIR/scripts/dev/pointcloud-workflow.sh"
    addp_prepare_pointcloud_workflow || return 1
    stop_pidfile_process ".dev-pids/pointcloud-workflow-engine.pid" "PointCloud Workflow Engine"
    addp_launch_pointcloud_workflow
}

restart_document_workflow_service() {
    source "$ROOT_DIR/scripts/dev/document-workflow.sh"
    addp_prepare_document_workflow || return 1
    stop_pidfile_process ".dev-pids/document-workflow-engine.pid" "Document Workflow Engine"
    addp_launch_document_workflow
}

restart_supermap_workflow_service() {
    bash "${SCRIPT_DIR}/supermap-workflow.sh"
}

restart_spark_workflow_service() {
    source "${SCRIPT_DIR}/spark-workflow.sh"
    addp_prepare_spark_workflow || return 1
    stop_pidfile_process ".dev-pids/spark-workflow-engine.pid" "Spark Workflow Engine"
    addp_launch_spark_workflow
}

restart_jupyter_service() {
    local api_port="${JUPYTER_API_PORT:-8097}"
    ensure_jupyter_python_env "$ROOT_DIR"
    stop_pidfile_process ".dev-pids/jupyter-api-server.pid" "Jupyter API Server"
    stop_matching_port_process "$api_port" "Jupyter API Server" "python.*api_server\\.py|engines/jupyter"
    echo "  启动 Jupyter Notebook Runtime..."
    (
        cd engines/jupyter
        export API_PORT="$api_port"
        start_background_process "." ".dev-pids/jupyter-api-server.pid" "logs/jupyter-api-server.log" "logs/jupyter-api-server-stderr.log" ./venv/bin/python api_server.py
    )
    wait_http_ready "Jupyter API Server" "http://localhost:${api_port}/health"
    verify_pidfile_process_alive ".dev-pids/jupyter-api-server.pid" "Jupyter API Server" "logs/jupyter-api-server.log" "logs/jupyter-api-server-stderr.log"
}

restart_copilot_service() {
    local port="${COPILOT_BACKEND_PORT:-8087}"
    stop_pidfile_process ".dev-pids/copilot-backend.pid" "Copilot Backend"
    stop_matching_port_process "$port" "Copilot Backend" "python.*main\\.py|copilot/backend"
    require_service_python "copilot/backend" "Copilot Backend" "copilot"
    echo "  启动 Copilot Backend..."
    (
        cd copilot/backend
        export PORT="$port"
        export DATABASE_URL="postgresql://addp:addp_password@localhost:${POSTGRES_PORT:-15432}/addp"
        start_background_process "." ".dev-pids/copilot-backend.pid" "logs/copilot-backend.log" "logs/copilot-backend-stderr.log" ./venv/bin/python main.py
    )
    wait_http_ready "Copilot Backend" "http://localhost:${port}/health/ready"
    verify_pidfile_process_alive ".dev-pids/copilot-backend.pid" "Copilot Backend" "logs/copilot-backend.log" "logs/copilot-backend-stderr.log"
}

restart_agent_service() {
    local port="${AGENT_BACKEND_PORT:-8190}"
    stop_pidfile_process ".dev-pids/agent-backend.pid" "Agent Backend"
    stop_matching_port_process "$port" "Agent Backend" "python.*main\\.py|agent/backend"
    require_service_python "agent/backend" "Agent Backend" "agent"
    echo "  启动 Agent Backend..."
    (
        cd agent/backend
        export PORT="$port"
        start_background_process "." ".dev-pids/agent-backend.pid" "logs/agent-backend.log" "logs/agent-backend-stderr.log" ./venv/bin/python "${ROOT_DIR}/agent/backend/main.py"
    )
    wait_http_ready "Agent Backend" "http://localhost:${port}/health/ready"
    verify_pidfile_process_alive ".dev-pids/agent-backend.pid" "Agent Backend" "logs/agent-backend.log" "logs/agent-backend-stderr.log"
}

restart_scoped_python_services() {
    echo "🐍 局部重启 Python/扩展服务: ${RESTART_MODULES[*]}"
    mkdir -p logs .dev-pids
    for module in "${RESTART_MODULES[@]}"; do
        case "$module" in
            geopython-workflow)
                restart_geopython_workflow_service
                ;;
            math-workflow)
                restart_math_workflow_service
                ;;
            model3d-workflow)
                restart_model3d_workflow_service
                ;;
            pointcloud-workflow)
                restart_pointcloud_workflow_service
                ;;
            document-workflow)
                restart_document_workflow_service
                ;;
            supermap-workflow)
                restart_supermap_workflow_service
                ;;
            spark-workflow)
                restart_spark_workflow_service
                ;;
            jupyter)
                restart_jupyter_service
                ;;
            copilot)
                restart_copilot_service
                ;;
            agent)
                restart_agent_service
                ;;
        esac
    done
    echo "✅ Python/扩展服务局部重启完成"
}

if only_python_service_params; then
    if ! addp_infra_ready; then
        echo "❌ ADDP Infra 未就绪，无法安全地局部重启 Python 服务" >&2
        exit 1
    fi
    addp_infra_read_actual_ports
    restart_scoped_python_services
    exit 0
fi

if only_duckdb_param; then
    echo "局部重启 DuckDB Federated Query Runtime"
    stop_pidfile_process ".dev-pids/duckdb.pid" "DuckDB Runtime"
    stop_matching_port_process "${DUCKDB_RUNTIME_PORT:-8104}" "DuckDB Runtime" "addp-duckdb"
    exec env SKIP_MODTIDY=1 "${SCRIPT_DIR}/start.sh" -duckdb
fi

echo "📦 构建计划: 同步 Swagger，按完整构建指纹复用或编译产物"
echo ""

# Validate native Spark prerequisites before stopping the workspace.
if [ "$RESTART_ALL" = true ]; then
    source "${SCRIPT_DIR}/spark-workflow.sh"
    (addp_prepare_spark_workflow) || exit 1
    source "${SCRIPT_DIR}/geopython-workflow.sh"
    (addp_prepare_geopython_workflow) || exit 1
    source "${SCRIPT_DIR}/pointcloud-workflow.sh"
    (addp_prepare_pointcloud_workflow) || exit 1
    source "${SCRIPT_DIR}/document-workflow.sh"
    (addp_prepare_document_workflow) || exit 1
    source "${SCRIPT_DIR}/model3d-workflow.sh"
    (addp_prepare_model3d_workflow) || exit 1
fi

# 全量停止保留原有注销顺序；局部停止在 Swagger 预检成功后执行。
STOP_ARGS=()
if [ "$RESTART_ALL" = true ]; then
  "${SCRIPT_DIR}/stop.sh" || exit 1
else
  for module in "${RESTART_MODULES[@]}"; do
    STOP_ARGS+=("-$module")
  done
fi

# 2. Swagger 是 Go 构建输入，先生成，再由 start.sh 统一校验产物指纹。
if [ "$RESTART_ALL" = true ]; then
  echo "📄 重新生成所有模块 Swagger 文档..."
  run_swagger_generate all
  echo "🔎 校验所有模块 Swagger 路由覆盖..."
  run_swagger_coverage_check all
elif [ ${#RESTART_MODULES[@]} -gt 0 ]; then
  SWAGGER_TARGETS=()
  for module in "${RESTART_MODULES[@]}"; do
    if is_swagger_module "$module" && [[ " ${SWAGGER_TARGETS[*]} " != *" $module "* ]]; then
      SWAGGER_TARGETS+=("$module")
    fi
  done
  if [ ${#SWAGGER_TARGETS[@]} -gt 0 ]; then
    run_swagger_generate "${SWAGGER_TARGETS[@]}"
    run_swagger_coverage_check "${SWAGGER_TARGETS[@]}"
  fi
fi
if [ "$RESTART_ALL" = false ]; then
  if ! "${SCRIPT_DIR}/stop.sh" "${STOP_ARGS[@]}"; then
    echo "❌ 停止所选模块失败，已中断重启" >&2
    exit 1
  fi
fi
echo "✅ 保留已有产物，启动时校验构建指纹并按需编译"
echo ""

# 4. 启动服务
# restart 时跳过 go mod tidy（模块依赖在重启间不会改变，避免网络调用拖慢速度）
START_ARGS=()
if [ "$RESTART_ALL" = false ]; then
  START_ARGS=("${STOP_ARGS[@]}")
fi
exec env SKIP_MODTIDY=1 "${SCRIPT_DIR}/start.sh" "${START_ARGS[@]}"
