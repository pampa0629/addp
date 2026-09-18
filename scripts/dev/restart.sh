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
  echo "  -ontology    重启并按需编译 Ontology Backend"
  echo "  -workbench   重启并按需编译 Workbench 模块"
  echo "  -portal      重启并按需编译 Portal 模块"
  echo "  -graph       重启并按需编译 Graph 模块"
  echo "  -inference   重启并按需编译 Inference 模块"
  echo "  -geopython-workflow   重启 GeoPython Workflow (Python 服务)"
  echo "  -math-workflow     重启 Math Workflow Engine (Python 服务)"
  echo "  -model3d-workflow  重启 Model3D Workflow Engine (Python 服务)"
  echo "  -pointcloud-workflow 重启 PointCloud Workflow Engine (Docker runtime)"
  echo "  -document-workflow 重启 Document Workflow Engine (Docker runtime)"
  echo "  -supermap-workflow 重启 SuperMap Workflow Engine (C++ Docker runtime，需先构建基础镜像)"
  echo "  -copilot     重启 Copilot Backend (Python 服务)"
  echo "  -agent       重启 Agent Backend (Python 服务)"
  echo "  -spark-workflow 重启 Spark 工作流 Engine (Python 服务)"
  echo "  -jupyter     重启 Jupyter Engine (Python 服务)"
  echo "  -duckdb      按需编译并重启 DuckDB Federated Query Runtime"
  echo ""
  echo "增量构建说明:"
  echo "  - 所有 Go 服务统一校验源码、共享依赖、工具链和编译参数"
  echo "  - 构建输入未变化时复用产物，变化或产物缺失时重新编译"
  echo "  - 无参数与 -all 均重新生成全部 Swagger，再校验构建指纹"
  echo "  - 只指定 Python/扩展服务参数时,仅重启对应服务,不停止整套环境"
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
ORIGINAL_ARGS=("$@")

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

  if [ "${ALLOW_SWAGGER_FAILURE:-0}" = "1" ]; then
    echo "⚠️ [$target] Swagger 文档生成失败，ALLOW_SWAGGER_FAILURE=1，本次继续"
    return 0
  fi

  echo "❌ [$target] Swagger 文档生成失败，已中断重启"
  echo "   如需临时容忍历史欠账，可使用：ALLOW_SWAGGER_FAILURE=1 $0 ${ORIGINAL_ARGS[*]}"
  return 1
}

run_swagger_coverage_check() {
  SWAGGER_COVERAGE_WARN_ONLY=1 bash "${SCRIPT_DIR}/../swagger/check-route-coverage.sh" "$@"
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
      RESTART_MODULES+=("$module")
      ;;
    *)
      echo "❌ 未知参数: $arg"
      show_usage
      ;;
  esac
done

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

