#!/usr/bin/env bash
# certify-infra-kafka.sh - certification for the formal ADDP Infra Kafka API plane

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

if [ -f ./.env ]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env
  set +a
fi

for command in docker curl jq go rg; do
  command -v "${command}" >/dev/null 2>&1 || { echo "missing command: ${command}" >&2; exit 1; }
done
docker compose version >/dev/null

COMPOSE=(docker compose -f docker-compose.infra.yml)
RPK_IMAGE="${REDPANDA_IMAGE:-docker.redpanda.com/redpandadata/redpanda:v24.3.18}"
ADMIN_USERNAME="${INFRA_KAFKA_ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${INFRA_KAFKA_ADMIN_PASSWORD:-addp_kafka_admin}"
CONNECT_PASSWORD="${INFRA_KAFKA_CONNECT_PASSWORD:-addp_kafka_connect}"
TRANSFER_PASSWORD="${INFRA_KAFKA_TRANSFER_PASSWORD:-addp_kafka_transfer}"
TEST_DATABASE="${ADDP_TEST_POSTGRES_DATABASE:-addp_test}"
STAMP="$(date +%Y%m%d%H%M%S)"
REPORT_DIR="${TMPDIR:-/tmp}/addp-redpanda-certification-${STAMP}"
mkdir -p "${REPORT_DIR}"

API_TOPIC="__addp_cdc.cert.api.${STAMP}"
RETENTION_TOPIC="__addp_cdc.cert.retention.${STAMP}"
GROUP="__addp_cdc_consumer.cert.${STAMP}"
CONNECTOR="addp-cert-redpanda-${STAMP}"
SOURCE_SCHEMA="rp_cert_${STAMP}"
SOURCE_TABLE="orders"
DATA_TOPIC="__addp_cdc.cert.connect.${STAMP}"
SLOT="rp_cert_${STAMP}"
PUBLICATION="rp_cert_${STAMP}_pub"

rpk_as() {
  local username="$1" password="$2"
  shift 2
  if [ "${RPK_ATTACH_STDIN:-0}" = 1 ]; then
    docker run --rm -i --network addp-network --entrypoint /usr/bin/rpk "${RPK_IMAGE}" \
      "$@" -X brokers=redpanda:29092 -X "user=${username}" -X "pass=${password}" \
      -X sasl.mechanism=SCRAM-SHA-256
    return
  fi
  docker run --rm --network addp-network --entrypoint /usr/bin/rpk "${RPK_IMAGE}" \
    "$@" -X brokers=redpanda:29092 -X "user=${username}" -X "pass=${password}" \
    -X sasl.mechanism=SCRAM-SHA-256 </dev/null
}

rpk_admin() { rpk_as "${ADMIN_USERNAME}" "${ADMIN_PASSWORD}" "$@"; }
rpk_connect() { rpk_as connect "${CONNECT_PASSWORD}" "$@"; }
rpk_transfer() { rpk_as transfer "${TRANSFER_PASSWORD}" "$@"; }

wait_redpanda() {
  local deadline=$((SECONDS + 120))
  until [ "$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}' addp-redpanda 2>/dev/null || true)" = healthy ]; do
    [ "${SECONDS}" -lt "${deadline}" ] || return 1
    sleep 1
  done
}

wait_connect() {
  local deadline=$((SECONDS + 120))
  until curl -sf http://localhost:18083/connectors >/dev/null; do
    [ "${SECONDS}" -lt "${deadline}" ] || return 1
    sleep 1
  done
}

wait_connector_running() {
  local deadline=$((SECONDS + 120)) status
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    status="$(curl -sf "http://localhost:18083/connectors/${CONNECTOR}/status" 2>/dev/null || true)"
    if [ -n "${status}" ] && jq -e '
      .connector.state == "RUNNING" and
      (.tasks | length > 0) and
      ([.tasks[].state == "RUNNING"] | all)
    ' <<<"${status}" >/dev/null; then
      return 0
    fi
    sleep 1
  done
  return 1
}

topic_offset() {
  local topic="$1" field="$2"
  rpk_admin topic describe "${topic}" --print-partitions --format json |
    jq -r --arg field "${field}" '.[0].partitions[0] | if $field == "earliest" then .log_start_offset else .high_watermark end'
}

wait_topic_latest_greater_than() {
  local topic="$1" baseline="$2" timeout="$3" failure_message="$4"
  local deadline=$((SECONDS + timeout)) latest
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    latest="$(topic_offset "${topic}" latest)"
    if [ "${latest}" -gt "${baseline}" ]; then
      printf '%s\n' "${latest}"
      return 0
    fi
    sleep 1
  done
  echo "${failure_message}" >&2
  return 1
}

