#!/usr/bin/env bash
# infra-port-resolution.sh - Check ADDP Infra port ownership and collision resolution without starting containers.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../infra/ports.sh"

MOCK_PRESENT=0
MOCK_FOREIGN=0
MOCK_EXTRA_BUSY=''
unset ADDP_ONLINE_HOST

mock_service() {
  case "$1" in
    addp-postgres) echo postgres ;;
    addp-redis) echo redis ;;
    addp-falkordb) echo falkordb ;;
    addp-minio) echo minio ;;
    addp-meilisearch) echo meilisearch ;;
    addp-redpanda) echo redpanda ;;
    addp-kafka-connect) echo kafka-connect ;;
    *) return 1 ;;
  esac
}

docker() {
  local container format='' service
  case "$1" in
    inspect)
      if [ "${2:-}" = --format ]; then
        format="$3"
        container="$4"
      else
        container="$2"
      fi
      [ "$MOCK_PRESENT" = 1 ] || return 1
      service=$(mock_service "$container") || return 1
      case "$format" in
        *com.docker.compose.project*)
          if [ "$MOCK_FOREIGN" = 1 ] && [ "$container" = addp-postgres ]; then
            echo "scp|${service}|${SCRIPT_DIR}/../.."
          else
            echo "addp-infra|${service}|$(cd "${SCRIPT_DIR}/../.." && pwd)"
          fi
          ;;
        *State.Running*)
          if [[ "$format" == *State.Health* ]]; then echo 'true:healthy'; else echo true; fi
          ;;
      esac
      ;;
    port)
      [ "$MOCK_PRESENT" = 1 ] || return 1
      case "$2:$3" in
        addp-postgres:5432/tcp) echo '0.0.0.0:25432' ;;
        addp-redis:6379/tcp) echo '0.0.0.0:26379' ;;
        addp-falkordb:6379/tcp) echo '127.0.0.1:16479' ;;
        addp-minio:9000/tcp) echo '0.0.0.0:19000' ;;
        addp-minio:9001/tcp) echo '0.0.0.0:19001' ;;
        addp-meilisearch:7700/tcp) echo '0.0.0.0:17700' ;;
        addp-redpanda:9092/tcp) echo '0.0.0.0:19092' ;;
        addp-kafka-connect:8083/tcp) echo '0.0.0.0:18083' ;;
        *) return 1 ;;
      esac
      ;;
    *) return 1 ;;
  esac
}

lsof() {
  local argument port=''
  for argument in "$@"; do
    case "$argument" in -iTCP:*) port="${argument#-iTCP:}" ;; esac
  done
  case " 15432 16379 ${MOCK_EXTRA_BUSY} " in
    *" ${port} "*) return 0 ;;
    *) return 1 ;;
  esac
}

POSTGRES_PORT=15432
REDIS_PORT=16379
addp_infra_resolve_ports >/dev/null
[ "$POSTGRES_PORT" = 25432 ]
[ "$REDIS_PORT" = 26379 ]
[ "$MEILISEARCH_PORT" = 17700 ]
[ "$INFRA_KAFKA_BOOTSTRAP_SERVERS" = localhost:19092 ]
if addp_infra_ready; then
  echo 'missing containers were incorrectly considered ready' >&2
  exit 1
fi

ADDP_ONLINE_HOST=1
POSTGRES_PORT=15432
if addp_infra_resolve_ports >/dev/null 2>&1; then
  echo 'online runner changed its explicit Infra port' >&2
  exit 1
fi
unset ADDP_ONLINE_HOST

MOCK_EXTRA_BUSY='16479 19001 19092'
POSTGRES_PORT=15432
REDIS_PORT=16379
FALKORDB_PORT=16479
MINIO_CONSOLE_PORT=19001
INFRA_KAFKA_PORT=19092
addp_infra_resolve_ports >/dev/null
[ "$FALKORDB_PORT" = 26479 ]
[ "$MINIO_API_PORT" = 19000 ]
[ "$MINIO_CONSOLE_PORT" = 29001 ]
[ "$INFRA_KAFKA_PORT" = 29093 ]
[ "$INFRA_FALKORDB_ADDRESS" = 127.0.0.1:26479 ]
[ "$INFRA_KAFKA_BOOTSTRAP_SERVERS" = localhost:29093 ]
MOCK_EXTRA_BUSY=''

