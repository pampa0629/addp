#!/usr/bin/env bash
# Shared deployment preparation; source from lifecycle owners before Compose.
addp_prepare_runtime_log_env() {
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
    python3 - "$runtime_env_file" <<'PYLOGENV' || return 1
from pathlib import Path
import os, re, secrets, socket, sys, tempfile
p = Path(sys.argv[1])
s = p.read_text() if p.exists() else ''
updates = {}
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
}
