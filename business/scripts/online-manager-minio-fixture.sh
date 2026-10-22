#!/usr/bin/env bash
# Lifecycle owner for the dedicated Business MinIO fixtures used by Manager T4 acceptance.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
BUSINESS_DIR=$(cd "${SCRIPT_DIR}/.." && pwd -P)
REPOSITORY_DIR=$(cd "${BUSINESS_DIR}/.." && pwd -P)
PPTX_FIXTURE_SOURCE="${REPOSITORY_DIR}/business/fixtures/manager/addp_online_preview_fixture.pptx"
HYBRID_SEARCH_IMAGE_FIXTURE_SOURCE="${REPOSITORY_DIR}/business/nfs/data/3d/stl/Print Light Gun/images/Autocop_4X3.jpg"
MC_IMAGE=addp-minio:RELEASE.2025-10-15T17-29-55Z
FORMAT_OBJECTS=(format-samples/vector-exchange/sites.kml format-samples/vector-exchange/sites.gml format-samples/vector-exchange/projected.topojson format-samples/gbk-quoted.csv format-samples/gbk-quoted.tsv format-samples/format-validation.xlsx format-samples/document.json format-samples/records.json format-samples/features.geojson format-samples/features.json format-samples/trajectory.csv format-samples/trajectory.json format-samples/trajectory.geojson)
MODEL_OBJECTS=(dae/model.dae dae/texture.png 3ds/model.3ds 3ds/texture.png ifc/model.ifc osgb/model.osgb skp/model.skp max/model.max max/texture.png fbx/model.fbx fbx/vCube.fbx fbx/LICENSE-three.txt obj/model.obj obj/model.mtl obj/texture.png)

fail() {
  echo "Online Manager MinIO fixture failed: $*" >&2
  exit 1
}

[ "${ADDP_ONLINE_HOST:-}" = "1" ] || fail "ADDP_ONLINE_HOST must be exactly 1"
if [ "${ADDP_ONLINE_HOSTED:-0}" = "1" ]; then
  [ "${GITHUB_ACTIONS:-}" = "true" ] && [ "${RUNNER_OS:-}" = "Linux" ] &&
    [ "$(uname -s)" = "Linux" ] && [ "$(uname -m)" = "x86_64" ] ||
    fail "Hosted Manager MinIO requires GitHub Linux x86_64"
  FORMAT_OBJECTS+=(index-source-versions/source.csv index-source-versions/source.tsv)
else
  [ "$(uname -s)" = "Darwin" ] || fail "the dedicated Online Manager MinIO fixture requires macOS"
fi

action=${1:-}
case "$action" in
  start|stop|status|index-csv-change|index-csv-restore|index-tsv-change|index-tsv-restore) ;;
  *) fail "unknown fixture action; use start|stop|status or a fixed Hosted index source change/restore action" ;;
esac
[ "$#" -eq 1 ] || fail "exactly one action is required"
if [[ "$action" = index-* ]]; then
  [ "${ADDP_ONLINE_HOSTED:-0}" = "1" ] || fail "index source mutation requires the exclusive Hosted fixture"
  [[ "${ADDP_ONLINE_TEST_RUN_ID:-}" =~ ^manager-[0-9]+-[0-9]+$ ]] || fail "index source mutation requires a Manager run identity"
  python3 - <<'PYOWNED'
import os, re
from pathlib import Path
state = Path(os.environ['ADDP_ONLINE_SECRET_DIR']) / 'nfs-root'
if state.is_symlink() or not state.is_file():
    raise SystemExit('owned Manager fixture state is missing')
root = Path(state.read_text().strip())
if not re.fullmatch(r'/tmp/addp-online-manager-nfs\.[A-Za-z0-9]{6}', str(root)) or root.is_symlink() or not root.is_dir():
    raise SystemExit('owned Manager fixture root is invalid')
owner = root / '.addp-online-owner'
if owner.is_symlink() or not owner.is_file() or owner.read_text().strip() != os.environ['ADDP_ONLINE_TEST_RUN_ID']:
    raise SystemExit('Manager fixture run ownership mismatch')
PYOWNED
fi

required=(
  ADDP_ONLINE_MANAGER_MINIO_PORT
  ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY
  ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY
  ADDP_ONLINE_MANAGER_MINIO_BUCKET
  ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT
  ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT
)
if [ "${ADDP_ONLINE_HOSTED:-0}" != "1" ]; then
  required+=(ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT)
