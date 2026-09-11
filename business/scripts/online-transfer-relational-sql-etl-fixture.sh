#!/usr/bin/env bash
# Lifecycle owner for the PostgreSQL -> PostgreSQL relational SQL ETL T4 fixture.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)

SOURCE_TABLE=addp_online_transfer_sql_etl_source
TARGET_TABLE=addp_online_transfer_sql_etl_target

fail() {
  echo "Online Transfer relational SQL ETL fixture failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_HOST:-}" = "1" ] || fail "ADDP_ONLINE_HOST must be exactly 1"
[ "$(uname -s)" = "Darwin" ] || fail "the Online Transfer relational SQL ETL fixture requires macOS"

action=${1:-}
case "$action" in
  start|verify|stop|status) ;;
  *) fail "usage: bash business/scripts/online-transfer-relational-sql-etl-fixture.sh start|verify|stop|status" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"

required=(
  ADDP_ONLINE_TEST_ENGINE_USER
  ADDP_ONLINE_TEST_ENGINE_DATABASE
)
for variable in "${required[@]}"; do
  [ -n "${!variable:-}" ] || fail "$variable is required"
done

postgres_running() {
  [ "$(docker inspect --format '{{.State.Running}}' business-postgres 2>/dev/null || true)" = "true" ]
}

postgres_sql() {
  docker exec business-postgres psql \
    -v ON_ERROR_STOP=1 \
    -U "$ADDP_ONLINE_TEST_ENGINE_USER" \
    -d "$ADDP_ONLINE_TEST_ENGINE_DATABASE" "$@"
}

reset_fixture() {
  postgres_sql <<SQL >/dev/null
DROP TABLE IF EXISTS public.${TARGET_TABLE};
DROP TABLE IF EXISTS public.${SOURCE_TABLE};
CREATE TABLE public.${SOURCE_TABLE} (
  id bigint PRIMARY KEY,
  region varchar(32) NOT NULL,
  status varchar(16) NOT NULL,
  amount numeric(10,2) NOT NULL,
  internal_note varchar(64) NOT NULL
);
INSERT INTO public.${SOURCE_TABLE} (id, region, status, amount, internal_note) VALUES
  (1, 'north', 'active', 10.50, 'hidden-1'),
  (2, 'south', 'inactive', 200.00, 'hidden-2'),
  (3, 'north', 'active', 300.25, 'hidden-3'),
  (4, 'east', 'active', 400.75, 'hidden-4'),
  (5, 'west', 'inactive', 500.00, 'hidden-5');
SQL
}

verify_fixture() {
  local values columns
  values=$(postgres_sql -Atc \
    "SELECT CONCAT_WS('|', COUNT(*), MIN(id), MAX(id), SUM(amount)::numeric(12,2)) FROM public.${TARGET_TABLE}")
  [ "$values" = "2|3|4|701.00" ] || fail "target rows do not prove SQL filter semantics: $values"
  columns=$(postgres_sql -Atc \
    "SELECT string_agg(column_name, ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = '${TARGET_TABLE}'")
  [ "$columns" = "id,region,amount" ] || fail "target columns do not prove SQL projection semantics: $columns"
}

case "$action" in
  start)
    bash "$SCRIPT_DIR/online-engine-fixture.sh" start
    postgres_running || fail "business-postgres is not running"
    reset_fixture
    echo "Online Transfer relational SQL ETL fixture is ready"
    ;;
  verify)
    postgres_running || fail "business-postgres is not running"
    verify_fixture
    echo "Online Transfer relational SQL ETL target is verified"
    ;;
  stop)
    if postgres_running; then
      postgres_sql -c "DROP TABLE IF EXISTS public.${TARGET_TABLE}; DROP TABLE IF EXISTS public.${SOURCE_TABLE}" >/dev/null
    fi
    bash "$SCRIPT_DIR/online-engine-fixture.sh" stop
    echo "Online Transfer relational SQL ETL fixture is stopped"
    ;;
  status)
    postgres_running || fail "business-postgres is not running"
    postgres_sql -Atc "SELECT COUNT(*) FROM public.${SOURCE_TABLE}" | grep -qx '5' || fail "source table is not ready"
    echo "Online Transfer relational SQL ETL fixture is ready"
    ;;
esac
