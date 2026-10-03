#!/usr/bin/env bash
# Business owns the disposable physical Redis; System owns its Engine registration.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
CONTAINER=addp-redis-online-disposable
LABEL=com.addp.online-fixture
SUITE=redis-consumer-flow
fail() { echo "Online Redis fixture failed: $*" >&2; exit 1; }
[ "${GITHUB_ACTIONS:-}" = true ] && [ "${RUNNER_OS:-}" = Linux ] &&
  [ "${ADDP_ONLINE_HOST:-}" = 1 ] && [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] &&
  [ "${ADDP_ONLINE_OWNER_MANAGED:-0}" != 1 ] &&
  [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || fail 'requires GitHub Hosted Linux x86_64'
[ "$#" -eq 1 ] || fail 'usage: start|stop'
owned() {
  [ "$(docker container inspect --format "{{index .Config.Labels \"$LABEL\"}}" "$CONTAINER")" = "$SUITE" ]
}
stop() {
  if docker container inspect "$CONTAINER" >/dev/null 2>&1; then
    owned || fail 'refusing to delete a foreign container'
    docker rm -fv "$CONTAINER" >/dev/null
  fi
  local remaining
  remaining=$(docker ps -aq --filter "label=$LABEL=$SUITE") || fail 'cannot verify fixture cleanup'
  [ -z "$remaining" ] || fail 'fixture containers remain'
}
case "$1" in
  stop) stop; exit 0 ;;
  start) ;;
  *) fail 'usage: start|stop' ;;
esac
if docker container inspect "$CONTAINER" >/dev/null 2>&1; then fail 'refusing to reuse an existing container'; fi
# A descriptor contains the reader credential and must stay in the lifecycle's secret partition.
python3 - <<'PY'
import os, stat
from pathlib import Path
root = Path(os.environ['ADDP_ONLINE_SECRET_DIR']).resolve(strict=True)
output = Path(os.environ['ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE'])
if not output.is_absolute() or output.parent.resolve() != root or output.exists() or output.is_symlink():
    raise SystemExit('descriptor must be a new file directly inside the secret directory')
if stat.S_IMODE(root.stat().st_mode) != 0o700:
    raise SystemExit('secret directory must have mode 0700')
PY
export BUSINESS_REDIS_ADMIN_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_urlsafe(32))')
export BUSINESS_REDIS_READER_PASSWORD=$(python3 -c 'import secrets; print(secrets.token_urlsafe(32))')
IMAGE=$(docker compose --env-file "$ROOT_DIR/business/.env.example" -f "$ROOT_DIR/business/docker-compose.yml" config --format json |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["services"]["redis"]["image"])')
[[ "$IMAGE" =~ ^public\.ecr\.aws/docker/library/redis:[0-9.]+@sha256:[a-f0-9]{64}$ ]] || fail 'Redis image must be fixed to the official digest'
created=0
finish_start() {
  local status=$?
  trap - EXIT INT TERM
  if [ "$status" -ne 0 ] && [ "$created" -eq 1 ]; then stop || status=1; fi
  unset BUSINESS_REDIS_ADMIN_PASSWORD BUSINESS_REDIS_READER_PASSWORD
  exit "$status"
}
trap finish_start EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Claim cleanup before run: Docker can create the container and then fail to start it.
created=1
docker run -d --name "$CONTAINER" --label "$LABEL=$SUITE" \
  --publish '127.0.0.1::6379' --env BUSINESS_REDIS_ADMIN_PASSWORD --env BUSINESS_REDIS_READER_PASSWORD \
  --mount "type=bind,src=$ROOT_DIR/business/redis,dst=/addp/redis,readonly" \
  --tmpfs /run/addp-redis:mode=0700 --tmpfs /data:mode=1777 --memory 512m \
  "$IMAGE" sh /addp/redis/start.sh >/dev/null
ready=0
for attempt in $(seq 1 60); do
  if docker exec "$CONTAINER" sh -c 'REDISCLI_AUTH="$BUSINESS_REDIS_ADMIN_PASSWORD" redis-cli --user addp_business_admin -e ping' 2>/dev/null | grep -qx PONG; then ready=1; break; fi
  sleep 1
done
[ "$ready" -eq 1 ] || fail 'Redis readiness timed out'
docker exec "$CONTAINER" sh /addp/redis/init.sh >/dev/null
export REDIS_FIXTURE_ENDPOINT=$(docker port "$CONTAINER" 6379/tcp)
python3 - <<'PY'
import json, os, re
endpoint = os.environ['REDIS_FIXTURE_ENDPOINT']
match = re.fullmatch(r'127\.0\.0\.1:([0-9]+)', endpoint)
if not match or not 1024 <= int(match[1]) <= 65535:
    raise SystemExit('fixture must expose one random loopback port')
value = {'name': 'Hosted Redis', 'engine_type': 'redis', 'engine_origin': 'general',
         'description': 'Disposable Business Redis native samples for Online acceptance',
         'connection_info': {'host': '127.0.0.1', 'port': int(match[1]), 'user': 'addp_business_reader',
                             'password': os.environ['BUSINESS_REDIS_READER_PASSWORD'], 'database': 0, 'use_ssl': False}}
fd = os.open(os.environ['ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE'], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as stream:
    json.dump(value, stream)
PY
echo 'Disposable Business Redis samples ready'
