#!/usr/bin/env bash
# certify-infra-kafka-ha.sh - HA/SASL_SSL certification for the formal Redpanda profile
#
# Usage: bash scripts/test/certify-infra-kafka-ha.sh

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

for command in docker curl jq go rg openssl awk; do
  command -v "${command}" >/dev/null 2>&1 || { echo "missing command: ${command}" >&2; exit 1; }
done
docker compose version >/dev/null

STAMP="$(date +%Y%m%d%H%M%S)"
REPORT_DIR="${TMPDIR:-/tmp}/addp-redpanda-ha-certification-${STAMP}"
CERT_DIR="$(mktemp -d)"
mkdir -p "${REPORT_DIR}"
export REDPANDA_CERT_DIR="${CERT_DIR}"

COMPOSE=(docker compose -f docker-compose.infra.yml -f docker-compose.infra.ha.yml)
DEFAULT_COMPOSE=(docker compose -f docker-compose.infra.yml)
RPK_IMAGE="${REDPANDA_IMAGE:-docker.redpanda.com/redpandadata/redpanda:v24.3.18}"
PERF_CLIENT_IMAGE="${KAFKA_CONNECT_IMAGE:-quay.io/debezium/connect:3.6.0.Final}"
ADMIN_USERNAME="${INFRA_KAFKA_ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${INFRA_KAFKA_ADMIN_PASSWORD:-addp_kafka_admin}"
CONNECT_PASSWORD="${INFRA_KAFKA_CONNECT_PASSWORD:-addp_kafka_connect}"
TRANSFER_PASSWORD="${INFRA_KAFKA_TRANSFER_PASSWORD:-addp_kafka_transfer}"
TEST_DATABASE="${ADDP_TEST_POSTGRES_DATABASE:-addp_test}"
CONNECT_GROUP="${KAFKA_CONNECT_GROUP_ID:-addp-connect-cluster}"
HOST_BOOTSTRAP_SERVERS="localhost:${INFRA_KAFKA_PORT:-19092},localhost:${REDPANDA_HA_KAFKA_2_PORT:-19093},localhost:${REDPANDA_HA_KAFKA_3_PORT:-19094}"

LOAD_TOPIC="__addp_cdc.cert.ha.load.${STAMP}"
RETENTION_TOPIC="__addp_cdc.cert.ha.retention.${STAMP}"
PERF_TOPIC="__addp_cdc.cert.ha.perf.${STAMP}"
LOAD_GROUP="__addp_cdc_consumer.cert.ha.${STAMP}"
DATA_TOPIC="__addp_cdc.cert.ha.connect.${STAMP}"
CONNECTOR="addp-cert-redpanda-ha-${STAMP}"
SOURCE_SCHEMA="rp_ha_cert_${STAMP}"
SOURCE_TABLE="orders"
SLOT="rp_ha_cert_${STAMP}"
PUBLICATION="rp_ha_cert_${STAMP}_pub"
LOAD_MESSAGES="${REDPANDA_HA_CERT_LOAD_MESSAGES:-5000}"
PERF_MESSAGES="${REDPANDA_HA_CERT_PERF_MESSAGES:-20000}"
PERF_THROUGHPUT="${REDPANDA_HA_CERT_PERF_THROUGHPUT:-2000}"
ha_started=0

CONNECT_CONFIG="$(mktemp)"

generate_certificates() {
  openssl req -x509 -newkey rsa:2048 -nodes -days 2 \
    -subj '/CN=ADDP Redpanda HA Certification CA' \
    -keyout "${CERT_DIR}/ca.key" -out "${CERT_DIR}/ca.crt" >/dev/null 2>&1
  openssl req -newkey rsa:2048 -nodes -subj '/CN=redpanda' \
    -addext 'subjectAltName=DNS:redpanda,DNS:redpanda-2,DNS:redpanda-3,DNS:localhost,IP:127.0.0.1' \
    -keyout "${CERT_DIR}/server.key" -out "${CERT_DIR}/server.csr" >/dev/null 2>&1
  openssl x509 -req -in "${CERT_DIR}/server.csr" \
    -CA "${CERT_DIR}/ca.crt" -CAkey "${CERT_DIR}/ca.key" -CAcreateserial \
    -days 2 -copy_extensions copy -out "${CERT_DIR}/server.crt" >/dev/null 2>&1
  chmod 0644 "${CERT_DIR}"/ca.crt "${CERT_DIR}"/server.crt "${CERT_DIR}"/server.key
  openssl x509 -in "${CERT_DIR}/server.crt" -noout -subject -issuer -dates -ext subjectAltName \
    >"${REPORT_DIR}/certificate.txt"
}