fi
for variable in "${required[@]}"; do
  [ -n "${!variable:-}" ] || fail "$variable is required"
done
[[ "$ADDP_ONLINE_MANAGER_MINIO_PORT" =~ ^[0-9]+$ ]] || fail "ADDP_ONLINE_MANAGER_MINIO_PORT must be numeric"
[ "$ADDP_ONLINE_MANAGER_MINIO_PORT" -ge 1024 ] && [ "$ADDP_ONLINE_MANAGER_MINIO_PORT" -le 65535 ] ||
  fail "ADDP_ONLINE_MANAGER_MINIO_PORT must be between 1024 and 65535"
[[ "$ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY" =~ ^[A-Za-z0-9_.-]{3,64}$ ]] ||
  fail "ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY must contain 3-64 URL-safe characters"
[[ "$ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY" =~ ^[A-Za-z0-9_.-]{16,128}$ ]] ||
  fail "ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY must contain 16-128 URL-safe characters"
[[ "$ADDP_ONLINE_MANAGER_MINIO_BUCKET" =~ ^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$ ]] ||
  fail "ADDP_ONLINE_MANAGER_MINIO_BUCKET must be a valid S3 bucket name"

validate_object_key() {
  local variable=$1
  local expected_extension=$2
  local value=${!variable}
  local normalized
  [[ "$value" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]{0,255}$ ]] ||
    fail "$variable must be a safe object key"
  case "/$value/" in
    *"/../"*|*"/./"*) fail "$variable must not contain dot segments" ;;
  esac
  normalized=$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')
  case "$normalized" in
    *".$expected_extension") ;;
    *) fail "$variable must end with .$expected_extension" ;;
  esac
}

validate_object_key ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT las
validate_object_key ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT pptx
fixture_objects=("$ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT" "$ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT")
if [ "${ADDP_ONLINE_HOSTED:-0}" != "1" ]; then
  validate_object_key ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT jpg
  fixture_objects+=("$ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT")
  [ -f "$HYBRID_SEARCH_IMAGE_FIXTURE_SOURCE" ] || fail "fixture source is missing: $HYBRID_SEARCH_IMAGE_FIXTURE_SOURCE"
fi
fixture_object_count=$(printf '%s\n' "${fixture_objects[@]}" | sort -u | wc -l | tr -d ' ')
[ "$fixture_object_count" = "${#fixture_objects[@]}" ] || fail "Manager fixture object keys must be distinct"
[ -f "$PPTX_FIXTURE_SOURCE" ] || fail "fixture source is missing: $PPTX_FIXTURE_SOURCE"
for format in ifc osgb skp max 3ds; do
  source="${BUSINESS_DIR}/fixtures/manager/addp_online_model_fixture.${format}"
  [ -f "$source" ] || fail "fixture source is missing: $source"
done

[ -f "${REPOSITORY_DIR}/engines/model3d-workflow/tests/fixtures/mesh/box.fbx" ] || fail "FBX fixture source is missing"
[ -f "${REPOSITORY_DIR}/manager/frontend/e2e/fixtures/fbx/vCube.fbx" ] || fail "colored FBX fixture source is missing"
[ -f "${REPOSITORY_DIR}/manager/frontend/e2e/fixtures/fbx/LICENSE-three.txt" ] || fail "colored FBX fixture license is missing"
[ -f "${BUSINESS_DIR}/fixtures/manager/addp_online_max_texture.png" ] || fail "MAX texture fixture source is missing"
[ -f "${BUSINESS_DIR}/fixtures/manager/addp_online_exchange_texture.png" ] || fail "3DS texture fixture source is missing"

docker_fixture() {
  env \
    MINIO_API_PORT="$ADDP_ONLINE_MANAGER_MINIO_PORT" \
    MINIO_CONSOLE_PORT="${ADDP_ONLINE_MANAGER_MINIO_CONSOLE_PORT:-9003}" \
    MINIO_BIND_HOST="${MINIO_BIND_HOST:-127.0.0.1}" \
    MINIO_ROOT_USER="$ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY" \
    MINIO_ROOT_PASSWORD="$ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY" \
    docker "$@"
}

compose() {
  docker_fixture compose --env-file /dev/null -f "$BUSINESS_DIR/docker-compose.yml" "$@"
}

