#!/usr/bin/env bash

set -euo pipefail

BROKERS="${INFRA_KAFKA_BOOTSTRAP_SERVERS:-redpanda:29092}"
ADMIN_HOSTS="${REDPANDA_ADMIN_HOSTS:-redpanda:9644}"
ADMIN_USERNAME="${INFRA_KAFKA_ADMIN_USERNAME:-admin}"
SASL_MECHANISM="${INFRA_KAFKA_SASL_MECHANISM:-SCRAM-SHA-256}"
SASL_MECHANISM="${SASL_MECHANISM^^}"
SECURITY_PROTOCOL="${INFRA_KAFKA_SECURITY_PROTOCOL:-SASL_PLAINTEXT}"
SECURITY_PROTOCOL="${SECURITY_PROTOCOL^^}"
INTERNAL_REPLICATION_FACTOR="${INFRA_KAFKA_INTERNAL_REPLICATION_FACTOR:-1}"
VERIFY_TOPIC=""
VERIFY_DLQ_TOPIC=""

if [ "${SASL_MECHANISM}" != "SCRAM-SHA-256" ]; then
  echo "Infra Kafka SASL mechanism must be SCRAM-SHA-256, got ${SASL_MECHANISM}" >&2
  exit 1
fi
if [ "${SECURITY_PROTOCOL}" != "SASL_PLAINTEXT" ] && [ "${SECURITY_PROTOCOL}" != "SASL_SSL" ]; then
  echo "Infra Kafka security protocol must be SASL_PLAINTEXT or SASL_SSL, got ${SECURITY_PROTOCOL}" >&2
  exit 1
fi

ADMIN_API_ARGS=(
  -X "admin.hosts=${ADMIN_HOSTS}"
  -X "brokers=${BROKERS}"
  -X "user=${ADMIN_USERNAME}"
  -X "pass=${INFRA_KAFKA_ADMIN_PASSWORD}"
  -X sasl.mechanism=SCRAM-SHA-256
)

client_args() {
  local username="$1"
  local password="$2"
  CLIENT_ARGS=(
    -X "brokers=${BROKERS}"
    -X "user=${username}"
    -X "pass=${password}"
    -X sasl.mechanism=SCRAM-SHA-256
  )
  if [ "${SECURITY_PROTOCOL}" = "SASL_SSL" ]; then
    if [ -z "${INFRA_KAFKA_TLS_CA_CERT_FILE:-}" ]; then
      echo "INFRA_KAFKA_TLS_CA_CERT_FILE is required for SASL_SSL" >&2
      exit 1
    fi
    CLIENT_ARGS+=(
      -X tls.enabled=true
      -X "tls.ca=${INFRA_KAFKA_TLS_CA_CERT_FILE}"
    )
  fi
}

client_args "${ADMIN_USERNAME}" "${INFRA_KAFKA_ADMIN_PASSWORD}"
ADMIN_KAFKA_ARGS=("${CLIENT_ARGS[@]}")
client_args connect "${INFRA_KAFKA_CONNECT_PASSWORD}"
CONNECT_KAFKA_ARGS=("${CLIENT_ARGS[@]}")
client_args transfer "${INFRA_KAFKA_TRANSFER_PASSWORD}"
TRANSFER_KAFKA_ARGS=("${CLIENT_ARGS[@]}")

if ! timeout 180 rpk cluster health --watch --exit-when-healthy "${ADMIN_API_ARGS[@]}"; then
  echo "Redpanda cluster did not become healthy before initialization." >&2
  exit 1
fi

cleanup() {
  set +e
  if [ -n "${VERIFY_TOPIC}" ]; then
    rpk topic delete "${VERIFY_TOPIC}" "${ADMIN_KAFKA_ARGS[@]}" >/dev/null 2>&1
  fi
  if [ -n "${VERIFY_DLQ_TOPIC}" ]; then
    rpk topic delete "${VERIFY_DLQ_TOPIC}" "${ADMIN_KAFKA_ARGS[@]}" >/dev/null 2>&1
  fi
}
trap cleanup EXIT

existing_users="$(rpk security user list "${ADMIN_API_ARGS[@]}")"
for credential in \
  "connect:${INFRA_KAFKA_CONNECT_PASSWORD}" \
  "transfer:${INFRA_KAFKA_TRANSFER_PASSWORD}"; do
  username="${credential%%:*}"
  password="${credential#*:}"
  if ! awk 'NR > 1 {print $1}' <<<"${existing_users}" | grep -Fxq "${username}"; then
    rpk security user create "${username}" \
      --password "${password}" --mechanism SCRAM-SHA-256 \
      "${ADMIN_API_ARGS[@]}"
  fi
done
rpk cluster config set write_caching_default false "${ADMIN_API_ARGS[@]}"

create_compacted_topic() {
  local topic="$1"
  local partitions="$2"
  if ! rpk topic describe "${topic}" "${ADMIN_KAFKA_ARGS[@]}" >/dev/null 2>&1; then
    rpk topic create "${topic}" \
      --partitions "${partitions}" \
      --replicas "${INTERNAL_REPLICATION_FACTOR}" \
      --topic-config cleanup.policy=compact \
      --topic-config min.cleanable.dirty.ratio=0.1 \
      "${ADMIN_KAFKA_ARGS[@]}"
  fi
}