write_perf_client_config() {
  umask 077
  {
    echo "security.protocol=SASL_SSL"
    echo "sasl.mechanism=SCRAM-SHA-256"
    echo "sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required username=\"connect\" password=\"${CONNECT_PASSWORD}\";"
    echo "request.timeout.ms=5000"
    echo "default.api.timeout.ms=10000"
    echo "ssl.truststore.type=PEM"
    echo "ssl.truststore.location=/tmp/ca.crt"
    echo "ssl.endpoint.identification.algorithm=https"
  } >"${CONNECT_CONFIG}"
}

rpk_as() {
  local username="$1" password="$2" brokers="$3" tls_mode="$4"
  shift 4
  local docker_args=(--rm --network addp-network --entrypoint /usr/bin/rpk)
  local rpk_args=(
    -X "brokers=${brokers}"
    -X "user=${username}"
    -X "pass=${password}"
    -X sasl.mechanism=SCRAM-SHA-256
    -X tls.enabled=true
  )
  if [ "${tls_mode}" = trusted ]; then
    docker_args+=(-v "${CERT_DIR}/ca.crt:/tmp/ca.crt:ro")
    rpk_args+=(-X tls.ca=/tmp/ca.crt)
  fi
  if [ "${RPK_ATTACH_STDIN:-0}" = 1 ]; then
    docker_args+=(-i)
    docker run "${docker_args[@]}" "${RPK_IMAGE}" "$@" "${rpk_args[@]}"
    return
  fi
  docker run "${docker_args[@]}" "${RPK_IMAGE}" "$@" "${rpk_args[@]}" </dev/null
}

rpk_admin() {
  rpk_as "${ADMIN_USERNAME}" "${ADMIN_PASSWORD}" \
    redpanda:29092,redpanda-2:29092,redpanda-3:29092 trusted "$@"
}

rpk_connect() {
  rpk_as connect "${CONNECT_PASSWORD}" \
    redpanda:29092,redpanda-2:29092,redpanda-3:29092 trusted "$@"
}

rpk_transfer() {
  rpk_as transfer "${TRANSFER_PASSWORD}" \
    redpanda:29092,redpanda-2:29092,redpanda-3:29092 trusted "$@"
}

rpk_wrong_host() {
  local redpanda_ip
  redpanda_ip="$(docker inspect addp-redpanda --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')"
  docker run --rm --network addp-network --add-host "wrong-redpanda:${redpanda_ip}" \
    --entrypoint /usr/bin/rpk -v "${CERT_DIR}/ca.crt:/tmp/ca.crt:ro" "${RPK_IMAGE}" \
    "$@" -X brokers=wrong-redpanda:29092 -X "user=${ADMIN_USERNAME}" -X "pass=${ADMIN_PASSWORD}" \
    -X sasl.mechanism=SCRAM-SHA-256 -X tls.enabled=true -X tls.ca=/tmp/ca.crt </dev/null
}

perf_client() {
  local executable="$1"
  shift
  docker run --rm -i --network addp-network --entrypoint "${executable}" \
    -v "${CERT_DIR}/ca.crt:/tmp/ca.crt:ro" \
    -v "${CONNECT_CONFIG}:/tmp/connect.properties:ro" \
    "${PERF_CLIENT_IMAGE}" "$@" </dev/null
}

wait_container_health() {
  local container="$1" deadline=$((SECONDS + 180))
  until [ "$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "${container}" 2>/dev/null || true)" = healthy ]; do
    [ "${SECONDS}" -lt "${deadline}" ] || return 1
    sleep 1
  done
}

wait_connect() {
  local url="$1" deadline=$((SECONDS + 180))
  until curl -sf "${url}/connectors" >/dev/null; do
    [ "${SECONDS}" -lt "${deadline}" ] || return 1
    sleep 1
  done
}

wait_cluster_healthy() {
  local deadline=$((SECONDS + 180))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if docker exec addp-redpanda rpk cluster health \
      -X brokers=localhost:29092 -X user="${ADMIN_USERNAME}" -X pass="${ADMIN_PASSWORD}" \
      -X sasl.mechanism=SCRAM-SHA-256 -X tls.enabled=true -X tls.ca=/etc/redpanda/certs/ca.crt \
      2>/dev/null | grep -q 'Healthy:.*true'; then
      return 0
    fi
    sleep 2
  done
  return 1
}

