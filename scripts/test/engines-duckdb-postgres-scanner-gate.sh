#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=postgres
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.spark-federation-t2.yml
# ADDP_T2_INPUT_FILES=engines/duckdb/ scripts/dev/start.sh scripts/dev/build-identity.sh scripts/test/docker-compose.spark-federation-t2.yml
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
module="$root/engines/duckdb"
work=$(mktemp -d "${TMPDIR:-/tmp}/addp-duckdb-native.XXXXXXXX")
trap 'rm -rf "$work"' EXIT
cache=${ADDP_DUCKDB_BUILD_CACHE:-"$root/.cache/duckdb/native-build"}
digest=$("$module/postgres-scanner/build.sh" "$cache" "$work")
# Run against a disposable read-only source, on an ephemeral loopback port.
project=$(printf 'duckdb-scanner-%s' "${work##*.}" | tr '[:upper:]' '[:lower:]')
cat > "$work/port.yml" <<'PORTS'
services:
  postgres:
    ports: !override
      - "127.0.0.1::5432"
PORTS
compose=(docker compose -p "$project" -f "$root/scripts/test/docker-compose.spark-federation-t2.yml" -f "$work/port.yml")
cleanup() {
  "${compose[@]}" down --volumes --remove-orphans && rm -rf "$work"
}
trap 'rc=$?; cleanup || exit 1; exit "$rc"' EXIT
"${compose[@]}" up -d --no-build --wait postgres
"${compose[@]}" exec -T postgres psql -Xq -v ON_ERROR_STOP=1 -U fixture_admin -d addp_test <<'SQL'
CREATE ROLE scanner_reader LOGIN PASSWORD 'fixture_reader';
CREATE TABLE public.parcels (id integer);
CREATE TABLE public.subparcels () INHERITS (public.parcels);
INSERT INTO public.parcels VALUES (1);
INSERT INTO public.subparcels VALUES (2);
GRANT SELECT ON public.parcels, public.subparcels TO scanner_reader;
SQL
port=$("${compose[@]}" port postgres 5432)
export ADDP_TEST_DUCKDB_SCANNER_PORT=${port##*:}
cd "$module"
GOWORK=off ADDP_TEST_PINNED_DUCKDB_SCANNER=1 DUCKDB_EXTENSION_DIRECTORY="$work" \
  go test -v -count=1 -ldflags "-X github.com/addp/engines/duckdb/internal/duckdb.postgresScannerSHA256=$digest" ./internal/duckdb
