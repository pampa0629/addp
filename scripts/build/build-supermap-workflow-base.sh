#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENGINE_DIR="${ROOT_DIR}/engines/supermap-workflow"
VENDOR_DIR="${ENGINE_DIR}/vendor"
IMAGE="${SUPERMAP_WORKFLOW_BASE_IMAGE:-addp-supermap-workflow-base:local}"
PLATFORM="${SUPERMAP_WORKFLOW_PLATFORM:-linux/arm64}"
BUILD_IMAGE="${SUPERMAP_WORKFLOW_BUILD_IMAGE:-192.168.106.71/datacenter/runtime-notebook-python:v3.0.0-aarch64}"

require_dir() {
  if [ ! -d "$1" ] || [ -z "$(find "$1" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)" ]; then
    echo "❌ 缺少 SuperMap 本地依赖目录或目录为空: $1" >&2
    exit 1
  fi
}

require_dir "${VENDOR_DIR}/objectsjava"
require_dir "${VENDOR_DIR}/gpa-libs"
require_dir "${VENDOR_DIR}/license"

echo "🏗️  构建 SuperMap Workflow 基础镜像: ${IMAGE}"
docker build \
  --no-cache \
  --platform "${PLATFORM}" \
  --build-arg "BASE_IMAGE=${BUILD_IMAGE}" \
  --build-context "objectsjava=${VENDOR_DIR}/objectsjava" \
  --build-context "gpa_libs=${VENDOR_DIR}/gpa-libs" \
  --build-context "license=${VENDOR_DIR}/license" \
  -f "${ENGINE_DIR}/Dockerfile.base" \
  -t "${IMAGE}" \
  "${ENGINE_DIR}"

docker run --rm --platform "${PLATFORM}" "${IMAGE}" sh -lc \
  'javac -version && test -d "$SUPERMAP_OBJECTSJAVA_BIN" && test -d "$SUPERMAP_GPA_LIB_DIR"'

echo "✅ SuperMap Workflow 基础镜像构建完成: ${IMAGE}"