wait_connector_running() {
  local base_url="$1" deadline=$((SECONDS + 180)) status
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    status="$(curl -sf "${base_url}/connectors/${CONNECTOR}/status" 2>/dev/null || true)"
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

wait_connect_group_members() {
  local expected="$1" deadline=$((SECONDS + 180)) description=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    description="$(rpk_admin group describe "${CONNECT_GROUP}" 2>/dev/null || true)"
    if awk -v expected="${expected}" '
      $1 == "STATE" {stable = ($2 == "Stable")}
      $1 == "MEMBERS" {members = $2}
      END {exit stable && members == expected ? 0 : 1}
    ' <<<"${description}"; then
      printf '%s\n' "${description}"
      return 0
    fi
    sleep 2
  done
  printf '%s\n' "${description}" >&2
  return 1
}

wait_replica_count() {
	local topic="$1" expected="$2" deadline=$((SECONDS + 180)) description count
	while [ "${SECONDS}" -lt "${deadline}" ]; do
		description="$(rpk_admin topic describe "${topic}" --print-partitions --format json 2>/dev/null || true)"
		count="$(jq -r '.[0].partitions[0].replicas | length' <<<"${description}" 2>/dev/null || echo 0)"
    if [ "${count:-0}" -eq "${expected}" ]; then
      printf '%s\n' "${description}"
      return 0
    fi
    sleep 2
  done
  return 1
}

kill_broker() {
	local service="$1" container="addp-${1}"
	docker update --restart=no "${container}" >/dev/null
	docker kill --signal KILL "${container}" >/dev/null
	[ "$(docker inspect --format '{{.State.Running}}' "${container}")" = false ]
}

start_broker() {
	local service="$1" container="addp-${1}"
	docker update --restart=unless-stopped "${container}" >/dev/null
	"${COMPOSE[@]}" start "${service}"
	wait_container_health "${container}"
}

wait_topic_under_replicated() {
  local topic="$1" deadline=$((SECONDS + 180)) health=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    health="$(docker exec addp-redpanda rpk cluster health \
      -X brokers=localhost:29092 -X user="${ADMIN_USERNAME}" -X pass="${ADMIN_PASSWORD}" \
      -X sasl.mechanism=SCRAM-SHA-256 -X tls.enabled=true -X tls.ca=/etc/redpanda/certs/ca.crt \
      2>/dev/null || true)"
    if grep -Fq "kafka/${topic}/0" <<<"${health}" &&
      grep -q 'Leaderless partitions (0)' <<<"${health}"; then
      printf '%s\n' "${health}"
      return 0
    fi
    sleep 2
  done
  printf '%s\n' "${health}" >&2
  return 1
}

cleanup_stale_certification_resources() {
	local topic group
	while IFS= read -r topic; do
		[[ "${topic}" =~ ^__addp_cdc\.cert\.ha\.(load|connect|retention|perf)\. ]] || continue
		rpk_admin topic delete "${topic}" >/dev/null
	done < <(rpk_admin topic list --format json | jq -r '.[].name')
	while IFS= read -r group; do
		[[ "${group}" =~ ^__addp_cdc_consumer\.cert\.ha\. ]] || continue
		rpk_admin group delete "${group}" >/dev/null
	done < <(rpk_admin group list | awk 'NR > 1 {print $2}')
}

cleanup_probe() {
  set +e
  curl -sf -X DELETE "http://localhost:18083/connectors/${CONNECTOR}" >/dev/null
  curl -sf -X DELETE "http://localhost:18084/connectors/${CONNECTOR}" >/dev/null
  sleep 1
  docker exec business-postgres psql -U "${BUSINESS_PG_USER:-business}" -d "${BUSINESS_PG_DB:-business}" -v ON_ERROR_STOP=0 -c \
    "SELECT pg_drop_replication_slot('${SLOT}') WHERE EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name='${SLOT}' AND NOT active); DROP PUBLICATION IF EXISTS \"${PUBLICATION}\"; DROP SCHEMA IF EXISTS \"${SOURCE_SCHEMA}\" CASCADE;" >/dev/null 2>&1
  rpk_admin group delete "${LOAD_GROUP}" >/dev/null 2>&1
  rpk_admin topic delete "${LOAD_TOPIC}" "${RETENTION_TOPIC}" "${PERF_TOPIC}" "${DATA_TOPIC}" >/dev/null 2>&1
}

remove_ha_volumes() {
  local logical_name volume_name
  for logical_name in redpanda_ha_0_data redpanda_ha_1_data redpanda_ha_2_data; do
    while IFS= read -r volume_name; do
      [ -z "${volume_name}" ] || docker volume rm "${volume_name}" >/dev/null
    done < <(docker volume ls -q \
      --filter label=com.docker.compose.project=addp-infra \
      --filter "label=com.docker.compose.volume=${logical_name}")
  done
}

restore_default_profile() {
  set +e
  "${COMPOSE[@]}" stop kafka-connect-2 redpanda-2 redpanda-3 >/dev/null 2>&1
  docker container rm -f addp-kafka-connect-2 addp-redpanda-2 addp-redpanda-3 addp-redpanda-init >/dev/null 2>&1
  INFRA_KAFKA_VERIFY_ACL=1 "${DEFAULT_COMPOSE[@]}" up -d --force-recreate redpanda redpanda-init kafka-connect \
    >"${REPORT_DIR}/default-restore.log" 2>&1
  wait_container_health addp-redpanda
  wait_connect http://localhost:18083
  remove_ha_volumes
}

finish() {
  local status=$?
  trap - EXIT
  set +e
  if [ "${status}" -eq 0 ] && ! grep -Fxq 'result=passed' "${REPORT_DIR}/result.txt" 2>/dev/null; then
    echo "HA certification ended before writing a passing result." >&2
    status=1
  fi
  if [ "${status}" -ne 0 ]; then
    for container in addp-redpanda addp-redpanda-2 addp-redpanda-3 addp-kafka-connect addp-kafka-connect-2 addp-redpanda-init; do
      docker logs --tail=300 "${container}" >"${REPORT_DIR}/${container}.log" 2>&1
    done
  fi
	if [ "${ha_started}" -eq 1 ]; then
		"${COMPOSE[@]}" unpause redpanda-2 redpanda-3 >/dev/null 2>&1
		docker update --restart=unless-stopped addp-redpanda addp-redpanda-2 addp-redpanda-3 >/dev/null 2>&1
		"${COMPOSE[@]}" start redpanda redpanda-2 redpanda-3 >/dev/null 2>&1
    wait_container_health addp-redpanda
    wait_container_health addp-redpanda-2
    wait_container_health addp-redpanda-3
    cleanup_probe
  fi
  restore_default_profile
  if [ "${status}" -eq 0 ]; then
    printf 'default_profile_restored=redpanda\n' >>"${REPORT_DIR}/result.txt"
  fi
  find "${CERT_DIR}" -type f -delete
  rmdir "${CERT_DIR}" 2>/dev/null
  unlink "${CONNECT_CONFIG}" 2>/dev/null
  exit "${status}"
}
trap finish EXIT

existing_connectors="$(curl -sf http://localhost:18083/connectors 2>/dev/null || echo '[]')"
if [ "$(jq 'length' <<<"${existing_connectors}")" -ne 0 ]; then
  echo "refusing HA certification while Kafka Connect has active connectors: ${existing_connectors}" >&2
  exit 1
fi

generate_certificates
write_perf_client_config

echo "[RP-HA-01] static 3 broker / 2 Connect topology"
"${COMPOSE[@]}" config >"${REPORT_DIR}/compose-redpanda-ha.yml"
[ "$("${COMPOSE[@]}" config --services | grep -Ec '^redpanda(-2|-3)?$')" -eq 3 ]
[ "$("${COMPOSE[@]}" config --services | grep -Ec '^kafka-connect(-2)?$')" -eq 2 ]
rg -q 'REDPANDA_CERT_DIR is required' docker-compose.infra.ha.yml
if rg -n 'image: apache/kafka|container_name: addp-kafka$|^[[:space:]]+kafka:$|kafka-init' \
  "${REPORT_DIR}/compose-redpanda-ha.yml"; then
  echo "obsolete Apache Kafka deployment path remains" >&2
  exit 1
fi

echo "[RP-HA-01/RP-HA-02] start HA/SASL_SSL profile"
remove_ha_volumes
ha_started=1
INFRA_KAFKA_VERIFY_ACL=1 "${COMPOSE[@]}" up -d --force-recreate \
  redpanda redpanda-2 redpanda-3 redpanda-init kafka-connect kafka-connect-2
wait_container_health addp-redpanda
wait_container_health addp-redpanda-2
wait_container_health addp-redpanda-3
wait_cluster_healthy
wait_connect http://localhost:18083
wait_connect http://localhost:18084
cleanup_stale_certification_resources
docker exec addp-redpanda rpk cluster config get write_caching_default \
	-X brokers=localhost:29092 -X user="${ADMIN_USERNAME}" -X pass="${ADMIN_PASSWORD}" \
	-X sasl.mechanism=SCRAM-SHA-256 -X tls.enabled=true -X tls.ca=/etc/redpanda/certs/ca.crt |
	tee "${REPORT_DIR}/write-caching-default.txt"
jq -e '. == false or . == "false"' "${REPORT_DIR}/write-caching-default.txt" >/dev/null
docker inspect addp-kafka-connect --format '{{range .Config.Env}}{{println .}}{{end}}' |
	tee "${REPORT_DIR}/connect-worker-env.txt" >/dev/null
rg -q '^CONNECT_PRODUCER_ACKS=all$' "${REPORT_DIR}/connect-worker-env.txt"
rg -q '^CONNECT_PRODUCER_ENABLE_IDEMPOTENCE=true$' "${REPORT_DIR}/connect-worker-env.txt"
rg -q '^CONNECT_SCHEDULED_REBALANCE_MAX_DELAY_MS=10000$' "${REPORT_DIR}/connect-worker-env.txt"
docker exec addp-redpanda rpk cluster info \
  -X brokers=localhost:29092 -X user="${ADMIN_USERNAME}" -X pass="${ADMIN_PASSWORD}" \
  -X sasl.mechanism=SCRAM-SHA-256 -X tls.enabled=true -X tls.ca=/etc/redpanda/certs/ca.crt |
  tee "${REPORT_DIR}/cluster-info.txt"
awk '$1 ~ /^[0-9]+\*?$/ && $2 ~ /^redpanda(-[23])?$/ {count++} END {exit count == 3 ? 0 : 1}' \
  "${REPORT_DIR}/cluster-info.txt"

echo "[RP-HA-02] TLS trust, hostname, and credential negative gates"
rpk_admin topic list >/dev/null
echo "trusted CA and valid credential accepted"
set +e
rpk_as "${ADMIN_USERNAME}" "${ADMIN_PASSWORD}" redpanda:29092 untrusted \
  topic list >/dev/null 2>&1
missing_ca_status=$?
set -e
if [ "${missing_ca_status}" -eq 0 ]; then
  echo "SASL_SSL unexpectedly succeeded without the certification CA" >&2
  exit 1
fi
echo "missing CA rejected with status ${missing_ca_status}"
set +e
rpk_as "${ADMIN_USERNAME}" wrong-password redpanda:29092 trusted \
  topic list >/dev/null 2>&1
wrong_credential_status=$?
set -e
if [ "${wrong_credential_status}" -eq 0 ]; then
  echo "SASL_SSL unexpectedly succeeded with a wrong password" >&2
  exit 1
fi
echo "wrong credential rejected with status ${wrong_credential_status}"
set +e
rpk_wrong_host topic list >/dev/null 2>&1
wrong_hostname_status=$?
set -e
if [ "${wrong_hostname_status}" -eq 0 ]; then
  echo "TLS hostname verification unexpectedly accepted wrong-redpanda" >&2
  exit 1
fi
echo "wrong TLS hostname rejected with status ${wrong_hostname_status}"

echo "[RP-HA-01/RP-HA-04] RF=3/acks=all topic and single-broker hard failover"
rpk_admin topic create "${LOAD_TOPIC}" --partitions 1 --replicas 3 \
  --topic-config cleanup.policy=delete >/dev/null
rpk_admin topic describe "${LOAD_TOPIC}" --print-configs |
	tee "${REPORT_DIR}/load-topic-config.txt"
rg -q 'cleanup.policy.*delete' "${REPORT_DIR}/load-topic-config.txt"
docker exec addp-redpanda rpk topic describe "${LOAD_TOPIC}" \
	-X brokers=localhost:29092 -X user="${ADMIN_USERNAME}" -X pass="${ADMIN_PASSWORD}" \
	-X sasl.mechanism=SCRAM-SHA-256 -X tls.enabled=true -X tls.ca=/etc/redpanda/certs/ca.crt |
	tee "${REPORT_DIR}/load-topic-redpanda-config.txt"
awk '$1 == "write.caching" && $2 == "false" {found=1} END {exit found ? 0 : 1}' \
	"${REPORT_DIR}/load-topic-redpanda-config.txt"
wait_replica_count "${LOAD_TOPIC}" 3 | tee "${REPORT_DIR}/load-topic-replicas-initial.txt"
(
  for sequence in $(seq 1 "${LOAD_MESSAGES}"); do
    printf '%08d\n' "${sequence}"
    if [ $((sequence % 20)) -eq 0 ]; then sleep 0.01; fi
  done
) | RPK_ATTACH_STDIN=1 rpk_connect topic produce "${LOAD_TOPIC}" --acks=-1 --delivery-timeout 30s \
  >"${REPORT_DIR}/load-producer.log" 2>&1 &
producer_pid=$!
sleep 1
kill_broker redpanda-2
wait "${producer_pid}"
wait_topic_under_replicated "${LOAD_TOPIC}" | tee "${REPORT_DIR}/load-topic-under-replicated.txt"
rpk_transfer topic consume "${LOAD_TOPIC}" --group "${LOAD_GROUP}" \
  --offset start --num "${LOAD_MESSAGES}" --format '%v\n' |
  rg '^[0-9]{8}$' >"${REPORT_DIR}/load-consumed.txt"
[ "$(wc -l <"${REPORT_DIR}/load-consumed.txt" | tr -d ' ')" -eq "${LOAD_MESSAGES}" ]
awk '{if (($0 + 0) != NR) exit 1}' "${REPORT_DIR}/load-consumed.txt"
start_broker redpanda-2
wait_cluster_healthy
wait_replica_count "${LOAD_TOPIC}" 3 | tee "${REPORT_DIR}/load-topic-replicas-recovered.txt"
[ "$(topic_offset "${LOAD_TOPIC}" latest)" -eq "${LOAD_MESSAGES}" ]

echo "[RP-HA-05] quorum boundary rejects writes after two broker failures"
quorum_before_offset="$(topic_offset "${LOAD_TOPIC}" latest)"
"${COMPOSE[@]}" pause redpanda-2 redpanda-3
set +e
printf '%s\n' quorum-must-fail | RPK_ATTACH_STDIN=1 rpk_as connect "${CONNECT_PASSWORD}" redpanda:29092 trusted \
  topic produce "${LOAD_TOPIC}" --acks=-1 --delivery-timeout 8s \
  >"${REPORT_DIR}/quorum-failure.log" 2>&1
quorum_probe_status=$?
set -e
printf 'exit_status=%s\n' "${quorum_probe_status}" >>"${REPORT_DIR}/quorum-failure.log"
if [ "${quorum_probe_status}" -eq 0 ]; then
  echo "acks=all write unexpectedly succeeded without broker quorum" >&2
  exit 1
fi
"${COMPOSE[@]}" unpause redpanda-2 redpanda-3
wait_container_health addp-redpanda-2
wait_container_health addp-redpanda-3
wait_cluster_healthy
wait_replica_count "${LOAD_TOPIC}" 3 >/dev/null
quorum_after_offset="$(topic_offset "${LOAD_TOPIC}" latest)"
printf 'before_offset=%s\nafter_offset=%s\n' "${quorum_before_offset}" "${quorum_after_offset}" \
	>>"${REPORT_DIR}/quorum-failure.log"
[ "${quorum_before_offset}" -eq "${LOAD_MESSAGES}" ]
[ "${quorum_after_offset}" -eq "${quorum_before_offset}" ]

echo "[RP-HA-03] Connect distributed ownership and worker failover"
docker exec business-postgres psql -U "${BUSINESS_PG_USER:-business}" -d "${BUSINESS_PG_DB:-business}" -v ON_ERROR_STOP=1 -c \
  "CREATE SCHEMA \"${SOURCE_SCHEMA}\"; CREATE TABLE \"${SOURCE_SCHEMA}\".\"${SOURCE_TABLE}\" (id bigint PRIMARY KEY, name text NOT NULL); INSERT INTO \"${SOURCE_SCHEMA}\".\"${SOURCE_TABLE}\" VALUES (1, 'snapshot');" >/dev/null
rpk_admin topic create "${DATA_TOPIC}" --partitions 1 --replicas 3 \
  --topic-config cleanup.policy=delete >/dev/null
jq -n \
  --arg connector "${CONNECTOR}" --arg schema "${SOURCE_SCHEMA}" --arg table "${SOURCE_TABLE}" \
  --arg topic "${DATA_TOPIC}" --arg slot "${SLOT}" --arg publication "${PUBLICATION}" \
  --arg user "${BUSINESS_PG_USER:-business}" --arg password "${BUSINESS_PG_PASSWORD:-business_password}" \
  '{
    "connector.class":"io.debezium.connector.postgresql.PostgresConnector", "tasks.max":"1",
    "database.hostname":"host.docker.internal", "database.port":"5433", "database.user":$user,
    "database.password":$password, "database.dbname":"business", "topic.prefix":$connector,
    "plugin.name":"pgoutput", "slot.name":$slot, "publication.name":$publication,
    "publication.autocreate.mode":"filtered", "snapshot.mode":"initial", "schema.include.list":$schema,
    "table.include.list":($schema + "." + $table), "key.converter":"org.apache.kafka.connect.json.JsonConverter",
    "key.converter.schemas.enable":"false", "value.converter":"org.apache.kafka.connect.json.JsonConverter",
    "value.converter.schemas.enable":"false", "transforms":"route",
    "transforms.route.type":"org.apache.kafka.connect.transforms.RegexRouter", "transforms.route.regex":".*",
    "transforms.route.replacement":$topic, "tombstones.on.delete":"false", "slot.drop.on.stop":"false"
  }' >"${REPORT_DIR}/connector-config.json"
