#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MODE=${1:-all}
[[ "$MODE" = all || "$MODE" = --runtime-only ]] || { echo 'usage: ArcGIS gate [--runtime-only]' >&2; exit 1; }
[[ $# -le 1 ]] || exit 1
GEOPYTHON_WORKFLOW_PYTHON=${GEOPYTHON_WORKFLOW_PYTHON:-$ROOT_DIR/engines/geopython-workflow/venv/bin/python}
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

source "$ROOT_DIR/scripts/dev/geopython-workflow.sh"
addp_geopython_native_environment || fail "native GDAL environment unavailable"
[[ -x "$GEOPYTHON_WORKFLOW_PYTHON" ]] || fail "native Python environment unavailable"

echo "[1/4] GeoPython native real MDB identity and PGeo data-plane acceptance"
(
  cd "$ROOT_DIR/engines/geopython-workflow"
  ADDP_ARCGIS_RUNTIME_ONLINE=1 \
  ADDP_ARCGIS_PGEO_FIXTURE="$PGEO_FIXTURE" \
  ADDP_ARCGIS_ACCESS_FIXTURE="$ACCESS_FIXTURE" \
  ADDP_ARCGIS_PGEO_MATRIX_FIXTURE="$MATRIX_FIXTURE" \
  "$GEOPYTHON_WORKFLOW_PYTHON" test_gdal_vector_dataset_online.py
)
if [[ "$MODE" = --runtime-only ]]; then
  echo 'ArcGIS native Runtime sample acceptance passed; Oracle round-trip not run'
  exit 0
fi

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
