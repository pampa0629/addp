#!/usr/bin/env bash

set -euo pipefail

BOOTSTRAP_SERVER="kafka:29092"
KAFKA_BIN="${KAFKA_BIN:-/opt/kafka/bin}"
ADMIN_CONFIG="$(mktemp)"
CONNECT_CONFIG="$(mktemp)"
TRANSFER_CONFIG="$(mktemp)"
VERIFY_TOPIC=""

cleanup() {
  if [ -n "${VERIFY_TOPIC}" ]; then
    "${KAFKA_BIN}/kafka-topics.sh" \
      --bootstrap-server "${BOOTSTRAP_SERVER}" \
      --command-config "${ADMIN_CONFIG}" \
      --delete --if-exists --topic "${VERIFY_TOPIC}" >/dev/null 2>&1 || true
  fi
  rm -f "${ADMIN_CONFIG}" "${CONNECT_CONFIG}" "${TRANSFER_CONFIG}"
}
trap cleanup EXIT

write_client_config() {
  local path="$1"
  local username="$2"
  local password="$3"
  umask 077
  {
    echo "security.protocol=SASL_PLAINTEXT"
    echo "sasl.mechanism=PLAIN"
    echo "sasl.jaas.config=org.apache.kafka.common.security.plain.PlainLoginModule required username=\"${username}\" password=\"${password}\";"
  } >"${path}"
}

write_client_config "${ADMIN_CONFIG}" "admin" "${INFRA_KAFKA_ADMIN_PASSWORD}"
write_client_config "${CONNECT_CONFIG}" "connect" "${INFRA_KAFKA_CONNECT_PASSWORD}"
write_client_config "${TRANSFER_CONFIG}" "transfer" "${INFRA_KAFKA_TRANSFER_PASSWORD}"

create_compacted_topic() {
  local topic="$1"
  local partitions="$2"
  "${KAFKA_BIN}/kafka-topics.sh" \
    --bootstrap-server "${BOOTSTRAP_SERVER}" \
    --command-config "${ADMIN_CONFIG}" \
    --create --if-not-exists \
    --topic "${topic}" \
    --partitions "${partitions}" \
    --replication-factor 1 \
    --config cleanup.policy=compact \
    --config min.cleanable.dirty.ratio=0.1
}

create_compacted_topic "${KAFKA_CONNECT_CONFIG_TOPIC}" 1
create_compacted_topic "${KAFKA_CONNECT_OFFSET_TOPIC}" 25
create_compacted_topic "${KAFKA_CONNECT_STATUS_TOPIC}" 5

grant_topic_operations() {
  local principal="$1"
  local topic="$2"
  shift 2
  local args=()
  local operation
  for operation in "$@"; do
    args+=(--operation "${operation}")
  done
  "${KAFKA_BIN}/kafka-acls.sh" \
    --bootstrap-server "${BOOTSTRAP_SERVER}" \
    --command-config "${ADMIN_CONFIG}" \
    --add --allow-principal "User:${principal}" \
    --topic "${topic}" \
    "${args[@]}"
}

for topic in "${KAFKA_CONNECT_CONFIG_TOPIC}" "${KAFKA_CONNECT_OFFSET_TOPIC}" "${KAFKA_CONNECT_STATUS_TOPIC}"; do
  grant_topic_operations connect "${topic}" Read Write Describe
done

"${KAFKA_BIN}/kafka-acls.sh" \
  --bootstrap-server "${BOOTSTRAP_SERVER}" \
  --command-config "${ADMIN_CONFIG}" \
  --add --allow-principal User:connect \
  --group "${KAFKA_CONNECT_GROUP_ID}" \
  --operation Read --operation Describe

"${KAFKA_BIN}/kafka-acls.sh" \
  --bootstrap-server "${BOOTSTRAP_SERVER}" \
  --command-config "${ADMIN_CONFIG}" \
  --add --allow-principal User:connect \
  --cluster --operation Describe

"${KAFKA_BIN}/kafka-acls.sh" \
  --bootstrap-server "${BOOTSTRAP_SERVER}" \
  --command-config "${ADMIN_CONFIG}" \
  --add --allow-principal User:connect \
  --resource-pattern-type prefixed \
  --topic "${INFRA_KAFKA_CDC_TOPIC_PREFIX}" \
  --operation Write --operation Describe

"${KAFKA_BIN}/kafka-acls.sh" \
  --bootstrap-server "${BOOTSTRAP_SERVER}" \
  --command-config "${ADMIN_CONFIG}" \
  --add --allow-principal User:transfer \
  --resource-pattern-type prefixed \
  --topic "${INFRA_KAFKA_CDC_TOPIC_PREFIX}" \
  --operation Read --operation Describe