cleanup() {
  set +e
  curl -sf -X DELETE "http://localhost:18083/connectors/${CONNECTOR}" >/dev/null
  sleep 1
  docker exec business-postgres psql -U "${BUSINESS_PG_USER:-business}" -d "${BUSINESS_PG_DB:-business}" -v ON_ERROR_STOP=0 -c \
    "SELECT pg_drop_replication_slot('${SLOT}') WHERE EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name='${SLOT}' AND NOT active); DROP PUBLICATION IF EXISTS \"${PUBLICATION}\"; DROP SCHEMA IF EXISTS \"${SOURCE_SCHEMA}\" CASCADE;" >/dev/null 2>&1
  rpk_admin group delete "${GROUP}" >/dev/null 2>&1
  rpk_admin topic delete "${API_TOPIC}" "${RETENTION_TOPIC}" "${DATA_TOPIC}" >/dev/null 2>&1
}
trap cleanup EXIT

existing_connectors="$(curl -sf http://localhost:18083/connectors 2>/dev/null || echo '[]')"
if [ "$(jq 'length' <<<"${existing_connectors}")" -ne 0 ]; then
  echo "refusing Infra Kafka certification while Kafka Connect has active connectors: ${existing_connectors}" >&2
  exit 1
fi

echo "[RP-01] static single-data-plane gate"
"${COMPOSE[@]}" config >"${REPORT_DIR}/compose-redpanda.yml"
[ "$("${COMPOSE[@]}" config --services | grep -cx redpanda)" -eq 1 ]
[ "$("${COMPOSE[@]}" config --services | grep -cx redpanda-init)" -eq 1 ]
rg -q 'image: docker.redpanda.com/redpandadata/redpanda:v24.3.18' "${REPORT_DIR}/compose-redpanda.yml"
rg -q 'image: quay.io/debezium/connect:3.6.0.Final' "${REPORT_DIR}/compose-redpanda.yml"
if rg -n 'image: apache/kafka|container_name: addp-kafka$|^[[:space:]]+kafka:$|kafka-init' \
  "${REPORT_DIR}/compose-redpanda.yml"; then
  echo "obsolete Apache Kafka deployment path remains" >&2
  exit 1
fi

echo "[RP-02/RP-03] start formal profile, initialize SCRAM/ACL with rpk, wait for Connect"
INFRA_KAFKA_VERIFY_ACL=1 "${COMPOSE[@]}" up -d --force-recreate redpanda redpanda-init kafka-connect
wait_redpanda
wait_connect
docker logs addp-redpanda-init >"${REPORT_DIR}/redpanda-init.log" 2>&1
rg -q 'Infra Kafka ACL verification passed' "${REPORT_DIR}/redpanda-init.log"
docker inspect addp-redpanda --format '{{index .Config.Labels "org.addp.infra.kafka.profile"}} {{.Config.Image}} {{.State.Health.Status}}' | tee "${REPORT_DIR}/broker.txt"
curl -sf http://localhost:18083/ | tee "${REPORT_DIR}/connect-root.json" | jq -e '.version == "4.3.0"' >/dev/null
curl -sf http://localhost:18083/connector-plugins | tee "${REPORT_DIR}/connect-plugins.json" |
  jq -e 'map(.class) | index("io.debezium.connector.postgresql.PostgresConnector") and index("io.debezium.connector.mysql.MySqlConnector")' >/dev/null

echo "[RP-04] topic/group/admin API through rpk"
rpk_admin topic create "${API_TOPIC}" --partitions 1 --replicas 1 \
  --topic-config cleanup.policy=delete --topic-config retention.ms=600000 >/dev/null
printf '%s\n' first | RPK_ATTACH_STDIN=1 rpk_connect topic produce "${API_TOPIC}" >/dev/null
first="$(rpk_transfer topic consume "${API_TOPIC}" --group "${GROUP}" --offset start --num 1 --format '%v\n')"
[ "${first}" = first ]
rpk_admin group describe "${GROUP}" | tee "${REPORT_DIR}/group-before-restart.txt"

