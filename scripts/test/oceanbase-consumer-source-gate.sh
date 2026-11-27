#!/usr/bin/env bash
# Check the Hosted source contract independently on one disposable database instance.
set -euo pipefail
source business/scripts/online-oceanbase-consumer-fixture.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/ob-source.XXXXXXXX")
CONTAINER="addp-ob-source-${work##*.}"
export ADDP_ONLINE_SECRET_DIR="$work" ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE="$work/source.json"
export OB_TENANT_PASSWORD
OB_TENANT_PASSWORD=$(openssl rand -hex 32)
container_id=""
cleanup() {
  local status=$?
  trap - EXIT
  [ -z "$container_id" ] || docker rm -fv "$container_id" >/dev/null || status=1
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT
container_id=$(docker run -d --name "$CONTAINER" -p 127.0.0.1::2881 -e MODE=mini -e OB_TENANT_NAME=test \
  -e OB_DATABASE=oceanbase -e OB_TENANT_PASSWORD "$IMAGE")
for _ in $(seq 1 180); do
  if admin_obclient -Nse 'SELECT 1' >/dev/null 2>&1; then break; fi
  sleep 2
done
reset_fixture
create_future
advance_fixture
export MYSQL_PWD
MYSQL_PWD=$(python3 -c 'import sys,json;print(json.load(open(sys.argv[1]))["connection_info"]["password"])' "$ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE")
source_value() {
  docker exec -e MYSQL_PWD "$container_id" obclient -h127.0.0.1 -P2881 -uob_consumer@test -Nse "$1"
}
before=$(audit_position)
[ "$(source_value 'SELECT id FROM sales.node_existing')" = 37 ]
[ "$(source_value 'SELECT id FROM sales.node_future')" = 41 ]
[ "$(source_value 'SELECT id FROM outside_sales.node_future')" = 83 ]
reads=$(count_reads "$before")
[ "$reads" = 3 ] || fail "three real fixture reads counted as $reads"
[ "$(count_reads "$(audit_position)")" = 0 ] || fail "empty observation window counted business reads"
echo 'OceanBase disposable source passed: current, future and sibling values, native audit delta 3 and incremental fixture'
