#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

if [ -f ".env" ]; then
  set -a
  source .env
  set +a
fi
port="${SUPERMAP_WORKFLOW_PORT:-8103}"
base_image="${SUPERMAP_WORKFLOW_BASE_IMAGE:-addp-supermap-workflow-base:local}"
image="${SUPERMAP_WORKFLOW_IMAGE:-addp-supermap-workflow-engine:dev}"
platform="${SUPERMAP_WORKFLOW_PLATFORM:-linux/arm64}"
output_dir="${SUPERMAP_OUTPUT_HOST_PATH:-/tmp/supermap-out}"
data_dir="${SUPERMAP_DATA_HOST_PATH:-}"
memory_limit="${SUPERMAP_WORKFLOW_MEMORY_LIMIT:-8g}"

if ! command -v docker >/dev/null 2>&1; then
  echo "❌ SuperMap Workflow Engine 需要 Docker" >&2
  exit 1
fi
base_label="$(docker image inspect -f '{{ index .Config.Labels "addp.supermap.base" }}' "${base_image}" 2>/dev/null || true)"
code_label="$(docker image inspect -f '{{ index .Config.Labels "addp.supermap.code-image" }}' "${base_image}" 2>/dev/null || true)"
if [ "${base_label}" != "true" ] || [ "${code_label}" = "true" ]; then
  echo "❌ SuperMap Workflow 基础镜像不存在或类型不正确: ${base_image}" >&2
  echo "   请先运行: bash scripts/build/build-supermap-workflow-base.sh" >&2
  exit 1
fi
if [ -n "${data_dir}" ] && [ ! -d "${data_dir}" ]; then
  echo "❌ SUPERMAP_DATA_HOST_PATH 不是有效目录: ${data_dir}" >&2
  exit 1
fi

supermap_workflow_source_fingerprint() {
  local base_image_id="$1"
  {
    printf '%s\n' \
      "supermap-workflow-image-v1" \
      "base-image=${base_image_id}" \
      "platform=${platform}"
    while IFS= read -r file; do
      printf '%s %s\n' "$file" "$(git hash-object "$file")"
    done < <(
      {
        printf '%s\n' \
          engines/supermap-workflow/CMakeLists.txt \
          engines/supermap-workflow/Dockerfile \
          engines/supermap-workflow/run.sh \
          engines/supermap-workflow/config/operators.json
        find engines/supermap-workflow/src -type f
      } | LC_ALL=C sort
    )
  } | git hash-object --stdin
}

ensure_supermap_workflow_image() {
  local base_image_id
  local source_fingerprint
  local current_fingerprint
  base_image_id="$(docker image inspect -f '{{.Id}}' "${base_image}")"
  source_fingerprint="$(supermap_workflow_source_fingerprint "${base_image_id}")"
  current_fingerprint="$(docker image inspect \
    -f '{{ index .Config.Labels "addp.supermap.source-fingerprint" }}' \
    "${image}" 2>/dev/null || true)"

  if [ "${current_fingerprint}" = "${source_fingerprint}" ]; then
    echo "  SuperMap Workflow Engine 构建输入未变化，复用现有镜像: ${image}"
    return 0
  fi

  echo "  编译 SuperMap Workflow C++ Runtime（构建输入已变化或镜像不存在）..."
  docker build \
    --platform "${platform}" \
    --build-arg "BASE_IMAGE=${base_image}" \
    --label "addp.supermap.source-fingerprint=${source_fingerprint}" \
    -f engines/supermap-workflow/Dockerfile \
    -t "${image}" \
    engines/supermap-workflow
}

ensure_supermap_workflow_image

docker rm -f supermap-workflow-engine >/dev/null 2>&1 || true
mkdir -p "${output_dir}" .dev-pids

mount_args=(-v "${output_dir}:/tmp/supermap-out")
if [ -n "${data_dir}" ]; then
  mount_args+=(-v "${data_dir}:/mnt/supermap/data:ro")
fi

echo "  启动 SuperMap Workflow Engine..."
docker run -d \
  --name supermap-workflow-engine \
  --label com.docker.compose.project=addp-app \
  --label com.docker.compose.service=supermap-workflow-engine \
  --label com.docker.compose.project.working_dir="${ROOT_DIR}" \
  --platform "${platform}" \
  --add-host=host.docker.internal:host-gateway \
  --cap-add SYS_ADMIN \
  --security-opt apparmor=unconfined \
  --memory "${memory_limit}" \
  -p "${port}:8103" \
  -e PORT=8103 \
  -e SUPERMAP_RESOURCE_LOCALHOST_ALIAS="${SUPERMAP_RESOURCE_LOCALHOST_ALIAS:-host.docker.internal}" \
  "${mount_args[@]}" \
  "${image}" > .dev-pids/supermap-workflow-engine.pid

echo -n "  等待 SuperMap Workflow Engine 就绪"
for _ in $(seq 1 90); do
  if curl -fsS "http://localhost:${port}/health" 2>/dev/null | grep -q '"status":"healthy"'; then
    echo " ✓"
    break
  fi
  echo -n "."
  sleep 1
done
if ! curl -fsS "http://localhost:${port}/health" 2>/dev/null | grep -q '"status":"healthy"'; then
  echo " ✗" >&2
  docker logs --tail 100 supermap-workflow-engine >&2 || true
  exit 1
fi

if [ -n "${INTERNAL_API_KEY:-}" ]; then
  system_url="${SYSTEM_URL:-http://localhost:${SYSTEM_BACKEND_PORT:-8180}}"
  response_file="$(mktemp)"
  http_code="$(curl -sS -o "${response_file}" -w '%{http_code}' \
    -H "X-Internal-API-Key: ${INTERNAL_API_KEY}" \
    -H "Content-Type: application/json" \
    -d "{\"engine_type\":\"supermap_workflow\",\"name\":\"SuperMap 工作流引擎\",\"description\":\"面向超图 iObjects C++ 的工作流运行时\",\"connection_info\":{\"protocol\":\"http\",\"port\":${port}},\"is_builtin\":true}" \
    "${system_url%/}/api/v1/internal/engines/register" || true)"
  if [ "${http_code}" = "200" ] || [ "${http_code}" = "202" ]; then
    echo "  ✓ SuperMap Workflow Engine 已注册到 System"
  else
    echo "  ⚠️  自动注册到 System 失败（HTTP ${http_code:-000}）"
    head -c 200 "${response_file}" || true
    echo
  fi
  rm -f "${response_file}"
else
  echo "  ⚠️  INTERNAL_API_KEY 未设置，跳过自动注册"
fi

echo "  ✓ SuperMap Workflow Engine 已使用当前源码启动"