ensure_model3d_node_dependencies() {
    local dir="engines/model3d-workflow"
    if [ ! -f "$dir/package.json" ]; then
        return 0
    fi
    if [ -d "$dir/node_modules/@mkkellogg/gaussian-splats-3d" ]; then
        echo "  Model3D Workflow Node 依赖已存在，跳过安装"
        return 0
    fi
    if ! command -v npm >/dev/null 2>&1; then
        echo "❌ Model3D Workflow 需要 npm 安装高斯泼溅 KSplat 转换依赖"
        return 1
    fi
    echo "  安装 Model3D Workflow Node 依赖..."
    addp_install_node_dependencies "$dir" --omit=dev
    echo "  ✓ Model3D Workflow Node 依赖安装完成"
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

geopython_workflow_source_fingerprint() {
    {
        printf '%s\n' "geopython-workflow-image-v1"
        while IFS= read -r file; do
            printf '%s %s\n' "$file" "$(git hash-object "$file")"
        done < <(
            {
                printf '%s\n' \
                    engines/geopython-workflow/Dockerfile \
                    engines/geopython-workflow/requirements.txt \
                    engines/geopython-workflow/api_server.py \
                    engines/geopython-workflow/container_entrypoint.sh \
                    engines/geopython-workflow/workflow_engine.py \
                    engines/geopython-workflow/geometry_batches.py \
                    common-python/README.md \
                    common-python/pyproject.toml
                find engines/geopython-workflow/operators common-python/addp_common \
                    -type f ! -path '*/__pycache__/*' ! -name '*.pyc'
            } | LC_ALL=C sort
        )
    } | git hash-object --stdin
}

ensure_geopython_workflow_image() {
    local image="$1"
    local fingerprint
    local current_fingerprint
    fingerprint="$(geopython_workflow_source_fingerprint)"
    current_fingerprint="$(docker image inspect \
        -f '{{ index .Config.Labels "addp.geopython.source-fingerprint" }}' \
        "$image" 2>/dev/null || true)"

    if [ "$current_fingerprint" = "$fingerprint" ]; then
        echo "  GeoPython Workflow 镜像构建输入未变化，复用现有镜像: $image"
        return 0
    fi

    echo "  构建 GeoPython Workflow 镜像（构建输入已变化或镜像不存在）..."
    docker build \
        --label "addp.geopython.source-fingerprint=${fingerprint}" \
        -f engines/geopython-workflow/Dockerfile \
        -t "$image" \
        .
}

restart_geopython_workflow_service() {
    local port="${GEOPYTHON_WORKFLOW_PORT:-8099}"
    local image="addp-geopython-workflow-engine:dev"
    local source_dir="${ROOT_DIR}/business/nfs/data"
    local container_source_dir="${ROOT_DIR}/business/nfs/data"
    local system_port="${SYSTEM_BACKEND_PORT:-8180}"

    if ! command -v docker >/dev/null 2>&1; then
        echo "❌ GeoPython Workflow 需要 Docker runtime 承载 GDAL/OGR"
        return 1
    fi

    docker rm -f geopython-workflow-engine >/dev/null 2>&1 || true
    rm -f .dev-pids/geopython-workflow-engine.pid
    stop_matching_port_process "$port" "GeoPython Workflow" "python.*api_server\\.py|engines/geopython-workflow"
    ensure_geopython_workflow_image "$image"

    echo "  启动 GeoPython Workflow Docker runtime..."
    mkdir -p "${source_dir}" logs .dev-pids
    docker run -d \
        --name geopython-workflow-engine \
        --label com.docker.compose.project=addp-app \
        --label com.docker.compose.service=geopython-workflow-engine \
        --label com.docker.compose.project.working_dir="${ROOT_DIR}" \
        --add-host=host.docker.internal:host-gateway \
        -p "${port}:8099" \
        -e PORT=8099 \
        -e SYSTEM_URL="http://host.docker.internal:${system_port}" \
        -e GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET="${GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET:-}" \
        -e GEOPYTHON_WORKFLOW_LOOPBACK_HOST=host.docker.internal \
        -e POSTGRES_HOST=host.docker.internal \
        -e POSTGRES_PORT="${POSTGRES_PORT:-15432}" \
        -e POSTGRES_USER="${POSTGRES_USER:-addp}" \
        -e POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-}" \
        -e POSTGRES_DB="${POSTGRES_DB:-addp}" \
        -e DB_SCHEMA=develop \
        -v "${ROOT_DIR}/logs:/app/logs" \
        -v "${source_dir}:${container_source_dir}" \
        "$image" > .dev-pids/geopython-workflow-engine.pid
    wait_http_ready "GeoPython Workflow" "http://localhost:${port}/health"
    if ! docker ps --filter "name=^/geopython-workflow-engine$" --format '{{.Names}}' | grep -qx "geopython-workflow-engine"; then
        echo "❌ GeoPython Workflow 容器启动后不存在"
        echo "   查看日志: docker logs geopython-workflow-engine"
        return 1
    fi
}

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
    local port="${MODEL3D_WORKFLOW_PORT:-8101}"
    stop_pidfile_process ".dev-pids/model3d-workflow-engine.pid" "Model3D Workflow Engine"
    stop_matching_port_process "$port" "Model3D Workflow Engine" "python.*api_server\\.py|engines/model3d-workflow"
    require_service_python "engines/model3d-workflow" "Model3D Workflow Engine" "model3d-workflow"
    ensure_model3d_node_dependencies
    echo "  启动 Model3D Workflow Engine..."
    (
        cd engines/model3d-workflow
        export PORT="$port"
        export MODEL3D_WORKFLOW_SERVICE_CLIENT_SECRET="${MODEL3D_WORKFLOW_SERVICE_CLIENT_SECRET:-}"
        start_background_process "." ".dev-pids/model3d-workflow-engine.pid" "logs/model3d-workflow-engine.log" "logs/model3d-workflow-engine-stderr.log" ./venv/bin/python api_server.py
    )
    wait_http_ready "Model3D Workflow Engine" "http://localhost:${port}/health"
    verify_pidfile_process_alive ".dev-pids/model3d-workflow-engine.pid" "Model3D Workflow Engine" "logs/model3d-workflow-engine.log" "logs/model3d-workflow-engine-stderr.log"
}

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
                    common-python/addp_common/module_lifecycle.py \
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
        echo "  PointCloud Workflow Engine 镜像构建输入未变化，复用现有镜像: $image"
        return 0
    fi

    echo "  构建 PointCloud Workflow Engine 镜像（构建输入已变化或镜像不存在）..."
    docker build \
        --label "addp.pointcloud.source-fingerprint=${fingerprint}" \
        -f engines/pointcloud-workflow/Dockerfile \
        -t "$image" \
        .
}

