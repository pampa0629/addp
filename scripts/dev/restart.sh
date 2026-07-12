#!/bin/bash
set -e

# 使用说明
show_usage() {
  echo "用法: $0 [-all] [-system] [-manager] [-meta] [-transfer] [-orchestrator] [-develop] [-service] [-monitor] [-gateway] [-model] [-quality] [-asset] [-portal] [-python-workflow] [-math-workflow] [-model3d-workflow] [-pointcloud-workflow] [-supermap-workflow] [-copilot] [-agent] [-spark-workflow] [-jupyter]"
  echo ""
  echo "选项:"
  echo "  无参数        只重启服务,自动检测 common 模块变化并增量编译受影响的模块"
  echo "  -all         强制重新编译所有 Go 模块 + 重启 Python/容器运行时服务"
  echo "  -system      强制重新编译 System 模块"
  echo "  -manager     强制重新编译 Manager 模块"
  echo "  -meta        强制重新编译 Meta 模块"
  echo "  -transfer    强制重新编译 Transfer 模块"
  echo "  -orchestrator 强制重新编译 Orchestrator 模块"
  echo "  -develop     强制重新编译 Develop 模块"
  echo "  -service     强制重新编译 Service 模块"
  echo "  -monitor     强制重新编译 Monitor 模块"
  echo "  -gateway     强制重新编译 Gateway 模块"
  echo "  -standard    强制重新编译 Standard 模块"
  echo "  -model       强制重新编译 Model 模块"
  echo "  -quality     强制重新编译 Quality 模块
  -asset       强制重新编译 Asset 模块
  -portal      强制重新编译 Portal 模块
  -graph       强制重新编译 Graph 模块"
  echo "  -python-workflow   重启 GeoPython Workflow Engine (Python 服务)"
  echo "  -math-workflow     重启 Math Workflow Engine (Python 服务)"
  echo "  -model3d-workflow  重启 Model3D Workflow Engine (Python 服务)"
  echo "  -pointcloud-workflow 重启 PointCloud Workflow Engine (Docker runtime)"
  echo "  -supermap-workflow 重启 SuperMap Workflow Engine (Docker runtime，需配置 SDK 挂载路径)"
  echo "  -copilot     重启 Copilot Backend (Python 服务)"
  echo "  -agent       重启 Agent Backend (Python 服务)"
  echo "  -spark-workflow 重启 Spark 工作流 Engine (Python 服务)"
  echo "  -jupyter     重启 Jupyter Engine (Python 服务)"
  echo ""
  echo "智能检测说明:"
  echo "  - 无参数时会自动检测 common 模块是否有变化"
  echo "  - 如果检测到 common 变化,会自动重新编译所有依赖的 Go 模块"
  echo "  - 指定 Go 模块参数时,不执行智能检测,直接按参数编译"
  echo "  - 只指定 Python/扩展服务参数时,仅重启对应服务,不停止整套环境"
  echo ""
  echo "注意:"
  echo "  - GeoPython Workflow Engine、Math Workflow Engine、Spark 工作流 Engine、PointCloud Workflow Engine、SuperMap Workflow Engine、Jupyter Engine、Copilot 和 Agent 支持局部重启"
  echo "  - 只有 Go 后端模块支持选择性编译"
  echo ""
  echo "示例:"
  echo "  $0                    # 智能检测 + 重启 (推荐)"
  echo "  $0 -system -meta      # 重启并重新编译 system 和 meta"
  echo "  $0 -python-workflow         # 仅重启 GeoPython Workflow Engine"
  echo "  $0 -all               # 重启并重新编译所有模块 (完整)"
  exit 1
}

echo "🔄 重启 ADDP 开发环境"
echo ""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ORIGINAL_ARGS=("$@")

cd "${ROOT_DIR}"

# 加载 .env 配置
if [ -f ".env" ]; then
    set -a
    source .env
    set +a
fi
if [ -f ".env.local" ]; then
    set -a
    source .env.local
    set +a
fi

export MODEL3D_WORKFLOW_PORT="${MODEL3D_WORKFLOW_PORT:-8101}"
export POINTCLOUD_WORKFLOW_PORT="${POINTCLOUD_WORKFLOW_PORT:-8102}"
export SUPERMAP_WORKFLOW_PORT="${SUPERMAP_WORKFLOW_PORT:-8103}"