create_compacted_topic "${KAFKA_CONNECT_CONFIG_TOPIC}" 1
create_compacted_topic "${KAFKA_CONNECT_OFFSET_TOPIC}" 25
create_compacted_topic "${KAFKA_CONNECT_STATUS_TOPIC}" 5

for topic in "${KAFKA_CONNECT_CONFIG_TOPIC}" "${KAFKA_CONNECT_OFFSET_TOPIC}" "${KAFKA_CONNECT_STATUS_TOPIC}"; do
  rpk acl create --allow-principal User:connect \
    --topic "${topic}" --operation read,write,describe \
    "${ADMIN_KAFKA_ARGS[@]}"
done

rpk acl create --allow-principal User:connect \
  --group "${KAFKA_CONNECT_GROUP_ID}" --operation read,describe \
  "${ADMIN_KAFKA_ARGS[@]}"
rpk acl create --allow-principal User:connect \
  --cluster --operation describe "${ADMIN_KAFKA_ARGS[@]}"
rpk acl create --allow-principal User:connect \
  --resource-pattern-type prefixed --topic "${INFRA_KAFKA_CDC_TOPIC_PREFIX}" \
  --operation write,describe "${ADMIN_KAFKA_ARGS[@]}"
rpk acl create --allow-principal User:transfer \
  --resource-pattern-type prefixed --topic "${INFRA_KAFKA_CDC_TOPIC_PREFIX}" \
  --operation read,describe "${ADMIN_KAFKA_ARGS[@]}"
rpk acl create --allow-principal User:transfer \
  --resource-pattern-type prefixed --group "${INFRA_KAFKA_CDC_CONSUMER_GROUP_PREFIX}" \
  --operation read,describe "${ADMIN_KAFKA_ARGS[@]}"
rpk acl create --allow-principal User:transfer \
  --resource-pattern-type prefixed --topic __addp_dlq. \
  --operation create,write,read,describe,describe_configs \
  "${ADMIN_KAFKA_ARGS[@]}"

if [ "${VERIFY_ACL:-0}" = "1" ]; then
  VERIFY_TOPIC="${INFRA_KAFKA_CDC_TOPIC_PREFIX}verify.$(date +%s)"
  VERIFY_GROUP="${INFRA_KAFKA_CDC_CONSUMER_GROUP_PREFIX}verify.$(date +%s)"
  VERIFY_MESSAGE='{"id":1,"source":"acl-verification"}'

  rpk topic create "${VERIFY_TOPIC}" --partitions 1 --replicas 1 \
    --topic-config cleanup.policy=delete --topic-config retention.ms=600000 \
    "${ADMIN_KAFKA_ARGS[@]}" >/dev/null
  printf '%s\n' "${VERIFY_MESSAGE}" | rpk topic produce "${VERIFY_TOPIC}" \
    "${CONNECT_KAFKA_ARGS[@]}" >/dev/null

  consumed="$(timeout 15 rpk topic consume "${VERIFY_TOPIC}" \
    --group "${VERIFY_GROUP}" --offset start --num 1 --format '%v\n' \
    "${TRANSFER_KAFKA_ARGS[@]}")"
  if ! printf '%s\n' "${consumed}" | grep -Fxq "${VERIFY_MESSAGE}"; then
    echo "Transfer principal did not consume the expected CDC record." >&2
    exit 1
  fi

  if printf '%s\n' '{"id":2}' | rpk topic produce "${VERIFY_TOPIC}" \
    --delivery-timeout 5s "${TRANSFER_KAFKA_ARGS[@]}" >/dev/null 2>&1; then
    echo "Transfer principal unexpectedly wrote to the Infra CDC topic." >&2
    exit 1
  fi
  if rpk topic describe "${KAFKA_CONNECT_CONFIG_TOPIC}" \
    "${TRANSFER_KAFKA_ARGS[@]}" >/dev/null 2>&1; then
    echo "Transfer principal unexpectedly accessed a Kafka Connect internal topic." >&2
    exit 1
  fi

  VERIFY_DLQ_TOPIC="__addp_dlq.verify.$(date +%s)"
  rpk topic create "${VERIFY_DLQ_TOPIC}" --partitions 1 --replicas 1 \
    --topic-config cleanup.policy=compact,delete --topic-config retention.ms=600000 \
    "${TRANSFER_KAFKA_ARGS[@]}" >/dev/null
  printf '%s\n' '{"schema":"transfer.dead_letter/v1","identity":"acl-verification"}' | \
    rpk topic produce "${VERIFY_DLQ_TOPIC}" --key acl-verification \
      "${TRANSFER_KAFKA_ARGS[@]}" >/dev/null

  if printf '%s\n' '{"schema":"unauthorized"}' | \
    rpk topic produce "${VERIFY_DLQ_TOPIC}" --key unauthorized \
      --delivery-timeout 5s "${CONNECT_KAFKA_ARGS[@]}" >/dev/null 2>&1; then
    echo "Connect principal unexpectedly wrote to the Transfer DLQ topic." >&2
    exit 1
  fi

  echo "Infra Kafka ACL verification passed."
fi

echo "Redpanda users, internal topics, and ACLs are ready."