restart_pointcloud_workflow_service() {
    local port="${POINTCLOUD_WORKFLOW_PORT:-8102}"
    local image="${POINTCLOUD_WORKFLOW_IMAGE:-addp-pointcloud-workflow-engine:dev}"
    local source_dir="${POINTCLOUD_DATA_HOST_PATH:-${ROOT_DIR}/business/nfs/data}"
    local container_source_dir="${POINTCLOUD_DATA_CONTAINER_PATH:-${ROOT_DIR}/business/nfs/data}"
    local work_dir="${POINTCLOUD_WORK_HOST_PATH:-${ROOT_DIR}/data/pointcloud-work}"
    local system_port="${SYSTEM_BACKEND_PORT:-8180}"

    if ! command -v docker >/dev/null 2>&1; then
        echo "❌ PointCloud Workflow Engine 需要 Docker runtime 承载 PDAL"
        return 1
    fi

    docker rm -f pointcloud-workflow-engine >/dev/null 2>&1 || true
    stop_matching_port_process "$port" "PointCloud Workflow Engine" "python.*api_server\\.py|engines/pointcloud-workflow"

    ensure_pointcloud_workflow_image "$image"

    echo "  启动 PointCloud Workflow Engine Docker runtime..."
    mkdir -p "${work_dir}"
    mkdir -p .dev-pids
    docker run -d \
        --name pointcloud-workflow-engine \
        --label com.docker.compose.project=addp-app \
        --label com.docker.compose.service=pointcloud-workflow-engine \
        --label com.docker.compose.project.working_dir="${ROOT_DIR}" \
        --add-host=host.docker.internal:host-gateway \
        -p "${port}:8102" \
        -e PORT=8102 \
        -e SYSTEM_URL="http://host.docker.internal:${system_port}" \
        -e POINTCLOUD_WORKFLOW_SERVICE_CLIENT_SECRET="${POINTCLOUD_WORKFLOW_SERVICE_CLIENT_SECRET:-}" \
        -e POINTCLOUD_PDAL_BIN=/opt/conda/bin/pdal \
        -e POINTCLOUD_WORK_DIR=/work/pointcloud \
        -e CPL_TMPDIR=/work/pointcloud \
        -e RUNTIME_HOST=localhost \
        -e POINTCLOUD_OBJECT_STORE_LOOPBACK_HOST="${POINTCLOUD_OBJECT_STORE_LOOPBACK_HOST:-host.docker.internal}" \
        -v "${ROOT_DIR}/logs:/app/logs" \
        -v "${work_dir}:/work/pointcloud" \
        -v "${ROOT_DIR}/engines/pointcloud-workflow/api_server.py:/app/api_server.py:ro" \
        -v "${ROOT_DIR}/engines/pointcloud-workflow/operators.py:/app/operators.py:ro" \
        -v "${source_dir}:${container_source_dir}:ro" \
        "$image" > .dev-pids/pointcloud-workflow-engine.pid
    local wait_count=0
    echo -n "  等待 PointCloud Workflow Engine 就绪"
    while ! curl -s "http://localhost:${port}/health" | grep -q '"status":"healthy"'; do
        if ! docker ps --filter "name=^/pointcloud-workflow-engine$" --format '{{.Names}}' | grep -qx "pointcloud-workflow-engine"; then
            echo " ✗"
            echo "❌ PointCloud Workflow Engine 容器已退出"
            docker logs --tail 100 pointcloud-workflow-engine 2>&1 || true
            return 1
        fi
        sleep 1
        echo -n "."
        wait_count=$((wait_count + 1))
        if [ "$wait_count" -ge 60 ]; then
            echo " ✗"
            echo "❌ PointCloud Workflow Engine 启动超时（60秒）"
            echo "   查看日志: docker logs pointcloud-workflow-engine"
            return 1
        fi
    done
    echo " ✓"
    if ! docker ps --filter "name=^/pointcloud-workflow-engine$" --format '{{.Names}}' | grep -qx "pointcloud-workflow-engine"; then
        echo "❌ PointCloud Workflow Engine 容器启动后不存在"
        echo "   查看日志: docker logs pointcloud-workflow-engine"
        return 1
    fi
    echo "  ✓ PointCloud Workflow Engine Docker runtime 已启动"
}