curl -sf -X PUT -H 'Content-Type: application/json' --data @"${REPORT_DIR}/connector-config.json" \
  "http://localhost:18083/connectors/${CONNECTOR}/config" >/dev/null
wait_connector_running http://localhost:18083
wait_topic_latest_greater_than \
  "${DATA_TOPIC}" 0 180 "connector initial snapshot was not emitted before worker failover" >/dev/null
status_before="$(curl -sf "http://localhost:18083/connectors/${CONNECTOR}/status")"
worker_before="$(jq -r '.connector.worker_id' <<<"${status_before}")"
printf '%s\n' "${status_before}" >"${REPORT_DIR}/connector-before-worker-failure.json"
if [[ "${worker_before}" == kafka-connect-2:* ]]; then
  owner_service=kafka-connect-2
  survivor_url=http://localhost:18083
else
  owner_service=kafka-connect
  survivor_url=http://localhost:18084
fi
"${COMPOSE[@]}" stop "${owner_service}"
wait_connector_running "${survivor_url}"
status_after="$(curl -sf "${survivor_url}/connectors/${CONNECTOR}/status")"
worker_after="$(jq -r '.connector.worker_id' <<<"${status_after}")"
[ "${worker_after}" != "${worker_before}" ]
printf '%s\n' "${status_after}" >"${REPORT_DIR}/connector-after-worker-failure.json"
"${COMPOSE[@]}" start "${owner_service}"
wait_connect http://localhost:18083
wait_connect http://localhost:18084
before_resume_offset="$(topic_offset "${DATA_TOPIC}" latest)"
docker exec business-postgres psql -U "${BUSINESS_PG_USER:-business}" -d "${BUSINESS_PG_DB:-business}" -v ON_ERROR_STOP=1 -c \
  "INSERT INTO \"${SOURCE_SCHEMA}\".\"${SOURCE_TABLE}\" VALUES (2, 'after-worker-failover');" >/dev/null
