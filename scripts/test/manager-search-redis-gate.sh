#!/usr/bin/env bash
# Run the Manager gate with a private, memory-only checkpoint cache.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
owner=$(python3 -c 'import uuid; print(uuid.uuid4().hex)')
project="addp-manager-search-$owner"
export MANAGER_PG_TEST_OWNER="$owner" MANAGER_PG_TEST_IMAGE="addp-manager-unused:$owner"
export MANAGER_SEARCH_REDIS_PASSWORD
MANAGER_SEARCH_REDIS_PASSWORD=$(openssl rand -hex 32)
compose() {
  docker compose --env-file /dev/null -p "$project" -f "$root/scripts/test/docker-compose.manager-postgres-t2.yml" "$@"
}
cleanup() {
  local result=$?
  trap - EXIT
  compose down --volumes --timeout 3 >/dev/null || result=1
  for kind in container volume network; do
    if [[ -n "$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$project")" ]]; then result=1; fi
  done
  exit "$result"
}
trap cleanup EXIT
compose up -d --wait --wait-timeout 60 manager-search-redis
export MANAGER_SEARCH_REDIS_HOST=127.0.0.1
redis_cli() { compose exec -T manager-search-redis sh -ec 'REDISCLI_AUTH="$MANAGER_SEARCH_REDIS_PASSWORD" exec redis-cli --user manager_search --raw "$@"' sh "$@"; }
[[ "$(redis_cli SET manager:search:probe numeric EX 300)" == OK ]]
[[ "$(redis_cli GET manager:search:probe)" == numeric ]]
for denied in 'GET other-module' 'SAVE' 'BGSAVE' 'CONFIG GET save'; do
  read -ra args <<< "$denied"
  [[ "$(redis_cli "${args[@]}")" == NOPERM* ]] || { echo "Search cache accepted disallowed operation: ${args[0]}" >&2; exit 1; }
done
[[ "$(compose exec -T manager-search-redis redis-cli --raw GET manager:search:probe)" == NOAUTH* ]]
# This is the gate's own isolated service. A restart must lose all search state.
compose restart manager-search-redis >/dev/null
compose up -d --wait --wait-timeout 60 manager-search-redis >/dev/null
[[ -z "$(redis_cli GET manager:search:probe)" ]]
echo 'Manager private Redis: authenticated scope, denied foreign key/persistence commands, restart discarded temporary state.'
mapping=$(compose port manager-search-redis 6379)
export MANAGER_SEARCH_REDIS_PORT="${mapping##*:}"
"$@"
