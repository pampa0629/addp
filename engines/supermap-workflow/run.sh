#!/usr/bin/env bash
set -euo pipefail

APP_ROOT="${APP_ROOT:-/app}"
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

if [ ! -f "${CLASS_DIR}/com/addp/supermap/workflow/SuperMapWorkflowRuntime.class" ]; then
  echo "Compiled SuperMap Workflow runtime class is missing" >&2
  exit 1
fi

# Objects Java cloud-license already embeds the Log4j SLF4J binding. PDT's full GPA lib set also
# carries alternate logging stacks and TLog converters that collide when loaded into this runtime.
gpa_classpath=""
for jar in "${GPA_LIB_DIR}"/*.jar; do
  jar_name="$(basename "${jar}")"
  case "${jar_name}" in
    logback-*.jar|logstash-logback-*.jar|tlog-*.jar|log4j-slf4j-impl-*.jar)
      continue
      ;;
  esac
  gpa_classpath="${gpa_classpath:+${gpa_classpath}:}${jar}"
done
if [ -z "${gpa_classpath}" ]; then
  echo "No GPA runtime jars found in ${GPA_LIB_DIR}" >&2
  exit 1
fi

read -r -a java_opts <<< "${JAVA_OPTS}"

exec java \
  "${java_opts[@]}" \
  -Djava.library.path="${SUPERMAP_BIN}" \
  -cp "${CLASS_DIR}:${LIB_DIR}/*:${gpa_classpath}:${SUPERMAP_BIN}/*" \
  "${MAIN_CLASS}"