wait_topic_latest_greater_than \
  "${DATA_TOPIC}" "${before_resume_offset}" 60 "connector did not resume after worker failover" >/dev/null
for container in addp-kafka-connect addp-kafka-connect-2; do
	(docker logs "${container}" 2>&1 |
		rg "groupId=${CONNECT_GROUP}.*Successfully joined group" |
		tail -n 1 || true) >>"${REPORT_DIR}/connect-group-members.txt"
done
wait_connect_group_members 2 >"${REPORT_DIR}/connect-group-rpk.txt"

echo "[RP-HA-06] time/bytes retention advances the earliest offset"
rpk_admin topic create "${RETENTION_TOPIC}" --partitions 1 --replicas 3 \
  --topic-config cleanup.policy=delete --topic-config retention.ms=2000 \
  --topic-config retention.bytes=4096 --topic-config segment.bytes=1048576 \
  --topic-config segment.ms=1000 >/dev/null
for batch in $(seq 1 6); do
	awk -v batch="${batch}" 'BEGIN { payload=sprintf("%02048d", 0); for (i=1; i<=200; i++) print batch "-" i "-" payload }' |
		RPK_ATTACH_STDIN=1 rpk_connect topic produce "${RETENTION_TOPIC}" --acks=-1 --compression none >/dev/null
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
rpk_admin topic describe "${RETENTION_TOPIC}" --print-all |
	tee "${REPORT_DIR}/retention-topic.txt"
