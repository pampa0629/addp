#!/usr/bin/env bash
# ADDP_T2_HOSTED_ONLY=apache-kafka
# ADDP_T2_NATIVE_LIFECYCLE=scripts/test/apache-kafka-fixture.sh
# ADDP_T2_INPUT_FILES=scripts/test/apache-kafka-fixture.sh common/engine/plugins/kafka/ transfer/backend/internal/deadletter/
set -euo pipefail
cd "$(dirname "$0")/../.."
if [[ ${1:-} == --verify ]]; then
    : "${ADDP_TEST_KAFKA_BOOTSTRAP_SERVERS:?disposable broker required}" "${LOG_DIR:?private gate logs required}"
    export ADDP_KAFKA_INTEGRATION=1 ADDP_INFRA_KAFKA_ACL_TEST=1 ADDP_DLQ_KAFKA_INTEGRATION=1
    (cd common && GOWORK=off go test ./engine/plugins/kafka -run '^(TestIntegrationKafkaChangeStreamStartsAtTransferCommittedPosition|TestInfraKafkaStreamPrincipals)$' -count=1 -v) | tee "$LOG_DIR/stream.log"
    grep -q '^--- PASS: TestIntegrationKafkaChangeStreamStartsAtTransferCommittedPosition ' "$LOG_DIR/stream.log"
    grep -q '^--- PASS: TestInfraKafkaStreamPrincipals ' "$LOG_DIR/stream.log"
    (cd transfer/backend && GOWORK=off go test ./internal/deadletter -run '^TestIntegrationKafkaPayloadWriterCreatesAndWritesTransferDLQTopic$' -count=1 -v) | tee "$LOG_DIR/dlq.log"
    grep -q '^--- PASS: TestIntegrationKafkaPayloadWriterCreatesAndWritesTransferDLQTopic ' "$LOG_DIR/dlq.log"
    exit
fi
if [[ ${GITHUB_ACTIONS:-} == true && ${RUNNER_OS:-} == Linux || ${ADDP_KAFKA_T2_DISPOSABLE:-} == 1 ]]; then
    exec bash scripts/test/apache-kafka-fixture.sh "$PWD/scripts/test/common-apache-kafka-gate.sh"
fi
echo 'Run on the exclusive Linux Hosted runner or opt in on a disposable host with ADDP_KAFKA_T2_DISPOSABLE=1.' >&2
exit 2
