#!/usr/bin/env bash
set -euo pipefail

APP_ROOT="${APP_ROOT:-/app}"
SRC_DIR="${APP_ROOT}/src/main/java"
CLASS_DIR="${APP_ROOT}/target/classes"
LIB_DIR="${APP_ROOT}/lib"
MAIN_CLASS="com.addp.supermap.workflow.SuperMapWorkflowRuntime"
SUPERMAP_BIN="${SUPERMAP_OBJECTSJAVA_BIN:-/opt/supermap/objectsjava/bin_linux_arm64}"
GPA_LIB_DIR="${SUPERMAP_GPA_LIB_DIR:-/opt/supermap/gpa/libs}"
LICENSE_DIR="${SUPERMAP_LICENSE_DIR:-${APP_ROOT}/license}"
JAVA_OPTS="${SUPERMAP_JAVA_OPTS:--Xms128m -Xmx4g}"

if [ ! -d "${SUPERMAP_BIN}" ]; then
  echo "SUPERMAP_OBJECTSJAVA_BIN does not point to a directory: ${SUPERMAP_BIN}" >&2
  exit 1
fi
if [ ! -d "${GPA_LIB_DIR}" ]; then
  echo "SUPERMAP_GPA_LIB_DIR does not point to a directory: ${GPA_LIB_DIR}" >&2
  exit 1
fi

if ! find "${SUPERMAP_BIN}" -maxdepth 1 -type f -name "*.lic12" | grep -q .; then
  LICENSE_FILE="$(find "${LICENSE_DIR}" -maxdepth 1 -type f -name "*.lic12" 2>/dev/null | head -n 1 || true)"
  if [ -n "${LICENSE_FILE}" ]; then
    if cp "${LICENSE_FILE}" "${SUPERMAP_BIN}/"; then
      echo "Copied SuperMap license into ${SUPERMAP_BIN}"
    else
      echo "Warning: failed to copy SuperMap license from ${LICENSE_FILE} into ${SUPERMAP_BIN}" >&2
    fi
  fi
fi

export LD_LIBRARY_PATH="${SUPERMAP_BIN}:${SUPERMAP_BIN}/systemlibs:${LD_LIBRARY_PATH:-}"

mkdir -p "${CLASS_DIR}"

SOURCE_FILE="${SRC_DIR}/com/addp/supermap/workflow/SuperMapWorkflowRuntime.java"
if [ ! -f "${CLASS_DIR}/com/addp/supermap/workflow/SuperMapWorkflowRuntime.class" ] || [ "${SOURCE_FILE}" -nt "${CLASS_DIR}/com/addp/supermap/workflow/SuperMapWorkflowRuntime.class" ]; then
  javac \
    -encoding UTF-8 \
    -cp "${LIB_DIR}/*:${GPA_LIB_DIR}/*:${SUPERMAP_BIN}/*" \
    -d "${CLASS_DIR}" \
    "${SOURCE_FILE}"
fi

read -r -a java_opts <<< "${JAVA_OPTS}"

exec java \
  "${java_opts[@]}" \
  -Djava.library.path="${SUPERMAP_BIN}" \
  -cp "${CLASS_DIR}:${LIB_DIR}/*:${GPA_LIB_DIR}/*:${SUPERMAP_BIN}/*" \
  "${MAIN_CLASS}"