printf 'earliest=%s latest=%s\n' "${retention_earliest}" "${retention_latest}" |
	tee "${REPORT_DIR}/retention-offsets.txt"

echo "[RP-HA-06] existing CDC/DLQ/cleanup contracts over SASL_SSL and RF=3/quorum=2"
export ADDP_TEST_POSTGRES_DATABASE="${TEST_DATABASE}"
export ADDP_TEST_INFRA_KAFKA_BOOTSTRAP_SERVERS="${HOST_BOOTSTRAP_SERVERS}"
export ADDP_TEST_INFRA_KAFKA_SECURITY_PROTOCOL=sasl_ssl
export ADDP_TEST_INFRA_KAFKA_SASL_MECHANISM=scram-sha-256
export ADDP_TEST_INFRA_KAFKA_TLS_CA_CERT_FILE="${CERT_DIR}/ca.crt"
export ADDP_TEST_INFRA_KAFKA_REPLICATION_FACTOR=3
export ADDP_TEST_KAFKA_CONNECT_BOOTSTRAP_SERVERS=redpanda:29092,redpanda-2:29092,redpanda-3:29092
export ADDP_TEST_KAFKA_CONNECT_SECURITY_PROTOCOL=sasl_ssl
export ADDP_TEST_KAFKA_SECURITY_PROTOCOL=sasl_ssl
export ADDP_TEST_KAFKA_SASL_MECHANISM=scram-sha-256
export ADDP_TEST_KAFKA_TLS_CA_CERT="$(<"${CERT_DIR}/ca.crt")"
(
  cd transfer/backend
  ADDP_CDC_CONTROL_E2E=1 go test ./internal/capture -run TestIntegrationPostgreSQLCaptureControlLifecycle -count=1 -v
  ADDP_CDC_DATA_E2E=1 go test ./internal/continuous -run TestIntegrationPostgreSQLCDCDataPlaneViaPublicAPISnapshotUpdateDeleteCrashResumeAndStopCleanup -count=1 -v
  ADDP_MYSQL_CDC_DATA_E2E=1 go test ./internal/continuous -run TestIntegrationMySQLCDCDataPlaneViaPublicAPIFullLifecycle -count=1 -v
	ADDP_DLQ_KAFKA_INTEGRATION=1 go test ./internal/deadletter -run TestIntegrationKafkaPayloadWriterCreatesAndWritesTransferDLQTopic -count=1 -v
	ADDP_CONTINUOUS_DLQ_E2E=1 go test ./internal/continuous -run TestIntegrationContinuousKafkaDeadLetterSkipAndCAS -count=1 -v
	ADDP_TEST_KAFKA_USERNAME="${ADMIN_USERNAME}" ADDP_TEST_KAFKA_PASSWORD="${ADMIN_PASSWORD}" \
		ADDP_CONTINUOUS_E2E=1 go test ./internal/continuous -run TestIntegrationContinuousKafkaRetentionHealthTransitions -count=1 -v
) | tee "${REPORT_DIR}/addp-ha-e2e.log"

