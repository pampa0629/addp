#!/usr/bin/env bash
set -euo pipefail

APP_ROOT="${APP_ROOT:-/app}"
SUPERMAP_ROOT="${SUPERMAP_CPP_SDK_ROOT:-/opt/supermap}"
SUPERMAP_BIN="${SUPERMAP_ROOT}/bin/bin"
SERVER="${APP_ROOT}/bin/supermap-workflow-server"
OPERATORS="${SUPERMAP_OPERATORS_CONFIG:-${APP_ROOT}/config/operators.json}"

if [ ! -x "${SERVER}" ]; then
  echo "SuperMap Workflow C++ server is missing: ${SERVER}" >&2
  exit 1
fi
if [ ! -f "${OPERATORS}" ]; then
  echo "SuperMap operator catalog is missing: ${OPERATORS}" >&2
  exit 1
fi
if [ ! -f "${SUPERMAP_BIN}/libSuEngine.so" ]; then
  echo "SUPERMAP_CPP_SDK_ROOT does not contain the iObjects C++ runtime: ${SUPERMAP_ROOT}" >&2
  exit 1
fi
if ! find "${SUPERMAP_BIN}" -maxdepth 1 -type f -name '*.lic12' -print -quit | grep -q .; then
  echo "SuperMap C++ license is missing from ${SUPERMAP_BIN}" >&2
  exit 1
fi

export LD_LIBRARY_PATH="${SUPERMAP_BIN}${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
export LD_PRELOAD="${LD_PRELOAD:-/lib/aarch64-linux-gnu/libfreetype.so.6}"
export QT_QPA_PLATFORM="${QT_QPA_PLATFORM:-offscreen}"

exec "${SERVER}"
