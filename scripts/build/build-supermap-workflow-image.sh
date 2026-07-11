#!/bin/bash
# =============================================================================
# ADDP SuperMap Workflow Image Builder
# =============================================================================
# Builds the private bundled SuperMap Workflow Engine image. The resulting image
# contains ADDP supermap_workflow runtime code, SuperMap iObjects Java Bin, and
# GPA/SPS libs. License files are copied only from ignored local paths.
# =============================================================================

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENGINE_DIR="${PROJECT_ROOT}/engines/supermap-workflow"

IMAGE="${SUPERMAP_WORKFLOW_IMAGE:-addp-supermap-workflow-engine:dev}"
PLATFORM="${SUPERMAP_WORKFLOW_PLATFORM:-linux/arm64}"
BASE_IMAGE="${SUPERMAP_WORKFLOW_BASE_IMAGE:-192.168.106.71/datacenter/runtime-notebook-python:v3.0.0-aarch64}"
OBJECTSJAVA_BIN="${SUPERMAP_OBJECTSJAVA_BIN_HOST:-${SUPERMAP_OBJECTSJAVA_BIN_HOST_PATH:-}}"
GPA_LIB_DIR="${SUPERMAP_GPA_LIB_DIR_HOST:-${SUPERMAP_GPA_LIB_DIR_HOST_PATH:-}}"
LICENSE_FILE="${SUPERMAP_LICENSE_HOST:-}"
PUSH=false
NO_CACHE=false