echo "[RP-HA-07] fixed producer load and resource samples"
rpk_admin topic create "${PERF_TOPIC}" --partitions 1 --replicas 3 \
  --topic-config cleanup.policy=delete >/dev/null
perf_client /kafka/bin/kafka-producer-perf-test.sh \
	--bootstrap-server redpanda:29092,redpanda-2:29092,redpanda-3:29092 \
	--topic "${PERF_TOPIC}" --num-records "${PERF_MESSAGES}" --record-size 1024 \
	--throughput "${PERF_THROUGHPUT}" --command-config /tmp/connect.properties \
	--command-property acks=all enable.idempotence=true \
	>"${REPORT_DIR}/producer-perf.txt" 2>&1 &
perf_pid=$!
while kill -0 "${perf_pid}" 2>/dev/null; do
	date -u +%Y-%m-%dT%H:%M:%SZ
	docker stats --no-stream addp-redpanda addp-redpanda-2 addp-redpanda-3 addp-kafka-connect addp-kafka-connect-2
	sleep 2
done >"${REPORT_DIR}/resources-under-load.txt"
wait "${perf_pid}"
cat "${REPORT_DIR}/producer-perf.txt"
rg -q '95th.*99th' "${REPORT_DIR}/producer-perf.txt"

echo "[RP-HA-07/RP-HA-08] resource and support-scope gates"
docker stats --no-stream addp-redpanda addp-redpanda-2 addp-redpanda-3 addp-kafka-connect addp-kafka-connect-2 |
  tee "${REPORT_DIR}/resources-final.txt"