# 自动生成服务 URL（与 start.sh 保持一致）
generate_service_urls() {
    local services=(system manager meta transfer orchestrator develop service copilot monitor standard model quality asset portal agent)
    for svc in "${services[@]}"; do
        local port_var="$(echo ${svc} | tr '[:lower:]' '[:upper:]')_BACKEND_PORT"
        local url_var="$(echo ${svc} | tr '[:lower:]' '[:upper:]')_SERVICE_URL"
        local port_val="${!port_var}"
        if [ -n "$port_val" ]; then
            export ${url_var}="http://${SERVICE_HOST}:${port_val}"
        fi
    done
    [ -n "$MEILISEARCH_PORT" ] && export MEILISEARCH_URL="http://${SERVICE_HOST}:${MEILISEARCH_PORT}"
    [ -n "$MODEL3D_WORKFLOW_PORT" ] && export MODEL3D_WORKFLOW_URL="http://${SERVICE_HOST}:${MODEL3D_WORKFLOW_PORT}"
    [ -n "$POINTCLOUD_WORKFLOW_PORT" ] && export POINTCLOUD_WORKFLOW_URL="http://${SERVICE_HOST}:${POINTCLOUD_WORKFLOW_PORT}"
    [ -n "$SUPERMAP_WORKFLOW_PORT" ] && export SUPERMAP_WORKFLOW_URL="http://${SERVICE_HOST}:${SUPERMAP_WORKFLOW_PORT}"
}

generate_service_urls

SWAGGER_MODULES=(system manager meta transfer orchestrator develop service monitor standard model quality portal graph)

is_swagger_module() {
  local module="$1"
  [[ " ${SWAGGER_MODULES[*]} " == *" $module "* ]]
}

run_swagger_generate() {
  local target="$1"
  if bash "${SCRIPT_DIR}/../swagger/gen-swagger.sh" "$target"; then
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
  local target="$1"
  SWAGGER_COVERAGE_WARN_ONLY=1 bash "${SCRIPT_DIR}/../swagger/check-route-coverage.sh" "$target"
}

# 解析参数
FORCE_BUILD_ALL=false
FORCE_BUILD_MODULES=()

for arg in "$@"; do
  case $arg in
    -h|--help)
      show_usage
      ;;
    -all)
      FORCE_BUILD_ALL=true
      ;;
    -system|-manager|-meta|-transfer|-orchestrator|-develop|-service|-monitor|-gateway|-standard|-model|-quality|-asset|-portal|-graph|-python-workflow|-math-workflow|-model3d-workflow|-pointcloud-workflow|-supermap-workflow|-copilot|-agent|-spark-workflow|-jupyter)
      module="${arg#-}"  # 移除前导的 -
      FORCE_BUILD_MODULES+=("$module")
      ;;
    *)
      echo "❌ 未知参数: $arg"
      show_usage
      ;;
  esac
done

# ============================================================
# 智能检测 Common 依赖
# ============================================================

# 检查是否有 Go 模块参数（排除 Python 服务参数）
has_go_module_params() {
    for module in "${FORCE_BUILD_MODULES[@]}"; do
        # Python 服务列表
        if [[ "$module" != "python-workflow" &&
              "$module" != "math-workflow" &&
              "$module" != "model3d-workflow" &&
              "$module" != "pointcloud-workflow" &&
              "$module" != "supermap-workflow" &&
              "$module" != "copilot" &&
              "$module" != "agent" &&
              "$module" != "spark-workflow" &&
              "$module" != "jupyter" ]]; then
            return 0  # 有 Go 模块参数
        fi
    done
    return 1  # 只有 Python 服务参数或无参数
}

is_python_service_module() {
    case "$1" in
        python-workflow|math-workflow|model3d-workflow|pointcloud-workflow|supermap-workflow|spark-workflow|jupyter|copilot|agent)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

