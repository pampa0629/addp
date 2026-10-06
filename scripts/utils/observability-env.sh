#!/usr/bin/env bash
# Shared deployment preparation; source from lifecycle owners before Compose.
addp_runtime_logs_enabled() {
  [[ "${ADDP_OBSERVABILITY_LOGS_ENABLED-true}" == true ]]
}

addp_runtime_log_preflight() {
  case "${ADDP_OBSERVABILITY_LOGS_ENABLED-true}" in
    false) return 0 ;;
    true) ;;
    *) echo 'ADDP_OBSERVABILITY_LOGS_ENABLED must be true or false' >&2; return 1 ;;
  esac
  local name value
  for name in LOKI_READ_TOKEN LOKI_WRITE_TOKEN LOKI_S3_SECRET_KEY; do
    value="${!name:-}"
    if [[ ! "$value" =~ ^[a-zA-Z0-9_-]{32,128}$ ]]; then
      echo "Invalid runtime log secret: $name" >&2
      return 1
    fi
  done
  [[ "${LOG_OBSERVER_SERVICE_CLIENT_SECRET:-}" =~ ^[a-zA-Z0-9_-]{32,72}$ ]] || {
    echo 'Invalid runtime log secret: LOG_OBSERVER_SERVICE_CLIENT_SECRET' >&2; return 1;
  }
  while IFS= read -r name; do
    [[ "$name" == LOG_OBSERVER_SERVICE_CLIENT_SECRET ]] && continue
    if [[ "${!name}" == "$LOG_OBSERVER_SERVICE_CLIENT_SECRET" ]]; then
      echo 'Runtime log observer must use an independent OAuth client secret' >&2; return 1
    fi
  done < <(compgen -e | sed -n '/_SERVICE_CLIENT_SECRET$/p')
  [[ -n "${LOKI_S3_ACCESS_KEY:-}" ]] || { echo 'Runtime log account is missing' >&2; return 1; }
  [[ "$LOKI_READ_TOKEN" != "$LOKI_WRITE_TOKEN" ]] || {
    echo 'Runtime log read/write tokens must be different' >&2; return 1;
  }
}