"${KAFKA_BIN}/kafka-acls.sh" \
  --bootstrap-server "${BOOTSTRAP_SERVER}" \
  --command-config "${ADMIN_CONFIG}" \
  --add --allow-principal User:transfer \
  --resource-pattern-type prefixed \
  --group "${INFRA_KAFKA_CDC_CONSUMER_GROUP_PREFIX}" \
  --operation Read --operation Describe

"${KAFKA_BIN}/kafka-topics.sh" --bootstrap-server "${BOOTSTRAP_SERVER}" --command-config "${CONNECT_CONFIG}" --list >/dev/null
"${KAFKA_BIN}/kafka-topics.sh" --bootstrap-server "${BOOTSTRAP_SERVER}" --command-config "${TRANSFER_CONFIG}" --list >/dev/null

if [ "${VERIFY_ACL:-0}" = "1" ]; then
  VERIFY_TOPIC="${INFRA_KAFKA_CDC_TOPIC_PREFIX}verify.$(date +%s)"
  VERIFY_GROUP="${INFRA_KAFKA_CDC_CONSUMER_GROUP_PREFIX}verify.$(date +%s)"
  VERIFY_MESSAGE='{"id":1,"source":"acl-verification"}'

  "${KAFKA_BIN}/kafka-topics.sh" \
    --bootstrap-server "${BOOTSTRAP_SERVER}" \
    --command-config "${ADMIN_CONFIG}" \
    --create --topic "${VERIFY_TOPIC}" --partitions 1 --replication-factor 1 \
    --config cleanup.policy=delete --config retention.ms=600000 >/dev/null

  printf '%s\n' "${VERIFY_MESSAGE}" | "${KAFKA_BIN}/kafka-console-producer.sh" \
    --bootstrap-server "${BOOTSTRAP_SERVER}" \
    --producer.config "${CONNECT_CONFIG}" \
    --topic "${VERIFY_TOPIC}" >/dev/null

  set +e
  consumed=$("${KAFKA_BIN}/kafka-console-consumer.sh" \
    --bootstrap-server "${BOOTSTRAP_SERVER}" \
    --command-config "${TRANSFER_CONFIG}" \
    --group "${VERIFY_GROUP}" \
    --topic "${VERIFY_TOPIC}" \
    --from-beginning --max-messages 1 --timeout-ms 10000 2>&1)
  consumer_status=$?
  set -e
  if [ "${consumer_status}" -ne 0 ] || ! printf '%s\n' "${consumed}" | grep -Fxq "${VERIFY_MESSAGE}"; then
    echo "Transfer principal did not consume the expected CDC record (status=${consumer_status})." >&2
    echo "Consumer output: ${consumed}" >&2
    exit 1
  fi

  offset_before=$("${KAFKA_BIN}/kafka-get-offsets.sh" \
    --bootstrap-server "${BOOTSTRAP_SERVER}" \
    --command-config "${ADMIN_CONFIG}" \
    --topic "${VERIFY_TOPIC}" --time latest | tail -n 1)

  set +e
  printf '%s\n' '{"id":2}' | "${KAFKA_BIN}/kafka-console-producer.sh" \
    --bootstrap-server "${BOOTSTRAP_SERVER}" \
    --producer.config "${TRANSFER_CONFIG}" \
    --producer-property delivery.timeout.ms=5000 \
    --producer-property request.timeout.ms=3000 \
    --producer-property max.block.ms=5000 \
    --topic "${VERIFY_TOPIC}" >/dev/null 2>&1
  set -e
  sleep 1

  offset_after=$("${KAFKA_BIN}/kafka-get-offsets.sh" \
    --bootstrap-server "${BOOTSTRAP_SERVER}" \
    --command-config "${ADMIN_CONFIG}" \
    --topic "${VERIFY_TOPIC}" --time latest | tail -n 1)
  if [ "${offset_after}" != "${offset_before}" ]; then
    echo "Transfer principal unexpectedly advanced the Infra CDC topic: ${offset_before} -> ${offset_after}." >&2
    exit 1
  fi

  if "${KAFKA_BIN}/kafka-topics.sh" \
    --bootstrap-server "${BOOTSTRAP_SERVER}" \
    --command-config "${TRANSFER_CONFIG}" \
    --describe --topic "${KAFKA_CONNECT_CONFIG_TOPIC}" >/dev/null 2>&1; then
    echo "Transfer principal unexpectedly accessed a Kafka Connect internal topic." >&2
    exit 1
  fi

  echo "Infra Kafka ACL verification passed."
fi

echo "Infra Kafka internal topics and ACLs are ready."
