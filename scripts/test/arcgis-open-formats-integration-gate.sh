#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GEOPYTHON_CONTAINER="${GEOPYTHON_CONTAINER:-geopython-workflow-engine}"
PGEO_FIXTURE="${ADDP_ARCGIS_PGEO_FIXTURE:-${ROOT_DIR}/business/nfs/data/arcgis/AggDB_1.2015.1_Data/AggDB_v1.2015.1.mdb}"
ACCESS_FIXTURE="${ADDP_ARCGIS_ACCESS_FIXTURE:-${ROOT_DIR}/business/nfs/data/arcgis/gdal_numeric_access.mdb}"
MATRIX_FIXTURE="${ADDP_ARCGIS_PGEO_MATRIX_FIXTURE:-${ROOT_DIR}/business/nfs/data/arcgis/idaho_dgm7/swgmgdb_id_igs.mdb}"

fail() {
  echo "ArcGIS open formats integration gate failed: $*" >&2
  exit 1
}

for fixture in "$PGEO_FIXTURE" "$ACCESS_FIXTURE" "$MATRIX_FIXTURE"; do
  [[ -f "$fixture" ]] || fail "fixture does not exist: $fixture"
done

docker inspect "$GEOPYTHON_CONTAINER" >/dev/null 2>&1 || fail "GeoPython container is unavailable: $GEOPYTHON_CONTAINER"
[[ "$(docker inspect -f '{{.State.Running}}' "$GEOPYTHON_CONTAINER")" == "true" ]] || fail "GeoPython container is not running: $GEOPYTHON_CONTAINER"

for fixture in "$PGEO_FIXTURE" "$ACCESS_FIXTURE" "$MATRIX_FIXTURE"; do
  docker exec "$GEOPYTHON_CONTAINER" test -f "$fixture" || fail "fixture is not mounted into GeoPython container: $fixture"
done

echo "[1/4] GeoPython real MDB identity and PGeo data-plane acceptance"
docker exec -i \
  -e ADDP_ARCGIS_RUNTIME_ONLINE=1 \
  -e ADDP_ARCGIS_PGEO_FIXTURE="$PGEO_FIXTURE" \
  -e ADDP_ARCGIS_ACCESS_FIXTURE="$ACCESS_FIXTURE" \
  -e ADDP_ARCGIS_PGEO_MATRIX_FIXTURE="$MATRIX_FIXTURE" \
  "$GEOPYTHON_CONTAINER" python - \
  < "$ROOT_DIR/engines/geopython-workflow/test_gdal_vector_dataset_online.py"

echo "[2/4] Transfer PGeo Point to Oracle Spatial bounded acceptance"
(
  cd "$ROOT_DIR/transfer/backend"
  ADDP_TRANSFER_PGEO_ORACLE_BOUNDED_E2E=1 \
  ADDP_ARCGIS_PGEO_FIXTURE="$PGEO_FIXTURE" \
  go test ./internal/executor -run '^TestIntegrationTransferPGeoToOracleSpatial$' -count=1 -v
  echo "[3/4] Transfer PGeo MultiPolygon to Oracle Spatial bounded acceptance"
  ADDP_TRANSFER_PGEO_ORACLE_MATRIX_E2E=1 \
  ADDP_ARCGIS_PGEO_MATRIX_FIXTURE="$MATRIX_FIXTURE" \
  go test ./internal/executor -run '^TestIntegrationTransferPGeoGeometryMatrixToOracleSpatial$' -count=1 -v
  echo "[4/4] Transfer Oracle Spatial to FileGDB round-trip bounded acceptance"
  ADDP_TRANSFER_ORACLE_FILEGDB_ROUNDTRIP_E2E=1 \
  ADDP_ARCGIS_PGEO_MATRIX_FIXTURE="$MATRIX_FIXTURE" \
  go test ./internal/executor -run '^TestIntegrationTransferOracleSpatialToFileGDBRoundTrip$' -count=1 -v
)

echo "ArcGIS open formats integration gate passed"