addp_observability_profiles() {
  # Explicit services are used by lifecycle owners; arbitrary COMPOSE_PROFILES
  # must not activate an unselected capability.
  unset COMPOSE_PROFILES
  local profiles=()
  if addp_runtime_logs_enabled; then profiles+=(observability-logs); fi
  if addp_metrics_enabled; then profiles+=(observability-metrics); fi
  if (( ${#profiles[@]} )); then
    local IFS=,
    export COMPOSE_PROFILES="${profiles[*]}"
  fi
}

addp_prepare_observability_env() {
  local runtime_env_file=$1
  if [[ "${ADDP_ONLINE_HOST:-0}" == 1 ]]; then
    python3 - "$PROJECT_ROOT" "$runtime_env_file" <<'PYBOUNDARY' || return 1
from pathlib import Path
import os, sys
root = Path(sys.argv[1]).resolve()
p = Path(sys.argv[2])
if not p.is_absolute() or p.resolve().is_relative_to(root):
    raise SystemExit('ADDP_ONLINE_ENV_FILE must be absolute and outside the repository')
artifact = os.environ.get('ADDP_ONLINE_ARTIFACT_DIR')
if artifact and p.resolve().is_relative_to(Path(artifact).resolve()):
    raise SystemExit('ADDP_ONLINE_ENV_FILE must not be inside artifacts')
if os.path.lexists(root / '.env'):
    raise SystemExit('Online lifecycle forbids a repository root .env')
PYBOUNDARY
  fi
  if [[ -f "$runtime_env_file" ]]; then
    set -a
    source "$runtime_env_file" || return 1
    set +a
  fi
  if [[ "${ENV:-development}" != production ]]; then
    python3 - "$runtime_env_file" <<'PYLOGENV' || echo 'Runtime log configuration preparation failed; core startup remains independent' >&2
from pathlib import Path
import os, re, secrets, socket, sys, tempfile
p = Path(sys.argv[1])
s = p.read_text() if p.exists() else ''
updates = {}
if os.environ.get('ADDP_OBSERVABILITY_LOGS_ENABLED', 'true') == 'true':
    for key in ('LOKI_READ_TOKEN', 'LOKI_WRITE_TOKEN', 'LOKI_S3_SECRET_KEY', 'LOG_OBSERVER_SERVICE_CLIENT_SECRET'):
        if not os.environ.get(key):
            updates[key] = secrets.token_hex(32)
    if not os.environ.get('LOKI_S3_ACCESS_KEY'):
        updates['LOKI_S3_ACCESS_KEY'] = 'addp-runtime-logs'
if not os.environ.get('ADDP_HOST_NODE_NAME'):
    node = socket.gethostname().split('.')[0]
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_.-]{0,99}', node):
        raise SystemExit('Set ADDP_HOST_NODE_NAME explicitly')
    updates['ADDP_HOST_NODE_NAME'] = node
for key, value in updates.items():
    pattern = re.compile(r'^' + key + r'=.*$', re.M)
    line = key + '=' + value
    s = pattern.sub(line, s) if pattern.search(s) else s.rstrip() + '\n' + line + '\n'
if updates:
    with tempfile.NamedTemporaryFile(mode='w', dir=p.parent, prefix='.runtime-log-', delete=False) as output:
        temporary = Path(output.name)
        try:
            os.chmod(temporary, 0o600)
            output.write(s)
            output.flush()
            os.fsync(output.fileno())
            temporary.replace(p)
        finally:
            temporary.unlink(missing_ok=True)
PYLOGENV
    if [[ -f "$runtime_env_file" ]]; then
      set -a
      source "$runtime_env_file" || return 1
      set +a
    fi
  fi
  # Host-mode producers, observer and pruner share one owner. Container-only
  # deployments can explicitly provide their producer's numeric UID:GID.
  export ADDP_RUNTIME_LOG_OWNER="${ADDP_RUNTIME_LOG_OWNER:-$(id -u):$(id -g)}"
  [[ "$ADDP_RUNTIME_LOG_OWNER" =~ ^[0-9]+:[0-9]+$ ]] || {
    echo 'ADDP_RUNTIME_LOG_OWNER must be numeric UID:GID' >&2
    return 1
  }
  addp_observability_profiles
}

addp_metrics_enabled() {
  [[ "${ADDP_OBSERVABILITY_METRICS_ENABLED-false}" == true ]]
}

addp_metrics_preflight() {
  case "${ADDP_OBSERVABILITY_METRICS_ENABLED-false}" in
    false) return 0 ;;
    true) ;;
    *) echo 'ADDP_OBSERVABILITY_METRICS_ENABLED must be true or false' >&2; return 1 ;;
  esac
  case "${ADDP_METRICS_TLS_DIR:-}" in
    /*) ;;
    *) echo 'ADDP_METRICS_TLS_DIR must be an absolute directory with deployment certificates' >&2; return 1 ;;
  esac
  local name
  for name in ca.crt server.crt server.key health.crt health.key; do
    [[ -f "$ADDP_METRICS_TLS_DIR/$name" && -r "$ADDP_METRICS_TLS_DIR/$name" ]] || {
      echo "Missing or unreadable metrics certificate file: $name" >&2; return 1;
    }
  done
  python3 "$PROJECT_ROOT/scripts/infra/generate-metrics-config.py"
}

addp_metrics_platform_preflight() {
  addp_metrics_enabled || return 1
  local name value
  for name in MONITOR_METRICS_CA_FILE MONITOR_METRICS_CLIENT_CERT_FILE MONITOR_METRICS_CLIENT_KEY_FILE; do
    value="${!name:-}"
    [[ "$value" == /* && -f "$value" && -r "$value" ]] || {
      echo "Missing absolute Monitor admission certificate file: $name" >&2; return 1;
    }
  done
}

# Query credentials have their own preflight and mount; never substitute health,
# admission or collector files when query credentials are absent.
addp_metrics_query_preflight() {
  addp_metrics_enabled || return 1
  [[ -n "${MONITOR_PROMETHEUS_URL:-}" ]] || return 1
  local name value
  for name in MONITOR_PROMETHEUS_CA_FILE MONITOR_PROMETHEUS_CLIENT_CERT_FILE MONITOR_PROMETHEUS_CLIENT_KEY_FILE; do
    value="${!name:-}"
    [[ "$value" == /* && -f "$value" && -r "$value" ]] || {
      echo "Missing absolute Monitor query certificate file: $name" >&2; return 1;
    }
  done
}