document_workflow_source_fingerprint() {
    {
        printf '%s\n' "document-workflow-image-v1"
        while IFS= read -r file; do
            printf '%s %s\n' "$file" "$(git hash-object "$file")"
        done < <(
            {
                printf '%s\n' \
                    engines/document-workflow/Dockerfile \
                    engines/document-workflow/requirements.txt \
                    engines/document-workflow/api_server.py \
                    engines/document-workflow/operators.py \
                    common-python/pyproject.toml \
                    common-python/README.md \
                    common-python/addp_common/__init__.py \
                    common-python/addp_common/module_lifecycle.py \
                    common-python/addp_common/workflow_access.py
                find common-python/addp_common/client common-python/addp_common/workflow_runtime \
                    -type f ! -path '*/__pycache__/*' ! -name '*.pyc'
            } | LC_ALL=C sort
        )
    } | git hash-object --stdin
}

ensure_document_workflow_image() {
    local image="$1"
    local fingerprint
    local current_fingerprint
    fingerprint="$(document_workflow_source_fingerprint)"
    current_fingerprint="$(docker image inspect -f '{{ index .Config.Labels "addp.document.source-fingerprint" }}' "$image" 2>/dev/null || true)"
    if [ "$current_fingerprint" = "$fingerprint" ]; then
        echo "  Document Workflow Engine 镜像构建输入未变化，复用现有镜像: $image"
        return 0
    fi
    docker build --label "addp.document.source-fingerprint=${fingerprint}" -f engines/document-workflow/Dockerfile -t "$image" .
}

