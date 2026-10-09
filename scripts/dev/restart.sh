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
  echo "  - 所有模块统一先停止，再由 start.sh 准备依赖、按需构建并启动"
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
addp_acquire_lifecycle_lock restart "$@"

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

echo "📦 构建计划: 同步 Swagger，按完整构建指纹复用或编译产物"
echo ""

# 全量先停止；局部在所选 Go 模块的 Swagger 检查通过后停止。
# 所有依赖准备交由停止后的 start.sh 执行，避免改写活动 Runtime。
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