echo "[RP-03/RP-10] active Debezium connector and broker/Connect restart recovery"
docker exec business-postgres psql -U "${BUSINESS_PG_USER:-business}" -d "${BUSINESS_PG_DB:-business}" -v ON_ERROR_STOP=1 -c \
  "CREATE SCHEMA \"${SOURCE_SCHEMA}\"; CREATE TABLE \"${SOURCE_SCHEMA}\".\"${SOURCE_TABLE}\" (id bigint PRIMARY KEY, name text NOT NULL); INSERT INTO \"${SOURCE_SCHEMA}\".\"${SOURCE_TABLE}\" VALUES (1, 'snapshot');" >/dev/null
rpk_admin topic create "${DATA_TOPIC}" --partitions 1 --replicas 1 \
  --topic-config cleanup.policy=delete --topic-config retention.ms=600000 >/dev/null
jq -n \
  --arg connector "${CONNECTOR}" --arg schema "${SOURCE_SCHEMA}" --arg table "${SOURCE_TABLE}" \
  --arg topic "${DATA_TOPIC}" --arg slot "${SLOT}" --arg publication "${PUBLICATION}" \
  --arg user "${BUSINESS_PG_USER:-business}" --arg password "${BUSINESS_PG_PASSWORD:-business_password}" \
  '{
    "connector.class":"io.debezium.connector.postgresql.PostgresConnector",
    "tasks.max":"1", "database.hostname":"host.docker.internal", "database.port":"5433",
    "database.user":$user, "database.password":$password, "database.dbname":"business",
    "topic.prefix":$connector, "plugin.name":"pgoutput", "slot.name":$slot,
    "publication.name":$publication, "publication.autocreate.mode":"filtered", "snapshot.mode":"initial",
    "schema.include.list":$schema, "table.include.list":($schema + "." + $table),
    "key.converter":"org.apache.kafka.connect.json.JsonConverter", "key.converter.schemas.enable":"false",
    "value.converter":"org.apache.kafka.connect.json.JsonConverter", "value.converter.schemas.enable":"false",
    "transforms":"route", "transforms.route.type":"org.apache.kafka.connect.transforms.RegexRouter",
    "transforms.route.regex":".*", "transforms.route.replacement":$topic,
    "tombstones.on.delete":"false", "slot.drop.on.stop":"false"
  }' >"${REPORT_DIR}/connector-config.json"
curl -sf -X PUT -H 'Content-Type: application/json' \
  --data @"${REPORT_DIR}/connector-config.json" "http://localhost:18083/connectors/${CONNECTOR}/config" >/dev/null
wait_connector_running
before_restart_latest="$(wait_topic_latest_greater_than \
  "${DATA_TOPIC}" 0 120 "connector initial snapshot was not emitted")"
docker stats --no-stream addp-redpanda addp-kafka-connect | tee "${REPORT_DIR}/resources-cdc-steady.txt"
docker exec addp-redpanda du -sb /var/lib/redpanda/data | tee "${REPORT_DIR}/redpanda-volume-bytes.txt"

"${COMPOSE[@]}" restart redpanda
wait_redpanda
wait_connect
wait_connector_running
printf '%s\n' second | RPK_ATTACH_STDIN=1 rpk_connect topic produce "${API_TOPIC}" >/dev/null
second="$(rpk_transfer topic consume "${API_TOPIC}" --group "${GROUP}" --offset 1 --num 1 --format '%v\n')"
[ "${second}" = second ]
"${COMPOSE[@]}" restart kafka-connect
wait_connect
wait_connector_running
docker exec business-postgres psql -U "${BUSINESS_PG_USER:-business}" -d "${BUSINESS_PG_DB:-business}" -v ON_ERROR_STOP=1 -c \
  "INSERT INTO \"${SOURCE_SCHEMA}\".\"${SOURCE_TABLE}\" VALUES (2, 'after-restart');" >/dev/null
wait_topic_latest_greater_than \
  "${DATA_TOPIC}" "${before_restart_latest}" 30 "connector did not resume after restart" >/dev/null
curl -sf "http://localhost:18083/connectors/${CONNECTOR}/status" | tee "${REPORT_DIR}/connector-after-restart.json" | jq -e '.connector.state == "RUNNING"' >/dev/null

echo "[RP-08] retention config and earliest-offset advancement"
rpk_admin topic create "${RETENTION_TOPIC}" --partitions 1 --replicas 1 \
  --topic-config cleanup.policy=delete --topic-config retention.ms=2000 \
  --topic-config retention.bytes=4096 --topic-config segment.bytes=1048576 \
  --topic-config segment.ms=1000 >/dev/null