MOCK_PRESENT=1
POSTGRES_PORT=15432
REDIS_PORT=16379
addp_infra_resolve_ports >/dev/null
[ "$POSTGRES_PORT" = 25432 ]
[ "$REDIS_PORT" = 26379 ]
addp_infra_read_actual_ports
[ "$POSTGRES_PORT" = 25432 ]
[ "$REDIS_PORT" = 26379 ]
addp_infra_ready

MOCK_FOREIGN=1
if addp_infra_resolve_ports >/dev/null 2>&1; then
  echo 'foreign container was incorrectly accepted as ADDP Infra' >&2
  exit 1
fi
if addp_infra_ready; then
  echo 'foreign container was incorrectly considered ready' >&2
  exit 1
fi

echo 'ADDP Infra port resolution OK'

# The lifecycle entry must never erase local volumes from a non-interactive
# worktree cleanup, even when --force was supplied by an automation wrapper.
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT
mkdir -p "$TEST_DIR/bin" "$TEST_DIR/repo/scripts/infra"
cp "${SCRIPT_DIR}/../infra/down.sh" "$TEST_DIR/repo/scripts/infra/down.sh"
cat > "$TEST_DIR/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  compose)
    if [[ "$2" == version ]]; then exit 0; fi
    [[ "$2" == -f && "$3" == docker-compose.infra.yml ]] || exit 2
    case "$4" in
      ps) echo addp-postgres ;;
      down) printf '%s\n' "${5:-down}" > "$MOCK_DOWN_MARKER" ;;
      *) exit 2 ;;
    esac
    ;;
  inspect)
    printf '%s\n' "$MOCK_WORKING_DIR"
    ;;
  *) exit 2 ;;
esac
EOF
chmod +x "$TEST_DIR/bin/docker"
DOWN_SCRIPT="$TEST_DIR/repo/scripts/infra/down.sh"
MOCK_MARKER="$TEST_DIR/down-called"

if env -u GITHUB_ACTIONS -u ADDP_ONLINE_HOST -u ADDP_ONLINE_TEST \
  PATH="$TEST_DIR/bin:$PATH" MOCK_DOWN_MARKER="$MOCK_MARKER" \
  MOCK_WORKING_DIR="$TEST_DIR/repo" \
  bash "$DOWN_SCRIPT" --volumes --force </dev/null >"$TEST_DIR/local.log" 2>&1; then
  echo 'non-interactive local volume deletion was accepted' >&2
  exit 1
fi
[ ! -e "$MOCK_MARKER" ]

if env PATH="$TEST_DIR/bin:$PATH" MOCK_DOWN_MARKER="$MOCK_MARKER" \
  MOCK_WORKING_DIR="$TEST_DIR/foreign" \
  bash "$DOWN_SCRIPT" --force </dev/null >"$TEST_DIR/foreign.log" 2>&1; then
  echo 'foreign-worktree container removal was accepted' >&2
  exit 1
fi
[ ! -e "$MOCK_MARKER" ]

env GITHUB_ACTIONS=true ADDP_ONLINE_HOST=1 ADDP_ONLINE_TEST=1 \
  ADDP_ONLINE_HOSTED=1 POSTGRES_DB=addp_online \
  PATH="$TEST_DIR/bin:$PATH" MOCK_DOWN_MARKER="$MOCK_MARKER" \
  MOCK_WORKING_DIR="$TEST_DIR/repo" \
  bash "$DOWN_SCRIPT" --volumes --force </dev/null >"$TEST_DIR/online.log" 2>&1
[ "$(cat "$MOCK_MARKER")" = -v ]

echo 'ADDP Infra down safety OK'
