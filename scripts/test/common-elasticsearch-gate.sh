#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=elasticsearch
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.elasticsearch-t2.yml
# ADDP_T2_INPUT_FILES=business/elasticsearch/ business/docker-compose.yml
# Run consumer contracts against an owned disposable Elasticsearch deployment.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/addp-elasticsearch.XXXXXX")
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
trap cleanup EXIT INT TERM
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