for batch in $(seq 1 6); do
  awk -v batch="${batch}" 'BEGIN { payload=sprintf("%02048d", 0); for (i=1; i<=200; i++) print batch "-" i "-" payload }' |
    RPK_ATTACH_STDIN=1 rpk_connect topic produce "${RETENTION_TOPIC}" --compression none >/dev/null
  sleep 1
done
retention_latest="$(topic_offset "${RETENTION_TOPIC}" latest)"
deadline=$((SECONDS + 60))
retention_earliest=0
while [ "${retention_earliest}" -eq 0 ] && [ "${SECONDS}" -lt "${deadline}" ]; do
  sleep 2
  retention_earliest="$(topic_offset "${RETENTION_TOPIC}" earliest)"
done
[ "${retention_latest}" -gt 0 ] && [ "${retention_earliest}" -gt 0 ]
rpk_admin topic describe "${RETENTION_TOPIC}" --print-all | tee "${REPORT_DIR}/retention-topic.txt"
printf 'earliest=%s latest=%s\n' "${retention_earliest}" "${retention_latest}" | tee "${REPORT_DIR}/retention-offsets.txt"

echo "[RP-05/RP-06/RP-07/RP-08/RP-09] existing ADDP end-to-end contracts"
export ADDP_TEST_POSTGRES_DATABASE="${TEST_DATABASE}"
export ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM=scram-sha-256
export ADDP_TEST_KAFKA_SASL_MECHANISM=scram-sha-256
export ADDP_TEST_KAFKA_CONNECT_BOOTSTRAP_SERVERS=redpanda:29092
(
  cd transfer/backend
  ADDP_CDC_CONTROL_E2E=1 go test ./internal/capture -run TestIntegrationPostgreSQLCaptureControlLifecycle -count=1 -v
  ADDP_CDC_DATA_E2E=1 go test ./internal/continuous -run TestIntegrationPostgreSQLCDCDataPlaneViaPublicAPISnapshotUpdateDeleteCrashResumeAndStopCleanup -count=1 -v
  ADDP_MYSQL_CDC_DATA_E2E=1 go test ./internal/continuous -run TestIntegrationMySQLCDCDataPlaneViaPublicAPIFullLifecycle -count=1 -v
  ADDP_DLQ_KAFKA_INTEGRATION=1 go test ./internal/deadletter -run TestIntegrationKafkaPayloadWriterCreatesAndWritesTransferDLQTopic -count=1 -v
  ADDP_CONTINUOUS_DLQ_E2E=1 go test ./internal/continuous -run TestIntegrationContinuousKafkaDeadLetterSkipAndCAS -count=1 -v
  ADDP_TEST_KAFKA_USERNAME="${ADMIN_USERNAME}" ADDP_TEST_KAFKA_PASSWORD="${ADMIN_PASSWORD}" \
    ADDP_CONTINUOUS_E2E=1 go test ./internal/continuous -run TestIntegrationContinuousKafkaRetentionHealthTransitions -count=1 -v
) | tee "${REPORT_DIR}/addp-e2e.log"

echo "[RP-11/RP-12] resource and support-scope gates"
docker stats --no-stream addp-redpanda addp-kafka-connect | tee "${REPORT_DIR}/resources-final.txt"
if rg -i redpanda common transfer gateway system \
  --glob '!**/*.md' --glob '!**/*test.go' \
  --glob '!transfer/backend/internal/config/config.go' --glob '!**/node_modules/**'; then
  echo "Redpanda runtime branch leaked outside deployment configuration" >&2
  exit 1
fi
if rg -n 'apache/kafka|addp-kafka-init|addp-kafka$|^[[:space:]]+kafka:$|kafka:29092|INFRA_KAFKA_CLI_IMAGE' \
  docker-compose.infra.yml docker-compose.infra.ha.yml scripts/infra .env.example; then
  echo "obsolete Apache Kafka deployment path remains" >&2
  exit 1
fi

cat >"${REPORT_DIR}/result.txt" <<EOF
result=passed
profile=redpanda
broker_service=redpanda
broker_container=addp-redpanda
broker_image=$(docker inspect addp-redpanda --format '{{.Config.Image}}')
init_image=$(docker inspect addp-redpanda-init --format '{{.Config.Image}}')
connect_image=$(docker inspect addp-kafka-connect --format '{{.Config.Image}}')
sasl_mechanism=SCRAM-SHA-256
retention_earliest=${retention_earliest}
retention_latest=${retention_latest}
completed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF

echo "Infra Kafka certification passed. Evidence: ${REPORT_DIR}"