docker inspect addp-redpanda addp-redpanda-2 addp-redpanda-3 addp-kafka-connect addp-kafka-connect-2 \
  --format '{{.Name}} restart_count={{.RestartCount}} oom={{.State.OOMKilled}}' |
  tee "${REPORT_DIR}/container-state.txt"
if rg -i 'redpanda|kafka-connect-2' common transfer gateway system \
  --glob '!**/*.md' --glob '!**/*test.go' \
  --glob '!transfer/backend/internal/config/config.go' --glob '!**/node_modules/**'; then
  echo "HA topology leaked outside deployment/certification code" >&2
  exit 1
fi
rg -n 'Infra Kafka.*Redpanda v24.3.18|Redpanda v24.3.18.*Infra Kafka' \
  docs/spec/addp技术栈规约.md docs/spec/addp配置介绍.md >"${REPORT_DIR}/formal-support-scope.txt"
if rg -n 'apache/kafka|addp-kafka-init|addp-kafka$|^[[:space:]]+kafka:$|kafka:29092|INFRA_KAFKA_CLI_IMAGE' \
  docker-compose.infra.yml docker-compose.infra.ha.yml scripts/infra .env.example; then
  echo "obsolete Apache Kafka deployment path remains" >&2
  exit 1
fi

cat >"${REPORT_DIR}/result.txt" <<EOF
result=passed
profile=redpanda-ha
broker_services=redpanda,redpanda-2,redpanda-3
broker_containers=addp-redpanda,addp-redpanda-2,addp-redpanda-3
init_container=addp-redpanda-init
broker_count=3
connect_worker_count=2
replication_factor=3
required_acks=all
confirmation_quorum=2
write_caching=false
security_protocol=SASL_SSL
sasl_mechanism=SCRAM-SHA-256
load_messages=${LOAD_MESSAGES}
perf_messages=${PERF_MESSAGES}
perf_record_bytes=1024
perf_target_messages_per_second=${PERF_THROUGHPUT}
retention_earliest=${retention_earliest}
retention_latest=${retention_latest}
load_sequence_complete=true
single_broker_hard_failover=true
double_broker_write_rejected=true
connect_worker_failover=true
completed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF

echo "Infra Kafka HA certification passed. Evidence: ${REPORT_DIR}"