container_running() {
  [ "$(docker_fixture inspect --format '{{.State.Running}}' business-minio 2>/dev/null || true)" = "true" ]
}

validate_container_ownership() {
  if ! docker_fixture inspect business-minio >/dev/null 2>&1; then
    return 0
  fi
  local ownership
  ownership=$(docker_fixture inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}/{{ index .Config.Labels "com.docker.compose.service" }}' business-minio)
  [ "$ownership" = "business/minio" ] ||
    fail "business-minio is not owned by the business/minio Compose service"
}

minio_network() {
  docker_fixture inspect --format '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' business-minio
}

mc() {
  local network
  network=$(minio_network)
  [ -n "$network" ] || fail "business-minio has no Docker network"
  docker_fixture run --rm \
    --entrypoint mc \
    --network "$network" \
    -e "MC_HOST_fixture=http://${ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY}:${ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY}@business-minio:9000" \
    "$MC_IMAGE" "$@"
}

generate_fixtures() {
  python3 - "$1" "${BUSINESS_DIR}/fixtures/manager/addp_online_exchange_texture.png" <<'PY'
import csv
import io
import struct
import sys
import zipfile
import json
from pathlib import Path
from xml.sax.saxutils import escape

root = Path(sys.argv[1])
(root / 'index-source-versions').mkdir()
for suffix, delimiter in (('csv', ','), ('tsv', '\t')):
    (root / 'index-source-versions' / ('source.' + suffix)).write_bytes('id,name\n1,Alice\n2,Bobby\n'.replace(',', delimiter).encode('ascii'))
(root / "format-samples").mkdir()
rows = [["id", "姓名", "备注"]] + [[str(i), "张三" + str(i), f"北京,第{i}项\n第二行"] for i in range(25)]
for extension, delimiter in (("csv", ","), ("tsv", "\t")):
    text = io.StringIO(newline="")
    csv.writer(text, delimiter=delimiter, lineterminator="\r\n").writerows(rows)
    (root / "format-samples" / f"gbk-quoted.{extension}").write_bytes(text.getvalue().encode("gbk"))

# Fixed, uncompressed OOXML package: no timestamp or compressor variation in source hashes.
ns = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
rel = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
json_document = {"title": "格式验证", "settings": {"enabled": True, "threshold": None}, "values": [1, "中文", False]}
json_records = [{"id": i, "label": f"记录{i}"} for i in range(25)]
geojson_features = {"type": "FeatureCollection", "features": [
    {"type": "Feature", "properties": {"id": 1, "label": "采样点"}, "geometry": {"type": "Point", "coordinates": [120.0, 30.0]}},
    {"type": "Feature", "properties": {"id": 2, "label": "采样线"}, "geometry": {"type": "LineString", "coordinates": [[120.0, 30.0], [120.01, 30.02], [120.02, 30.0]]}},
]}
for name, value in [("document.json", json_document), ("records.json", json_records),
                    ("features.geojson", geojson_features), ("features.json", geojson_features)]:
    (root / "format-samples" / name).write_bytes((json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode('utf-8'))

# Fixed, self-contained dual-layer exchange files; no schema or texture downloads.
vector_exchange = {'sites.kml': '<?xml version="1.0"?><kml xmlns="http://www.opengis.net/kml/2.2"><Document>\n      <Folder><name>sites</name>\n       <Placemark><name>长沙</name><Point><coordinates>112.98,28.19,8</coordinates></Point></Placemark>\n       <Placemark><name>株洲</name><Point><coordinates>113.13,27.83,9</coordinates></Point></Placemark>\n      </Folder><Folder><name>roads</name><Placemark><name>route</name>\n       <LineString><coordinates>112.98,28.19 113.13,27.83</coordinates></LineString>\n      </Placemark></Folder></Document></kml>', 'sites.gml': '<gml:FeatureCollection xmlns:gml="http://www.opengis.net/gml" xmlns:app="urn:addp:test">\n      <gml:featureMember><app:sites gml:id="p1"><app:name>长沙</app:name><app:position>\n       <gml:Point srsName="urn:ogc:def:crs:EPSG::4326"><gml:pos>28.19 112.98</gml:pos></gml:Point>\n      </app:position></app:sites></gml:featureMember>\n      <gml:featureMember><app:roads gml:id="r1"><app:name>route</app:name><app:position>\n       <gml:LineString srsName="urn:ogc:def:crs:EPSG::4326"><gml:posList>28.19 112.98 27.83 113.13</gml:posList></gml:LineString>\n      </app:position></app:roads></gml:featureMember>\n     </gml:FeatureCollection>', 'projected.topojson': '{"type":"Topology","transform":{"scale":[10,20],"translate":[500000,3000000]},\n     "objects":{\n      "routes":{"type":"GeometryCollection","geometries":[\n       {"type":"LineString","properties":{"name":"forward"},"arcs":[0]},\n       {"type":"LineString","properties":{"name":"reverse"},"arcs":[-1]}]},\n      "sites":{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[2,3],"properties":{"name":"station"}}]}},\n     "arcs":[[[0,0],[1,2],[2,1]]]}'}
(root / 'format-samples/vector-exchange').mkdir()
for name, content in vector_exchange.items():
    (root / 'format-samples/vector-exchange' / name).write_bytes(content.encode('utf-8'))

# Three deterministic, interleaved device sources exceed the whole-file point cap.
from datetime import datetime, timedelta, timezone
trajectory_rows = []
for hour in range(1500):
    at = (datetime(2026, 1, 1, tzinfo=timezone.utc) + timedelta(hours=hour)).isoformat().replace('+00:00', 'Z')
    for device in ('A', 'B'):
        trajectory_rows.append({'unit': device, 'at': at, 'east': round(120 + hour / 10000 + (0.001 if device == 'B' else 0), 4), 'north': round(30 + hour / 10000, 4)})
trajectory_csv = io.StringIO(newline='')
writer = csv.DictWriter(trajectory_csv, fieldnames=['unit', 'at', 'east', 'north'], lineterminator='\n')
writer.writeheader()
writer.writerows(trajectory_rows)
(root / 'format-samples/trajectory.csv').write_bytes(trajectory_csv.getvalue().encode('utf-8'))
for extension, value in [('json', trajectory_rows), ('geojson', {'type': 'FeatureCollection', 'features': [
        {'type': 'Feature', 'properties': row, 'geometry': {'type': 'Point', 'coordinates': [row['east'], row['north']]}}
        for row in trajectory_rows]})]:
    (root / f'format-samples/trajectory.{extension}').write_bytes((json.dumps(value, ensure_ascii=False, separators=(',', ':')) + '\n').encode('utf-8'))

sheets = [
    ("Cities", [["city_id", "city_name"]] + [[i, f"杭州{i}"] for i in range(1, 26)]),
    ("Readings", [["sensor", "temperature", "status"], ["Sensor-A", 18.5, "正常"]]),
    ("Empty", []),
    ("HeaderOnly", [["code", "label"]]),
]
parts = {
    "[Content_Types].xml": '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
        '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
        '<Default Extension="xml" ContentType="application/xml"/>'
        '<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>'
        + ''.join(f'<Override PartName="/xl/worksheets/sheet{i}.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>' for i in range(1, 5)) + '</Types>',
    "_rels/.rels": f'<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="{rel}/officeDocument" Target="xl/workbook.xml"/></Relationships>',
    "xl/workbook.xml": f'<workbook xmlns="{ns}" xmlns:r="{rel}"><bookViews><workbookView activeTab="0"/></bookViews><sheets>'
        + ''.join(f'<sheet name="{name}" sheetId="{i}" r:id="rId{i}"/>' for i, (name, _) in enumerate(sheets, 1)) + '</sheets></workbook>',
    "xl/_rels/workbook.xml.rels": '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
        + ''.join(f'<Relationship Id="rId{i}" Type="{rel}/worksheet" Target="worksheets/sheet{i}.xml"/>' for i in range(1, 5)) + '</Relationships>',
}
for index, (_, values) in enumerate(sheets, 1):
    rows = []
    for number, values_row in enumerate(values, 1):
        cells = []
        for column, value in enumerate(values_row):
            address = f"{chr(65 + column)}{number}"
            if isinstance(value, (int, float)):
                cells.append(f'<c r="{address}" t="n"><v>{value}</v></c>')
            else:
                cells.append(f'<c r="{address}" t="inlineStr"><is><t>{escape(value)}</t></is></c>')
        rows.append(f'<row r="{number}">' + ''.join(cells) + '</row>')
    dimension = f'A1:{chr(64 + len(values[0]))}{len(values)}' if values else 'A1'
    parts[f"xl/worksheets/sheet{index}.xml"] = f'<worksheet xmlns="{ns}"><dimension ref="{dimension}"/><sheetData>' + ''.join(rows) + '</sheetData></worksheet>'
with zipfile.ZipFile(root / "format-samples/format-validation.xlsx", "w", compression=zipfile.ZIP_STORED) as workbook:
    for name, xml in parts.items():
        entry = zipfile.ZipInfo(name, date_time=(2026, 1, 1, 0, 0, 0))
        entry.create_system = 3
        entry.external_attr = 0o100644 << 16
        workbook.writestr(entry, ('<?xml version="1.0" encoding="UTF-8"?>' + xml).encode('utf-8'))
# LAS 1.2, point format 0: three first-return points in EPSG:3857.
keys = struct.pack("<16H", 1, 1, 0, 3, 1024, 0, 1, 1, 3072, 0, 1, 3857, 3076, 0, 1, 9001)
vlr = struct.pack("<H16sHH32s", 0, b"LASF_Projection", 34735, len(keys), b"ADDP test projection") + keys
header = bytearray(227)
header[:4] = b"LASF"
header[24:26] = bytes((1, 2))
header[26:58] = b"ADDP deterministic fixture".ljust(32, b"\0")
header[58:90] = b"ADDP".ljust(32, b"\0")
struct.pack_into("<HHHII", header, 90, 1, 2026, 227, 227 + len(vlr), 1)
struct.pack_into("<BHI5I", header, 104, 0, 20, 3, 3, 0, 0, 0, 0)
struct.pack_into("<12d", header, 131, .01, .01, .01, 0, 0, 0, 2, 0, 2, 0, 1, 0)
records = b"".join(struct.pack("<iiiHBBbBH", x, y, z, 100, 9, 1, 0, 0, 0)
                   for x, y, z in ((0, 0, 0), (100, 100, 50), (200, 200, 100)))
(root / "pdal_las12_format0.las").write_bytes(header + vlr + records)
for format_name in ("dae", "3ds", "fbx", "obj"):
    (root / format_name).mkdir(parents=True)

(root / "dae/texture.png").write_bytes(Path(sys.argv[2]).read_bytes())

(root / "obj/model.obj").write_text('mtllib model.mtl\no panel\nv 0 0 0\nv 2 0 0\nv 0 3 0\n'
    'vt 0 0\nvt 1 0\nvt 0 1\nusemtl paint\nf 1/1 2/2 3/3\n')
(root / "obj/model.mtl").write_text('newmtl paint\nKd 1 1 1\nd 1\nmap_Kd texture.png\n')

(root / "dae/model.dae").write_bytes(Path(sys.argv[2]).with_name("addp_online_model_fixture.dae").read_bytes())

PY
}

seed_fixture() {
  MODEL_FIXTURE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-online-model3d.XXXXXX")
  trap 'rm -rf "$MODEL_FIXTURE_DIR"' EXIT
  generate_fixtures "$MODEL_FIXTURE_DIR"
  python3 "$SCRIPT_DIR/online-content-boundary-fixture.py" "$MODEL_FIXTURE_DIR"
  for format in ifc osgb skp max 3ds; do
    mkdir -p "$MODEL_FIXTURE_DIR/$format"
    cp "${BUSINESS_DIR}/fixtures/manager/addp_online_model_fixture.${format}" "$MODEL_FIXTURE_DIR/$format/model.$format"
  done
  cp "${BUSINESS_DIR}/fixtures/manager/addp_online_exchange_texture.png" "$MODEL_FIXTURE_DIR/3ds/texture.png"
  cp "${BUSINESS_DIR}/fixtures/manager/addp_online_max_texture.png" "$MODEL_FIXTURE_DIR/max/texture.png"
  cp "${BUSINESS_DIR}/fixtures/manager/addp_online_max_texture.png" "$MODEL_FIXTURE_DIR/obj/texture.png"
  cp "${REPOSITORY_DIR}/engines/model3d-workflow/tests/fixtures/mesh/box.fbx" "$MODEL_FIXTURE_DIR/fbx/model.fbx"
  cp "${REPOSITORY_DIR}/manager/frontend/e2e/fixtures/fbx/vCube.fbx" "$MODEL_FIXTURE_DIR/fbx/vCube.fbx"
  cp "${REPOSITORY_DIR}/manager/frontend/e2e/fixtures/fbx/LICENSE-three.txt" "$MODEL_FIXTURE_DIR/fbx/LICENSE-three.txt"
  POINTCLOUD_FIXTURE_SOURCE="$MODEL_FIXTURE_DIR/pdal_las12_format0.las"
  mc mb --ignore-existing "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET" >/dev/null
  local network
  network=$(minio_network)
  docker_fixture run --rm \
    --entrypoint mc \
    --network "$network" \
    -e "MC_HOST_fixture=http://${ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY}:${ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY}@business-minio:9000" \
    -v "$POINTCLOUD_FIXTURE_SOURCE:/fixture/source.las:ro" \
    -v "$PPTX_FIXTURE_SOURCE:/fixture/source.pptx:ro" \
    "$MC_IMAGE" cp --quiet /fixture/source.las \
    "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT"
  docker_fixture run --rm \
    --entrypoint mc \
    --network "$network" \
    -e "MC_HOST_fixture=http://${ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY}:${ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY}@business-minio:9000" \
    -v "$POINTCLOUD_FIXTURE_SOURCE:/fixture/source.las:ro" \
    -v "$PPTX_FIXTURE_SOURCE:/fixture/source.pptx:ro" \
    "$MC_IMAGE" cp --quiet /fixture/source.pptx \
    "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT"
  if [ "${ADDP_ONLINE_HOSTED:-0}" != "1" ]; then
    docker_fixture run --rm \
      --entrypoint mc \
      --network "$network" \
      -e "MC_HOST_fixture=http://${ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY}:${ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY}@business-minio:9000" \
      -v "$HYBRID_SEARCH_IMAGE_FIXTURE_SOURCE:/fixture/source.jpg:ro" \
      "$MC_IMAGE" cp --quiet /fixture/source.jpg \
      "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT"
  fi
  mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT" >/dev/null
  mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT" >/dev/null
  if [ "${ADDP_ONLINE_HOSTED:-0}" != "1" ]; then
    mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT" >/dev/null
  fi
  local format_object
  for format_object in "${FORMAT_OBJECTS[@]}"; do
    docker_fixture run --rm \
      --entrypoint mc \
      --network "$network" \
      -e "MC_HOST_fixture=http://${ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY}:${ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY}@business-minio:9000" \
      -v "$MODEL_FIXTURE_DIR:/fixture:ro" \
      "$MC_IMAGE" cp --quiet "/fixture/$format_object" \
      "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$format_object"
    mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$format_object" >/dev/null
  done
  local model_object
  local boundary_object
  while IFS= read -r boundary_object; do
    boundary_object=${boundary_object#"$MODEL_FIXTURE_DIR/"}
    docker_fixture run --rm --entrypoint mc --network "$network" \
      -e "MC_HOST_fixture=http://${ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY}:${ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY}@business-minio:9000" \
      -v "$MODEL_FIXTURE_DIR:/fixture:ro" "$MC_IMAGE" cp --quiet "/fixture/$boundary_object" \
      "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$boundary_object"
    mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$boundary_object" >/dev/null
  done < <(find "$MODEL_FIXTURE_DIR/scan-boundary" -type f | LC_ALL=C sort)
  for model_object in "${MODEL_OBJECTS[@]}"; do
    docker_fixture run --rm \
      --entrypoint mc \
      --network "$network" \
      -e "MC_HOST_fixture=http://${ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY}:${ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY}@business-minio:9000" \
      -v "$MODEL_FIXTURE_DIR:/fixture:ro" \
      "$MC_IMAGE" cp --quiet "/fixture/$model_object" \
      "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/model3d/$model_object"
    mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/model3d/$model_object" >/dev/null
  done
}

validate_container_ownership

case "$action" in
  index-*)
    container_running || fail "Business MinIO is not running"
    directory=$(mktemp -d "${TMPDIR:-/tmp}/addp-online-index-source.XXXXXX")
    trap 'rm -rf "$directory"' EXIT
    suffix=${action#index-}
    suffix=${suffix%%-*}
    python3 - "$directory" "$suffix" <<'PYINDEX'
from pathlib import Path
import sys
root = Path(sys.argv[1])
separator = {'csv': ',', 'tsv': '\t'}[sys.argv[2]]
for name, text in (('original', 'id,name\n1,Alice\n2,Bobby\n'), ('changed', 'id,city\n1,Tokyo\n2,Paris\n')):
    (root / name).write_bytes(text.replace(',', separator).encode('ascii'))
PYINDEX
    target="fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/index-source-versions/source.$suffix"
    # Exact byte comparison avoids treating a foreign/partially written object as ours.
    mc cat "$target" > "$directory/current"
    if [[ "$action" = *-change ]]; then
      cmp -s "$directory/current" "$directory/original" || fail "index source differs from the fixed original"
      desired=changed
    else
      if cmp -s "$directory/current" "$directory/original"; then
        exit 0
      fi
      cmp -s "$directory/current" "$directory/changed" || fail "index source restoration refused unexpected bytes"
      desired=original
    fi
    docker_fixture run --rm --entrypoint mc --network "$(minio_network)" \
      -e "MC_HOST_fixture=http://${ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY}:${ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY}@business-minio:9000" \
      -v "$directory:/fixture:ro" "$MC_IMAGE" cp --quiet "/fixture/$desired" "$target" >/dev/null
    mc cat "$target" > "$directory/current"
    cmp -s "$directory/current" "$directory/$desired" || fail "index source write verification failed"
    ;;
  start)
    if ! docker_fixture image inspect "$MC_IMAGE" >/dev/null 2>&1; then
      compose build minio
    fi
    compose up -d minio
    for _ in $(seq 1 60); do
      if container_running && curl -fsS "http://127.0.0.1:${ADDP_ONLINE_MANAGER_MINIO_PORT}/minio/health/live" >/dev/null 2>&1; then
        seed_fixture
        if [ "${ADDP_ONLINE_HOSTED:-0}" = "1" ]; then
          python3 - <<'PYDESCRIPTOR'
import json, os
from pathlib import Path
path = Path(os.environ["ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE"])
with path.open("w") as output:
    path.chmod(0o600)
    json.dump({"name": "Hosted Manager MinIO", "engine_type": "minio", "engine_origin": "general",
               "description": "Disposable Manager artifact T4 source",
               "connection_info": {"endpoint": "127.0.0.1:" + os.environ["ADDP_ONLINE_MANAGER_MINIO_PORT"],
                                   "access_key": os.environ["ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY"],
                                   "secret_key": os.environ["ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY"], "use_ssl": False}}, output)
PYDESCRIPTOR
        fi
        echo "Online Manager MinIO Fixture is ready on port $ADDP_ONLINE_MANAGER_MINIO_PORT"
        exit 0
      fi
      sleep 1
    done
    fail "business-minio did not become ready"
    ;;
  stop)
    if [ "${ADDP_ONLINE_HOSTED:-0}" = "1" ]; then
      compose down --volumes --remove-orphans
      for kind in container network volume; do
        case "$kind" in
          container) remaining=$(docker_fixture ps -aq --filter label=com.docker.compose.project=business) ;;
          *) remaining=$(docker_fixture "$kind" ls -q --filter label=com.docker.compose.project=business) ;;
        esac
        [ -z "$remaining" ] || fail "Business MinIO $kind cleanup has residuals"
      done
    else
      compose rm -sf minio
    fi
    if container_running; then
      fail "business-minio is still running"
    fi
    echo "Online Manager MinIO Fixture is stopped"
    ;;
  status)
    container_running || fail "business-minio is not running"
    curl -fsS "http://127.0.0.1:${ADDP_ONLINE_MANAGER_MINIO_PORT}/minio/health/live" >/dev/null ||
      fail "business-minio is not ready"
    mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_POINTCLOUD_OBJECT" >/dev/null ||
      fail "point-cloud fixture object is missing"
    mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_PPTX_OBJECT" >/dev/null ||
      fail "PPTX fixture object is missing"
    if [ "${ADDP_ONLINE_HOSTED:-0}" != "1" ]; then
      mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$ADDP_ONLINE_MANAGER_MINIO_HYBRID_SEARCH_IMAGE_OBJECT" >/dev/null ||
        fail "hybrid-search image fixture object is missing"
    fi
    for format_object in "${FORMAT_OBJECTS[@]}"; do
      mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/$format_object" >/dev/null ||
        fail "format fixture object is missing: $format_object"
    done
    for model_object in "${MODEL_OBJECTS[@]}"; do
      mc stat "fixture/$ADDP_ONLINE_MANAGER_MINIO_BUCKET/model3d/$model_object" >/dev/null ||
        fail "model fixture object is missing: $model_object"
    done
    echo "Online Manager MinIO Fixture is ready"
    ;;
esac
