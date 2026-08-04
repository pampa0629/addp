#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENGINE_DIR="${ROOT_DIR}/engines/supermap-workflow"
LICENSE_DIR="${ENGINE_DIR}/vendor/license"
SDK_PATH="${SUPERMAP_CPP_SDK_PATH:-}"
IMAGE="${SUPERMAP_WORKFLOW_BASE_IMAGE:-addp-supermap-workflow-base:local}"
PLATFORM="${SUPERMAP_WORKFLOW_PLATFORM:-linux/arm64}"
BUILD_IMAGE="${SUPERMAP_WORKFLOW_BUILD_IMAGE:-ubuntu:24.04}"

require_file() {
  if [ ! -f "$1" ]; then
    echo "❌ 缺少 SuperMap C++ SDK 文件: $1" >&2
    exit 1
  fi
}

if [ -z "${SDK_PATH}" ] || [ ! -d "${SDK_PATH}" ]; then
  echo "❌ SUPERMAP_CPP_SDK_PATH 必须指向完整 SuperMap iObjects C++ SDK 母版" >&2
  exit 1
fi
if ! find "${LICENSE_DIR}" -maxdepth 1 -type f -name '*.lic12' -print -quit | grep -q .; then
  echo "❌ ${LICENSE_DIR} 中缺少独立许可文件 (*.lic12)" >&2
  exit 1
fi

require_file "${SDK_PATH}/include/Engine/UGDataSource.h"
require_file "${SDK_PATH}/include/private/CacheBuilder/UGOSGBCacheBuilder.h"
require_file "${SDK_PATH}/bin/bin/libSuEngine.so"
require_file "${SDK_PATH}/bin/bin/libSuCacheBuilder.so"
require_file "${SDK_PATH}/bin/bin/libSuTileStorage.so"
require_file "${SDK_PATH}/bin/bin/libSuBase3D.so"

echo "🏗️  使用完整只读 C++ SDK 构建 SuperMap Workflow 基础镜像: ${IMAGE}"
docker build \
  --no-cache \
  --platform "${PLATFORM}" \
  --build-arg "BASE_IMAGE=${BUILD_IMAGE}" \
  --build-context "supermap_sdk=${SDK_PATH}" \
  --build-context "license=${LICENSE_DIR}" \
  -f "${ENGINE_DIR}/Dockerfile.base" \
  -t "${IMAGE}" \
  "${ENGINE_DIR}"

docker run --rm --platform "${PLATFORM}" "${IMAGE}" sh -lc \
  'test -f "$SUPERMAP_CPP_SDK_ROOT/include/Engine/UGDataSource.h" \
    && test -f "$SUPERMAP_CPP_SDK_ROOT/include/private/CacheBuilder/UGOSGBCacheBuilder.h" \
    && test -f "$SUPERMAP_CPP_SDK_ROOT/bin/bin/libSuEngine.so" \
    && find "$SUPERMAP_CPP_SDK_ROOT/bin/bin" -maxdepth 1 -type f -name "*.lic12" -print -quit | grep -q .'

echo "✅ SuperMap Workflow C++ 基础镜像构建完成: ${IMAGE}"