only_python_service_params() {
    if [ "$FORCE_BUILD_ALL" = true ] || [ ${#FORCE_BUILD_MODULES[@]} -eq 0 ]; then
        return 1
    fi
    for module in "${FORCE_BUILD_MODULES[@]}"; do
        if ! is_python_service_module "$module"; then
            return 1
        fi
    done
    return 0
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
    if [ -f "$dir/package-lock.json" ]; then
        (cd "$dir" && npm ci --omit=dev)
    else
        (cd "$dir" && npm install --omit=dev)
    fi
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

restart_python_workflow_service() {
    local port="${PYTHON_WORKFLOW_PORT:-8099}"
    stop_pidfile_process ".dev-pids/python-workflow-engine.pid" "GeoPython Workflow Engine"
    stop_matching_port_process "$port" "GeoPython Workflow Engine" "python.*api_server\\.py|engines/python-workflow"
    require_service_python "engines/python-workflow" "GeoPython Workflow Engine" "python-workflow"
    echo "  启动 GeoPython Workflow Engine..."
    (
        cd engines/python-workflow
        export PORT="$port"
        export INTERNAL_API_KEY="${INTERNAL_API_KEY:-}"
        export POSTGRES_HOST=localhost
        export POSTGRES_PORT=15432
        export POSTGRES_USER=addp
        export POSTGRES_PASSWORD=addp_password
        export POSTGRES_DB=addp
        export DB_SCHEMA=develop
        start_background_process "." ".dev-pids/python-workflow-engine.pid" "logs/python-workflow-engine.log" "logs/python-workflow-engine-stderr.log" ./venv/bin/python api_server.py
    )
    wait_http_ready "GeoPython Workflow Engine" "http://localhost:${port}/health"
    verify_pidfile_process_alive ".dev-pids/python-workflow-engine.pid" "GeoPython Workflow Engine" "logs/python-workflow-engine.log" "logs/python-workflow-engine-stderr.log"
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
        export INTERNAL_API_KEY="${INTERNAL_API_KEY:-}"
        start_background_process "." ".dev-pids/model3d-workflow-engine.pid" "logs/model3d-workflow-engine.log" "logs/model3d-workflow-engine-stderr.log" ./venv/bin/python api_server.py
    )
    wait_http_ready "Model3D Workflow Engine" "http://localhost:${port}/health"
    verify_pidfile_process_alive ".dev-pids/model3d-workflow-engine.pid" "Model3D Workflow Engine" "logs/model3d-workflow-engine.log" "logs/model3d-workflow-engine-stderr.log"
}

restart_pointcloud_workflow_service() {
    local port="${POINTCLOUD_WORKFLOW_PORT:-8102}"
    local image="${POINTCLOUD_WORKFLOW_IMAGE:-addp-pointcloud-workflow-engine:dev}"
    local source_dir="${POINTCLOUD_DATA_HOST_PATH:-${ROOT_DIR}/business/nfs/data}"
    local container_source_dir="${POINTCLOUD_DATA_CONTAINER_PATH:-${ROOT_DIR}/business/nfs/data}"
    local work_dir="${POINTCLOUD_WORK_HOST_PATH:-${ROOT_DIR}/data/pointcloud-work}"
    local system_port="${SYSTEM_BACKEND_PORT:-8180}"
    local minio_port="${MINIO_API_PORT:-19000}"

    if ! command -v docker >/dev/null 2>&1; then
        echo "❌ PointCloud Workflow Engine 需要 Docker runtime 承载 PDAL"
        return 1
    fi

    stop_matching_port_process "$port" "PointCloud Workflow Engine" "python.*api_server\\.py|engines/pointcloud-workflow"
    docker rm -f pointcloud-workflow-engine >/dev/null 2>&1 || true

    echo "  构建 PointCloud Workflow Engine 镜像..."
    docker build -t "$image" engines/pointcloud-workflow

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
        -e INTERNAL_API_KEY="${INTERNAL_API_KEY:-}" \
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
        "$image" > .dev-pids/pointcloud-workflow-engine.pid
    wait_http_ready "PointCloud Workflow Engine" "http://localhost:${port}/health"
    if ! curl -s "http://localhost:${port}/health" | grep -q '"status":"healthy"'; then
        echo "❌ PointCloud Workflow Engine 未进入 healthy 状态"
        echo "   查看日志: docker logs pointcloud-workflow-engine"
        return 1
    fi
    if ! docker ps --filter "name=^/pointcloud-workflow-engine$" --format '{{.Names}}' | grep -qx "pointcloud-workflow-engine"; then
        echo "❌ PointCloud Workflow Engine 容器启动后不存在"
        echo "   查看日志: docker logs pointcloud-workflow-engine"
        return 1
    fi
    echo "  ✓ PointCloud Workflow Engine Docker runtime 已启动"
}

restart_supermap_workflow_service() {
    local port="${SUPERMAP_WORKFLOW_PORT:-8103}"
    local image="${SUPERMAP_WORKFLOW_IMAGE:-addp-supermap-workflow-engine:dev}"
    local objectsjava_host="${SUPERMAP_OBJECTSJAVA_BIN_HOST:-${SUPERMAP_OBJECTSJAVA_BIN_HOST_PATH:-}}"
    local gpa_lib_host="${SUPERMAP_GPA_LIB_DIR_HOST:-${SUPERMAP_GPA_LIB_DIR_HOST_PATH:-}}"
    local output_dir="${SUPERMAP_OUTPUT_HOST_PATH:-/tmp/supermap-out}"
    local data_dir="${SUPERMAP_DATA_HOST_PATH:-}"
    local platform="${SUPERMAP_WORKFLOW_PLATFORM:-linux/arm64}"
    local memory_limit="${SUPERMAP_WORKFLOW_MEMORY_LIMIT:-8g}"
    local java_opts="${SUPERMAP_JAVA_OPTS:--Xms128m -Xmx4g}"
    local rebuild_image="${SUPERMAP_WORKFLOW_REBUILD:-${FORCE_SUPERMAP_WORKFLOW_IMAGE_BUILD:-0}}"
    local source_dir="${SUPERMAP_WORKFLOW_SOURCE_HOST:-${ROOT_DIR}/engines/supermap-workflow/src}"
    local mount_source="${SUPERMAP_WORKFLOW_MOUNT_SOURCE:-}"
    local container_objectsjava="/opt/supermap/objectsjava/bin_linux_arm64"
    local container_gpa_lib="/opt/supermap/gpa/libs"
    local image_exists=false
    local bundled_image=false

    if ! command -v docker >/dev/null 2>&1; then
        echo "❌ SuperMap Workflow Engine 需要 Docker runtime 承载 Linux arm64 SuperMap SDK"
        return 1
    fi

    if docker image inspect "$image" >/dev/null 2>&1; then
        image_exists=true
        if [ "$(docker image inspect -f '{{ index .Config.Labels "addp.supermap.bundled" }}' "$image" 2>/dev/null || true)" = "true" ]; then
            bundled_image=true
        fi
    fi

    if [ -z "$mount_source" ]; then
        if [ "$bundled_image" = true ]; then
            mount_source=0
        else
            mount_source=1
        fi
    fi
    if [ "$bundled_image" = true ] && [ "$mount_source" = "1" ]; then
        echo "❌ SuperMap bundled 镜像只包含 JRE，不能挂载源码后在容器内编译"
        echo "   请重新运行 scripts/build/build-supermap-workflow-image.sh 构建 Java 代码"
        return 1
    fi

    if [ "$rebuild_image" = "1" ] && [ "$bundled_image" = true ]; then
        echo "❌ ${image} 是 SuperMap bundled 镜像，不能用 SUPERMAP_WORKFLOW_REBUILD=1 覆盖为瘦镜像"
        echo "   请使用: scripts/build/build-supermap-workflow-image.sh --image ${image}"
        return 1
    fi

    if [ -z "$objectsjava_host" ] && [ -n "${SUPERMAP_OBJECTSJAVA_BIN:-}" ] && [ -d "${SUPERMAP_OBJECTSJAVA_BIN}" ]; then
        objectsjava_host="${SUPERMAP_OBJECTSJAVA_BIN}"
    fi
    if [ -z "$gpa_lib_host" ] && [ -n "${SUPERMAP_GPA_LIB_DIR:-}" ] && [ -d "${SUPERMAP_GPA_LIB_DIR}" ]; then
        gpa_lib_host="${SUPERMAP_GPA_LIB_DIR}"
    fi

    if [ "$bundled_image" != true ] && { [ -z "$objectsjava_host" ] || [ ! -d "$objectsjava_host" ]; }; then
        echo "❌ 请设置 SUPERMAP_OBJECTSJAVA_BIN_HOST 指向宿主机完整 iObjects Java Bin 目录"
        echo "   或先构建 bundled 镜像: scripts/build/build-supermap-workflow-image.sh"
        return 1
    fi
    if [ "$bundled_image" != true ] && { [ -z "$gpa_lib_host" ] || [ ! -d "$gpa_lib_host" ]; }; then
        echo "❌ 请设置 SUPERMAP_GPA_LIB_DIR_HOST 指向宿主机 GPA/SPS libs 目录"
        return 1
    fi
    if [ -n "$data_dir" ] && [ ! -d "$data_dir" ]; then
        echo "❌ SUPERMAP_DATA_HOST_PATH 不是有效目录: $data_dir"
        return 1
    fi

    docker rm -f supermap-workflow-engine >/dev/null 2>&1 || true
    if [ "$image_exists" = false ] || [ "$rebuild_image" = "1" ]; then
        echo "  构建 SuperMap Workflow Engine 镜像..."
        docker build -t "$image" engines/supermap-workflow
        bundled_image=false
    fi

    echo "  启动 SuperMap Workflow Engine Docker runtime..."
    mkdir -p "${output_dir}" .dev-pids
    local mount_args=(
        -v "${output_dir}:/tmp/supermap-out"
    )
    if [ "$bundled_image" != true ]; then
        mount_args+=(
            -v "${objectsjava_host}:${container_objectsjava}:ro"
            -v "${gpa_lib_host}:${container_gpa_lib}:ro"
        )
    fi
    if [ "$mount_source" = "1" ] && [ -d "$source_dir" ]; then
        mount_args+=(-v "${source_dir}:/app/src:ro")
    fi
    if [ -n "$data_dir" ]; then
        mount_args+=(-v "${data_dir}:/mnt/supermap/data:ro")
    fi

    docker run -d \
        --name supermap-workflow-engine \
        --label com.docker.compose.project=addp-app \
        --label com.docker.compose.service=supermap-workflow-engine \
        --label com.docker.compose.project.working_dir="${ROOT_DIR}" \
        --platform "$platform" \
        --add-host=host.docker.internal:host-gateway \
        --cap-add SYS_ADMIN \
        --security-opt apparmor=unconfined \
        --memory "$memory_limit" \
        -p "${port}:8103" \
        -e PORT=8103 \
        -e SUPERMAP_OBJECTSJAVA_BIN="${container_objectsjava}" \
        -e SUPERMAP_GPA_LIB_DIR="${container_gpa_lib}" \
        -e SUPERMAP_JAVA_OPTS="$java_opts" \
        -e SUPERMAP_RESOURCE_LOCALHOST_ALIAS="${SUPERMAP_RESOURCE_LOCALHOST_ALIAS:-host.docker.internal}" \
        "${mount_args[@]}" \
        "$image" > .dev-pids/supermap-workflow-engine.pid

    wait_http_ready "SuperMap Workflow Engine" "http://localhost:${port}/health" 90
    if ! curl -s "http://localhost:${port}/health" | grep -q '"status":"healthy"'; then
        echo "❌ SuperMap Workflow Engine 未进入 healthy 状态"
        echo "   查看日志: docker logs supermap-workflow-engine"
        echo "   确认 objectsjava Bin 目录内已放置许可文件，并且 GPA/SPS libs 完整"
        return 1
    fi
    if ! docker ps --filter "name=^/supermap-workflow-engine$" --format '{{.Names}}' | grep -qx "supermap-workflow-engine"; then
        echo "❌ SuperMap Workflow Engine 容器启动后不存在"
        echo "   查看日志: docker logs supermap-workflow-engine"
        return 1
    fi
    if [ -n "${INTERNAL_API_KEY:-}" ]; then
        local system_url="${SYSTEM_URL:-http://localhost:${SYSTEM_BACKEND_PORT:-8180}}"
        local response_file
        response_file=$(mktemp)
        local register_payload
        register_payload=$(cat <<JSON
{"engine_type":"supermap_workflow","name":"SuperMap 工作流引擎","description":"面向超图 iObjects Java / SPS 的工作流运行时","connection_info":{"protocol":"http","port":${port}},"is_builtin":true}
JSON
)
        local http_code
        http_code=$(curl -s -o "$response_file" -w "%{http_code}" \
            -H "X-Internal-API-Key: ${INTERNAL_API_KEY}" \
            -H "Content-Type: application/json" \
            -d "$register_payload" \
            "${system_url%/}/api/v1/internal/engines/register" || true)
        if [ "$http_code" = "202" ] || [ "$http_code" = "200" ]; then
            echo "  ✓ SuperMap Workflow Engine 已注册到 System"
        else
            echo "  ⚠️  SuperMap Workflow Engine 自动注册到 System 失败（HTTP ${http_code:-000}）"
            echo "     $(head -c 200 "$response_file")"
        fi
        rm -f "$response_file"
    else
        echo "  ⚠️  INTERNAL_API_KEY 未设置，跳过 SuperMap Workflow Engine 自动注册"
    fi
    echo "  ✓ SuperMap Workflow Engine Docker runtime 已启动"
}

restart_spark_workflow_service() {
    local port="${SPARK_WORKFLOW_PORT:-8098}"
    stop_pidfile_process ".dev-pids/spark-workflow-engine.pid" "Spark Workflow Engine"
    stop_matching_port_process "$port" "Spark Workflow Engine" "python.*api_server\\.py|engines/spark-workflow"
    require_service_python "engines/spark-workflow" "Spark Workflow Engine" "spark-workflow"
    echo "  启动 Spark Workflow Engine..."
    (
        cd engines/spark-workflow
        export PORT="$port"
        export INTERNAL_API_KEY="${INTERNAL_API_KEY:-}"
        start_background_process "." ".dev-pids/spark-workflow-engine.pid" "logs/spark-workflow-engine.log" "logs/spark-workflow-engine-stderr.log" ./venv/bin/python api_server.py
    )
    wait_http_ready "Spark Workflow Engine" "http://localhost:${port}/health"
    verify_pidfile_process_alive ".dev-pids/spark-workflow-engine.pid" "Spark Workflow Engine" "logs/spark-workflow-engine.log" "logs/spark-workflow-engine-stderr.log"
}

restart_jupyter_service() {
    local api_port="${JUPYTER_API_PORT:-8097}"
    local lab_port="${JUPYTER_LAB_PORT:-8088}"
    stop_pidfile_process ".dev-pids/jupyter-api-server.pid" "Jupyter API Server"
    stop_pidfile_process ".dev-pids/jupyter-lab.pid" "Jupyter Lab"
    stop_matching_port_process "$api_port" "Jupyter API Server" "python.*api_server\\.py|engines/jupyter"
    stop_matching_port_process "$lab_port" "Jupyter Lab" "jupyter.*lab|engines/jupyter"
    require_service_python "engines/jupyter" "Jupyter Engine" "jupyter"
    if [ ! -x "engines/jupyter/venv/bin/jupyter" ]; then
        echo "❌ Jupyter Lab 可执行文件不存在: engines/jupyter/venv/bin/jupyter"
        echo "   请先运行: bash scripts/dev/start.sh -jupyter"
        return 1
    fi
    echo "  启动 Jupyter Engine..."
    (
        cd engines/jupyter
        export API_PORT="$api_port"
        export JUPYTER_PORT="$lab_port"
        export INTERNAL_API_KEY="${INTERNAL_API_KEY:-}"
        export TENANT_ID="${DEFAULT_TENANT_ID:-1}"
        start_background_process "." ".dev-pids/jupyter-api-server.pid" "logs/jupyter-api-server.log" "logs/jupyter-api-server-stderr.log" ./venv/bin/python api_server.py
        start_background_process "." ".dev-pids/jupyter-lab.pid" "logs/jupyter-lab.log" "logs/jupyter-lab-stderr.log" ./venv/bin/jupyter lab --config=jupyter_lab_config.py
    )
    wait_http_ready "Jupyter API Server" "http://localhost:${api_port}/health"
    verify_pidfile_process_alive ".dev-pids/jupyter-api-server.pid" "Jupyter API Server" "logs/jupyter-api-server.log" "logs/jupyter-api-server-stderr.log"
    wait_http_ready "Jupyter Lab" "http://localhost:${lab_port}/lab" 60 || true
    verify_pidfile_process_alive ".dev-pids/jupyter-lab.pid" "Jupyter Lab" "logs/jupyter-lab.log" "logs/jupyter-lab-stderr.log"
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
    wait_http_ready "Copilot Backend" "http://localhost:${port}/health"
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
    wait_http_ready "Agent Backend" "http://localhost:${port}/health"
    verify_pidfile_process_alive ".dev-pids/agent-backend.pid" "Agent Backend" "logs/agent-backend.log" "logs/agent-backend-stderr.log"
}

restart_scoped_python_services() {
    echo "🐍 局部重启 Python/扩展服务: ${FORCE_BUILD_MODULES[*]}"
    mkdir -p logs .dev-pids
    for module in "${FORCE_BUILD_MODULES[@]}"; do
        case "$module" in
            python-workflow)
                restart_python_workflow_service
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

# 在用户未指定 Go 模块编译选项时，自动检测 common 变化
if [ "$FORCE_BUILD_ALL" = false ] && ! has_go_module_params; then
    echo "🔍 检测 common 模块依赖..."

    # 加载检测函数
    source "${SCRIPT_DIR}/../utils/detect-common.sh"

    # 执行智能检测
    AFFECTED_MODULES=$(detect_common_affected_modules)

    if [ -n "$AFFECTED_MODULES" ]; then
        echo "📦 检测到 common 模块已更新，以下模块需要重新编译:"
        echo "   ${AFFECTED_MODULES}"
        echo ""

        # 自动标记受影响的模块需要重新编译
        for module in $AFFECTED_MODULES; do
            FORCE_BUILD_MODULES+=("$module")
        done

        echo "✅ 已自动标记 ${#FORCE_BUILD_MODULES[@]} 个模块需要重新编译"
    else
        echo "✅ common 模块无变化，使用增量编译"
    fi
    echo ""
fi

# ============================================================
# 显示编译计划
# ============================================================

# 显示编译计划
if [ "$FORCE_BUILD_ALL" = true ]; then
  echo "📦 编译计划: 重新编译所有模块"
elif [ ${#FORCE_BUILD_MODULES[@]} -gt 0 ]; then
  echo "📦 编译计划: 重新编译 ${FORCE_BUILD_MODULES[*]}"
else
  echo "📦 编译计划: 仅重启服务,按需增量编译"
fi
echo ""

# 1. 先强制杀死 Python 服务（避免端口残留导致 stop.sh 误判为非 ADDP 进程）
echo "🐍 强制终止 Python 服务..."
pkill -9 -f "engines/python-workflow/api_server.py" 2>/dev/null || true
pkill -9 -f "engines/math-workflow/api_server.py" 2>/dev/null || true
pkill -9 -f "engines/model3d-workflow/api_server.py" 2>/dev/null || true
pkill -9 -f "engines/pointcloud-workflow/api_server.py" 2>/dev/null || true
pkill -9 -f "engines/spark-workflow/api_server.py" 2>/dev/null || true
pkill -9 -f "engines/jupyter/api_server.py" 2>/dev/null || true
pkill -9 -f "jupyter.*lab" 2>/dev/null || true
pkill -9 -f "copilot/backend/main.py" 2>/dev/null || true
pkill -9 -f "agent/backend/main.py" 2>/dev/null || true
pkill -9 -f "uvicorn" 2>/dev/null || true
echo ""

# 2. 停止服务
if "${SCRIPT_DIR}/stop.sh"; then
  echo ""
  echo "✅ 已停止现有服务"
else
  echo ""
  echo "⚠️ 停止脚本返回非零状态,继续执行启动流程"
fi

# 3. 强制重新编译(如果需要)
if [ "$FORCE_BUILD_ALL" = true ]; then
  echo ""
  echo "🔨 强制重新编译所有模块..."

  # Touch 所有 .go 文件
  find . -type f -name "*.go" -path "*/backend/*" -exec touch {} \; 2>/dev/null || true
  find . -type f -name "*.go" -path "*/common/*" -exec touch {} \; 2>/dev/null || true
  find . -type f -name "*.go" -path "*/gateway/*" -exec touch {} \; 2>/dev/null || true

  # 删除所有二进制
  rm -rf .dev-bins 2>/dev/null || true

  # 清理构建缓存
  go clean -cache 2>/dev/null || true

  # 重新生成并校验所有模块的 Swagger 文档
  echo "📄 重新生成所有模块 Swagger 文档..."
  run_swagger_generate all
  echo "🔎 校验所有模块 Swagger 路由覆盖..."
  run_swagger_coverage_check all || true

  echo "✅ 已标记所有模块需要重新编译"

elif [ ${#FORCE_BUILD_MODULES[@]} -gt 0 ]; then
  echo ""
  echo "🔨 强制重新编译指定模块..."

  for module in "${FORCE_BUILD_MODULES[@]}"; do
    echo "  处理 $module 模块..."

    # 生成并校验 Swagger 文档（所有 Go 后端模块，跳过 Python 服务和 gateway）
    if is_swagger_module "$module"; then
      run_swagger_generate "$module"
      run_swagger_coverage_check "$module" || true
    fi

    # Touch 指定模块的源文件
    if [ "$module" = "gateway" ]; then
      find gateway -type f -name "*.go" -exec touch {} \; 2>/dev/null || true
    elif [ "$module" = "copilot" ]; then
      # Copilot 是 Python 服务，不需要编译，只需清理虚拟环境
      echo "  标记 Copilot Backend 需要重启（无需编译）"
    elif [ "$module" = "agent" ]; then
      # Agent 是 Python 服务，不需要编译
      echo "  标记 Agent Backend 需要重启（无需编译）"
    elif [ "$module" = "python-workflow" ]; then
      # GeoPython Workflow Engine 是 Python 服务，不需要编译
      echo "  标记 GeoPython Workflow Engine 需要重启（无需编译）"
    elif [ "$module" = "math-workflow" ]; then
      # Math Workflow Engine 是 Python 服务，不需要编译
      echo "  标记 Math Workflow Engine 需要重启（无需编译）"
    elif [ "$module" = "model3d-workflow" ]; then
      # Model3D Workflow Engine 是 Python 服务，不需要编译
      echo "  标记 Model3D Workflow Engine 需要重启（无需编译）"
    elif [ "$module" = "pointcloud-workflow" ]; then
      # PointCloud Workflow Engine 是 Docker runtime，不需要 Go 编译
      echo "  标记 PointCloud Workflow Engine 需要重启（无需 Go 编译）"
    elif [ "$module" = "supermap-workflow" ]; then
      # SuperMap Workflow Engine 是 Docker runtime，不需要 Go 编译
      echo "  标记 SuperMap Workflow Engine 需要重启（无需 Go 编译）"
    elif [ "$module" = "spark-workflow" ]; then
      # Spark 工作流 Engine 是 Python 服务，不需要编译
      echo "  标记 Spark 工作流 Engine 需要重启（无需编译）"
    elif [ "$module" = "jupyter" ]; then
      # Jupyter Engine 是 Python 服务，不需要编译
      echo "  标记 Jupyter Engine 需要重启（无需编译）"
    elif [ "$module" = "standard" ]; then
      find "${module}/backend" -type f -name "*.go" -exec touch {} \; 2>/dev/null || true
    elif [ "$module" = "model" ]; then
      find "${module}/backend" -type f -name "*.go" -exec touch {} \; 2>/dev/null || true
    elif [ "$module" = "quality" ]; then
      find "${module}/backend" -type f -name "*.go" -exec touch {} \; 2>/dev/null || true
    else
      find "${module}/backend" -type f -name "*.go" -exec touch {} \; 2>/dev/null || true
    fi

    # 删除指定模块的二进制
    if [ "$module" = "gateway" ]; then
      rm -f .dev-bins/addp-gateway 2>/dev/null || true
    elif [ "$module" = "copilot" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "agent" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "python-workflow" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "math-workflow" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "model3d-workflow" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "pointcloud-workflow" ]; then
      # Docker runtime 无 Go 二进制文件
      :
    elif [ "$module" = "supermap-workflow" ]; then
      # Docker runtime 无 Go 二进制文件
      :
    elif [ "$module" = "spark-workflow" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "jupyter" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "standard" ]; then
      rm -f .dev-bins/addp-standard 2>/dev/null || true
    elif [ "$module" = "model" ]; then
      rm -f .dev-bins/addp-model 2>/dev/null || true
    elif [ "$module" = "quality" ]; then
      rm -f .dev-bins/addp-quality 2>/dev/null || true
    else
      rm -f .dev-bins/addp-${module} 2>/dev/null || true
      rm -f .dev-bins/addp-${module}-worker 2>/dev/null || true
    fi

    # 清理指定模块的构建缓存
    if [ "$module" = "gateway" ]; then
      (cd gateway && go clean -cache 2>/dev/null) || true
    elif [ "$module" = "copilot" ]; then
      # Python 服务无需清理 Go 缓存
      :
    elif [ "$module" = "agent" ]; then
      # Python 服务无需清理 Go 缓存
      :
    elif [ "$module" = "python-workflow" ]; then
      # Python 服务无需清理 Go 缓存
      :
    elif [ "$module" = "math-workflow" ]; then
      # Python 服务无需清理 Go 缓存
      :
    elif [ "$module" = "model3d-workflow" ]; then
      # Python 服务无需清理 Go 缓存
      :
    elif [ "$module" = "pointcloud-workflow" ]; then
      # Docker runtime 无需清理 Go 缓存
      :
    elif [ "$module" = "supermap-workflow" ]; then
      # Docker runtime 无需清理 Go 缓存
      :
    elif [ "$module" = "spark-workflow" ]; then
      # Python 服务无需清理 Go 缓存
      :
    elif [ "$module" = "jupyter" ]; then
      # Python 服务无需清理 Go 缓存
      :
    elif [ "$module" = "standard" ]; then
      (cd "${module}/backend" && go clean -cache 2>/dev/null) || true
    elif [ "$module" = "model" ]; then
      (cd "${module}/backend" && go clean -cache 2>/dev/null) || true
    elif [ "$module" = "quality" ]; then
      (cd "${module}/backend" && go clean -cache 2>/dev/null) || true
    else
      (cd "${module}/backend" && go clean -cache 2>/dev/null) || true
    fi
  done

  # 总是 touch common 模块(因为其他模块可能依赖它)
  find common -type f -name "*.go" -exec touch {} \; 2>/dev/null || true

  echo "✅ 已标记 ${FORCE_BUILD_MODULES[*]} 需要重新编译"
fi

echo ""

# 4. 启动服务
# restart 时跳过 go mod tidy（模块依赖在重启间不会改变，避免网络调用拖慢速度）
START_ARGS=()
if [ "$FORCE_BUILD_ALL" = false ] && [ ${#ORIGINAL_ARGS[@]} -eq 1 ]; then
  START_ARGS=("${ORIGINAL_ARGS[0]}")
fi
exec env SKIP_MODTIDY=1 "${SCRIPT_DIR}/start.sh" "${START_ARGS[@]}"