restart_document_workflow_service() {
    local port="${DOCUMENT_WORKFLOW_PORT:-8105}"
    local image="${DOCUMENT_WORKFLOW_IMAGE:-addp-document-workflow-engine:dev}"
    local source_dir="${DOCUMENT_DATA_HOST_PATH:-${ROOT_DIR}/business/nfs/data}"
    local container_source_dir="${DOCUMENT_DATA_CONTAINER_PATH:-${ROOT_DIR}/business/nfs/data}"
    local work_dir="${DOCUMENT_WORK_HOST_PATH:-${ROOT_DIR}/data/document-work}"
    local system_port="${SYSTEM_BACKEND_PORT:-8180}"
    command -v docker >/dev/null 2>&1 || { echo "❌ Document Workflow Engine 需要 Docker runtime 承载 LibreOffice"; return 1; }
    docker rm -f document-workflow-engine >/dev/null 2>&1 || true
    stop_matching_port_process "$port" "Document Workflow Engine" "python.*api_server\\.py|engines/document-workflow"
    ensure_document_workflow_image "$image"
    mkdir -p "$work_dir" .dev-pids
    docker run -d \
        --name document-workflow-engine \
        --label com.docker.compose.project=addp-app \
        --label com.docker.compose.service=document-workflow-engine \
        --label com.docker.compose.project.working_dir="${ROOT_DIR}" \
        --read-only \
        --tmpfs /tmp:rw,nosuid,nodev,size=67108864 \
        --cap-drop=ALL \
        --security-opt=no-new-privileges \
        --add-host=host.docker.internal:host-gateway \
        -p "${port}:8105" \
        -e PORT=8105 \
        -e SYSTEM_URL="http://host.docker.internal:${system_port}" \
        -e DOCUMENT_WORKFLOW_SERVICE_CLIENT_SECRET="${DOCUMENT_WORKFLOW_SERVICE_CLIENT_SECRET:-}" \
        -e DOCUMENT_LIBREOFFICE_BIN=/usr/bin/soffice \
        -e DOCUMENT_WORK_DIR=/work/document \
        -e DOCUMENT_CONVERSION_CONCURRENCY="${DOCUMENT_CONVERSION_CONCURRENCY:-1}" \
        -e DOCUMENT_CONVERSION_TIMEOUT_SECONDS="${DOCUMENT_CONVERSION_TIMEOUT_SECONDS:-600}" \
        -e DOCUMENT_OBJECT_STORE_LOOPBACK_HOST="${DOCUMENT_OBJECT_STORE_LOOPBACK_HOST:-host.docker.internal}" \
        -e RUNTIME_HOST=localhost \
        -v "${ROOT_DIR}/logs:/app/logs" \
        -v "${work_dir}:/work/document" \
        -v "${ROOT_DIR}/engines/document-workflow/api_server.py:/app/api_server.py:ro" \
        -v "${ROOT_DIR}/engines/document-workflow/operators.py:/app/operators.py:ro" \
        -v "${source_dir}:${container_source_dir}:ro" \
        "$image" > .dev-pids/document-workflow-engine.pid
    local wait_count=0
    while ! curl -s "http://localhost:${port}/health" | grep -q '"status":"healthy"'; do
        if ! docker ps --filter "name=^/document-workflow-engine$" --format '{{.Names}}' | grep -qx document-workflow-engine; then
            docker logs --tail 100 document-workflow-engine 2>&1 || true
            return 1
        fi
        sleep 1
        wait_count=$((wait_count + 1))
        [ "$wait_count" -lt 90 ] || { docker logs --tail 100 document-workflow-engine 2>&1 || true; return 1; }
    done
    echo "  ✓ Document Workflow Engine Docker runtime 已启动"
}