usage() {
  cat <<EOF
Usage: $0 [OPTIONS]

Options:
  --image IMAGE              Target image (default: ${IMAGE})
  --platform PLATFORM        Docker platform (default: ${PLATFORM})
  --base-image IMAGE         Base image (default: ${BASE_IMAGE})
  --objectsjava-bin PATH     SuperMap iObjects Java Bin directory
  --gpa-libs PATH            SuperMap GPA/SPS libs directory
  --license PATH             Optional .lic12 file to bake into the private image
  --push                     Push image after build
  --no-cache                 Build without Docker cache
  -h, --help                 Show this help

Environment variables:
  SUPERMAP_WORKFLOW_IMAGE
  SUPERMAP_WORKFLOW_PLATFORM
  SUPERMAP_WORKFLOW_BASE_IMAGE
  SUPERMAP_OBJECTSJAVA_BIN_HOST
  SUPERMAP_GPA_LIB_DIR_HOST
  SUPERMAP_LICENSE_HOST

If --license is omitted, the script will automatically use the first
engines/supermap-workflow/license/*.lic12 file when present. That directory is
git-ignored and is intended for local private license files.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --image)
      IMAGE="$2"
      shift 2
      ;;
    --platform)
      PLATFORM="$2"
      shift 2
      ;;
    --base-image)
      BASE_IMAGE="$2"
      shift 2
      ;;
    --objectsjava-bin)
      OBJECTSJAVA_BIN="$2"
      shift 2
      ;;
    --gpa-libs)
      GPA_LIB_DIR="$2"
      shift 2
      ;;
    --license)
      LICENSE_FILE="$2"
      shift 2
      ;;
    --push)
      PUSH=true
      shift
      ;;
    --no-cache)
      NO_CACHE=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo -e "${RED}Unknown option: $1${NC}" >&2
      usage
      exit 1
      ;;
  esac
done

if [ -z "${OBJECTSJAVA_BIN}" ] || [ ! -d "${OBJECTSJAVA_BIN}" ]; then
  echo -e "${RED}Error: --objectsjava-bin must point to a SuperMap iObjects Java Bin directory${NC}" >&2
  exit 1
fi
if [ -z "${GPA_LIB_DIR}" ] || [ ! -d "${GPA_LIB_DIR}" ]; then
  echo -e "${RED}Error: --gpa-libs must point to the SuperMap GPA/SPS libs directory${NC}" >&2
  exit 1
fi
if [ -n "${LICENSE_FILE}" ] && [ ! -f "${LICENSE_FILE}" ]; then
  echo -e "${RED}Error: --license file does not exist: ${LICENSE_FILE}${NC}" >&2
  exit 1
fi

if [ -z "${LICENSE_FILE}" ]; then
  LICENSE_FILE="$(find "${ENGINE_DIR}/license" -maxdepth 1 -type f -name "*.lic12" 2>/dev/null | head -n 1 || true)"
fi

LICENSE_CONTEXT="$(mktemp -d)"
cleanup() {
  rm -rf "${LICENSE_CONTEXT}"
}
trap cleanup EXIT

if [ -n "${LICENSE_FILE}" ]; then
  cp "${LICENSE_FILE}" "${LICENSE_CONTEXT}/"
fi

BUILD_ARGS=(
  --platform "${PLATFORM}"
  --build-arg "BASE_IMAGE=${BASE_IMAGE}"
  --build-context "objectsjava=${OBJECTSJAVA_BIN}"
  --build-context "gpa_libs=${GPA_LIB_DIR}"
  --build-context "license=${LICENSE_CONTEXT}"
  -f "${ENGINE_DIR}/Dockerfile.bundled"
  -t "${IMAGE}"
)
if [ "${NO_CACHE}" = true ]; then
  BUILD_ARGS+=(--no-cache)
fi

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP SuperMap Workflow Image Builder${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "Image:          ${GREEN}${IMAGE}${NC}"
echo -e "Platform:       ${GREEN}${PLATFORM}${NC}"
echo -e "Base image:     ${GREEN}${BASE_IMAGE}${NC}"
echo -e "ObjectsJava:    ${GREEN}${OBJECTSJAVA_BIN}${NC}"
echo -e "GPA/SPS libs:   ${GREEN}${GPA_LIB_DIR}${NC}"
if [ -n "${LICENSE_FILE}" ]; then
  echo -e "License:        ${GREEN}${LICENSE_FILE}${NC}"
else
  echo -e "License:        ${YELLOW}none baked; runtime must use SuperMap external licensing${NC}"
fi
echo ""

docker build "${BUILD_ARGS[@]}" "${ENGINE_DIR}"

echo -e "${YELLOW}Smoke checking ${IMAGE}...${NC}"
docker run -d --rm \
  --name addp-supermap-workflow-image-smoke \
  --platform "${PLATFORM}" \
  -e PORT=8103 \
  "${IMAGE}" >/tmp/addp-supermap-workflow-smoke.cid

SMOKE_CID="$(cat /tmp/addp-supermap-workflow-smoke.cid)"
rm -f /tmp/addp-supermap-workflow-smoke.cid
cleanup_smoke() {
  docker rm -f "${SMOKE_CID}" >/dev/null 2>&1 || true
}
trap 'cleanup_smoke; cleanup' EXIT

for _ in $(seq 1 90); do
  if docker exec "${SMOKE_CID}" curl -fsS http://localhost:8103/health >/tmp/addp-supermap-workflow-smoke-health.json 2>/dev/null; then
    if grep -q '"status":"healthy"' /tmp/addp-supermap-workflow-smoke-health.json; then
      break
    fi
  fi
  sleep 1
done

if ! grep -q '"status":"healthy"' /tmp/addp-supermap-workflow-smoke-health.json 2>/dev/null; then
  echo -e "${RED}Error: smoke check failed; container health did not become healthy${NC}" >&2
  docker logs "${SMOKE_CID}" >&2 || true
  exit 1
fi
rm -f /tmp/addp-supermap-workflow-smoke-health.json
cleanup_smoke
trap cleanup EXIT

if [ "${PUSH}" = true ]; then
  echo -e "${YELLOW}Pushing ${IMAGE}...${NC}"
  docker push "${IMAGE}"
fi

echo -e "${GREEN}✓ Built SuperMap Workflow bundled image: ${IMAGE}${NC}"
