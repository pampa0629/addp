#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=elasticsearch,spark-master,spark-worker
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.elasticsearch-t2.yml
# ADDP_T2_INPUT_FILES=business/elasticsearch/ business/docker-compose.yml engines/spark-workflow/ develop/backend/internal/service/workflow_engine_service.go develop/backend/internal/service/workflow_operator_adapter.go scripts/test/elasticsearch-spark-contract.py
# Run consumer contracts against an owned disposable Elasticsearch deployment.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-elasticsearch.XXXXXX")
export ELASTICSEARCH_T2_WORK_DIR="$WORK_DIR"
COMPOSE_PROJECT="addp-elasticsearch-t2-${PPID}-$$"
export ELASTICSEARCH_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_urlsafe(24))')"
export ELASTICSEARCH_READER_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_urlsafe(24))')"
export ELASTICSEARCH_READER_USER=addp_business_reader
compose() { docker compose -p "$COMPOSE_PROJECT" -f "$ROOT_DIR/scripts/test/docker-compose.elasticsearch-t2.yml" "$@"; }
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    compose down --volumes --remove-orphans >/dev/null 2>&1 || status=1
    if docker ps -a --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.ID}}' | grep -q . || docker volume ls --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.Name}}' | grep -q . || docker network ls --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" --format '{{.ID}}' | grep -q .; then
        echo "Elasticsearch T2 cleanup left containers or volumes" >&2; status=1
    fi
    rm -rf "$WORK_DIR"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
python3 - "$ROOT_DIR" "$WORK_DIR" <<'PY'
import sys
sys.path.insert(0, sys.argv[1] + '/engines/spark-workflow')
from spark_dependencies import ensure_elasticsearch_jar
ensure_elasticsearch_jar(sys.argv[2])
PY
compose up -d
port=$(compose port elasticsearch 9200)
export ELASTICSEARCH_ENDPOINT="http://$port"
python3 "$ROOT_DIR/business/elasticsearch/init.py"
# Reinitialization must be idempotent.
python3 "$ROOT_DIR/business/elasticsearch/init.py"
export ADDP_ELASTICSEARCH_INTEGRATION=1
for specification in 'common engine/plugins/elasticsearch' 'manager/backend internal/preview' 'meta/backend internal/scanruntime'; do
    read -r owner package <<< "$specification"
    (cd "$ROOT_DIR/$owner" && GOWORK=off go test "./$package" -run '^TestIntegrationElasticsearch' -count=1 -v) 2>&1 | tee "$WORK_DIR/${owner//\//-}.log"
    if grep -q -- '--- SKIP:' "$WORK_DIR/${owner//\//-}.log"; then echo "Elasticsearch T2 refuses skipped tests" >&2; exit 1; fi
    grep -q -- '--- PASS: TestIntegrationElasticsearch' "$WORK_DIR/${owner//\//-}.log" || { echo "Elasticsearch T2 requires real owner tests" >&2; exit 1; }
done
compose exec -T spark-worker hostname > "$WORK_DIR/worker-hostname"
python3 - "$ROOT_DIR" <<'PY'
import base64, importlib.util, os, sys, urllib.error, urllib.request
spec = importlib.util.spec_from_file_location('es_fixture', sys.argv[1] + '/business/elasticsearch/init.py')
fixture = importlib.util.module_from_spec(spec); spec.loader.exec_module(fixture)
endpoint, password = os.environ['ELASTICSEARCH_ENDPOINT'], os.environ['ELASTICSEARCH_PASSWORD']
headers = {'Authorization': 'Basic ' + base64.b64encode((os.environ['ELASTICSEARCH_READER_USER'] + ':' + os.environ['ELASTICSEARCH_READER_PASSWORD']).encode()).decode()}
for path, expected in (('/', 200), ('/_cluster/health/addp_orders.v1', 200), ('/_nodes/http', 403), ('/_cluster/settings', 403)):
    try:
        with urllib.request.urlopen(urllib.request.Request(endpoint + path, headers=headers), timeout=10) as response: actual = response.status
    except urllib.error.HTTPError as error: actual = error.code
    assert actual == expected, (path, actual, expected)
mapping = fixture.request(endpoint, password, 'GET', '/addp_orders.v1/_mapping')['addp_orders.v1']['mappings']
fixture.request(endpoint, password, 'PUT', '/addp_multi.v1', {'settings': {'number_of_shards': 3, 'number_of_replicas': 0}, 'mappings': mapping})
fixture.request(endpoint, password, 'POST', '/_reindex?refresh=true', {'source': {'index': 'addp_orders.v1'}, 'dest': {'index': 'addp_multi.v1'}})
fixture.request(endpoint, password, 'POST', '/_aliases', {'actions': [{'add': {'index': 'addp_orders.v1', 'alias': 'addp_alias'}}]})
fixture.request(endpoint, password, 'PUT', '/foreign_private', {'settings': {'number_of_replicas': 0}})
PY
compose exec -T spark-master /opt/spark/bin/spark-submit --master spark://spark-master:7077 \
    --jars /gate/elasticsearch-spark-30_2.12-9.5.4.jar /addp/elasticsearch-spark-contract.py 2>&1 | tee "$WORK_DIR/spark.log"
grep -q '^ES_SPARK_PASS rows=25 amount_sum=562.5 empty=true multi_shard=true distributed=true$' "$WORK_DIR/spark.log"
python3 - "$ROOT_DIR" <<'PY'
import importlib.util, os, sys, time
spec = importlib.util.spec_from_file_location('es_fixture', sys.argv[1] + '/business/elasticsearch/init.py')
fixture = importlib.util.module_from_spec(spec); spec.loader.exec_module(fixture)
for attempt in range(30):
    stats = fixture.request(os.environ['ELASTICSEARCH_ENDPOINT'], os.environ['ELASTICSEARCH_PASSWORD'], 'GET', '/_nodes/stats/indices/search')
    if all(node['indices']['search']['open_contexts'] == 0 for node in stats['nodes'].values()): break
    time.sleep(1)
else: raise AssertionError('Elasticsearch scroll/PIT contexts remain after reads or errors')
PY
echo 'ELASTICSEARCH_T2_PASS plugin, consumers and distributed Spark contracts'