restart_supermap_workflow_service() {
    bash "${SCRIPT_DIR}/supermap-workflow.sh"
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
            echo "  Spark Workflow 使用 JDK 11: ${JAVA_HOME}"
            return 0
        fi
    done

    echo "❌ Spark Workflow 需要 JDK 11，当前未找到可用安装"
    echo "   macOS 请运行: brew install openjdk@11"
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

restart_spark_workflow_service() {
    local port="${SPARK_WORKFLOW_PORT:-8098}"
    stop_pidfile_process ".dev-pids/spark-workflow-engine.pid" "Spark Workflow Engine"
    stop_matching_port_process "$port" "Spark Workflow Engine" "python.*api_server\\.py|engines/spark-workflow"
    require_service_python "engines/spark-workflow" "Spark Workflow Engine" "spark-workflow"
    configure_spark_workflow_java
    echo "  启动 Spark Workflow Engine..."
    (
        cd engines/spark-workflow
        export PORT="$port"
        export SPARK_WORKFLOW_SERVICE_CLIENT_SECRET="${SPARK_WORKFLOW_SERVICE_CLIENT_SECRET:-}"
        export SPARK_WORKFLOW_SHARED_HOST="${SPARK_WORKFLOW_SHARED_HOST:-$(detect_spark_workflow_shared_host)}"
        start_background_process "." ".dev-pids/spark-workflow-engine.pid" "logs/spark-workflow-engine.log" "logs/spark-workflow-engine-stderr.log" ./venv/bin/python api_server.py
    )
    wait_http_ready "Spark Workflow Engine" "http://localhost:${port}/health"
    verify_pidfile_process_alive ".dev-pids/spark-workflow-engine.pid" "Spark Workflow Engine" "logs/spark-workflow-engine.log" "logs/spark-workflow-engine-stderr.log"
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

# 1. 先强制杀死 Python 服务（避免端口残留导致 stop.sh 误判为非 ADDP 进程）
echo "🐍 强制终止 Python 服务..."
pkill -9 -f "engines/geopython-workflow/api_server.py" 2>/dev/null || true
pkill -9 -f "engines/math-workflow/api_server.py" 2>/dev/null || true
pkill -9 -f "engines/model3d-workflow/api_server.py" 2>/dev/null || true
pkill -9 -f "engines/pointcloud-workflow/api_server.py" 2>/dev/null || true
pkill -9 -f "engines/document-workflow/api_server.py" 2>/dev/null || true
pkill -9 -f "engines/spark-workflow/api_server.py" 2>/dev/null || true
pkill -9 -f "engines/jupyter/api_server.py" 2>/dev/null || true
pkill -9 -f "jupyter.*lab" 2>/dev/null || true
pkill -9 -f "copilot/backend/main.py" 2>/dev/null || true
pkill -9 -f "agent/backend/main.py" 2>/dev/null || true
pkill -9 -f "uvicorn" 2>/dev/null || true
echo ""

# 2. 停止服务
if ! "${SCRIPT_DIR}/stop.sh"; then
  echo ""
  echo "❌ 停止现有服务失败，已中断重启"
  exit 1
fi
echo ""
echo "✅ 已停止现有服务"

# 3. Swagger 是 Go 构建输入，先生成，再由 start.sh 统一校验产物指纹。
if [ "$RESTART_ALL" = true ]; then
  echo "📄 重新生成所有模块 Swagger 文档..."
  run_swagger_generate all
  echo "🔎 校验所有模块 Swagger 路由覆盖..."
  run_swagger_coverage_check all || true
elif [ ${#RESTART_MODULES[@]} -gt 0 ]; then
  SWAGGER_TARGETS=()
  for module in "${RESTART_MODULES[@]}"; do
    if is_swagger_module "$module" && [[ " ${SWAGGER_TARGETS[*]} " != *" $module "* ]]; then
      SWAGGER_TARGETS+=("$module")
    fi
  done
  if [ ${#SWAGGER_TARGETS[@]} -gt 0 ]; then
    run_swagger_generate "${SWAGGER_TARGETS[@]}"
    run_swagger_coverage_check "${SWAGGER_TARGETS[@]}" || true
  fi
fi
echo "✅ 保留已有产物，启动时校验构建指纹并按需编译"
echo ""

# 4. 启动服务
# restart 时跳过 go mod tidy（模块依赖在重启间不会改变，避免网络调用拖慢速度）
START_ARGS=()
if [ "$RESTART_ALL" = false ] && [ ${#ORIGINAL_ARGS[@]} -eq 1 ]; then
  START_ARGS=("${ORIGINAL_ARGS[0]}")
fi
exec env SKIP_MODTIDY=1 "${SCRIPT_DIR}/start.sh" "${START_ARGS[@]}"
