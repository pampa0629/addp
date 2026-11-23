#!/usr/bin/env bash
# Native disposable Apache Kafka fixture. Only the owned child and directory are removed.
set -euo pipefail
umask 077
version=4.4.0
distribution=kafka_2.13-$version
checksum=f5ceba8a265ab6bb2d68362b1f6859a95e15d60ef92ba83b43fdf065b557dc18e98d6383c15b9977320e25b3a2c8b6f11decb17b0fa6c0f163423793a0eb95cb
if [[ $1 == --archive-key ]]; then
    printf 'apache-kafka-%s-%s\n' "$distribution" "$checksum"
    exit
fi
verify=$1
work=$(mktemp -d "${TMPDIR:-/tmp}/addp-kafka-XXXXXX")
pid=
cleanup() {
    local status=$?
    trap - EXIT INT TERM
    if [[ -n $pid ]]; then
        kill "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    fi
    rm -rf "$work"
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
archive=${ADDP_KAFKA_T2_ARCHIVE:-$work/$distribution.tgz}
if [[ $verify == --prepare-archive || -z ${ADDP_KAFKA_T2_ARCHIVE:-} ]]; then
    archive=$work/$distribution.tgz
    curl -fsSL --retry 2 --max-time 180 "https://archive.apache.org/dist/kafka/$version/$distribution.tgz" -o "$archive"
fi
python3 - "$archive" "$checksum" <<'PYTHON'
import hashlib, sys
with open(sys.argv[1], "rb") as stream:
    digest = hashlib.sha512()
    for block in iter(lambda: stream.read(1024*1024), b""):
        digest.update(block)
if digest.hexdigest() != sys.argv[2]:
    sys.exit("Apache Kafka distribution checksum mismatch")
PYTHON
if [[ $verify == --prepare-archive ]]; then
    mv "$archive" "${2:?missing destination for verified Kafka archive}"
    exit
fi
tar -xzf "$archive" -C "$work"
cd "$work"
export PATH="$work/$distribution/bin:$PATH" KAFKA_HEAP_OPTS="-Xms128m -Xmx256m" LOG_DIR="$work/logs"
mkdir state "$LOG_DIR"
# Reserve both distinct loopback ports before starting the private broker.
read -r BROKER_PORT CONTROLLER_PORT < <(python3 - <<'PYTHON'
import socket
with socket.socket() as a, socket.socket() as b:
    for sock in (a, b):
        sock.bind(("127.0.0.1", 0))
    print(a.getsockname()[1], b.getsockname()[1])
PYTHON
)
ADMIN_SECRET=$(openssl rand -hex 24)
CONNECT_SECRET=$(openssl rand -hex 24)
TRANSFER_SECRET=$(openssl rand -hex 24)
BROKER_SECRET=$(openssl rand -hex 24)
cat >server.properties <<EOF
process.roles=broker,controller
node.id=1
controller.listener.names=CONTROLLER
controller.quorum.bootstrap.servers=127.0.0.1:$CONTROLLER_PORT
listeners=BROKER://127.0.0.1:$BROKER_PORT,CONTROLLER://127.0.0.1:$CONTROLLER_PORT
advertised.listeners=BROKER://127.0.0.1:$BROKER_PORT
listener.security.protocol.map=BROKER:SASL_PLAINTEXT,CONTROLLER:SASL_PLAINTEXT
inter.broker.listener.name=BROKER
sasl.mechanism.inter.broker.protocol=SCRAM-SHA-256
sasl.mechanism.controller.protocol=PLAIN
listener.name.controller.sasl.enabled.mechanisms=PLAIN
listener.name.controller.plain.sasl.jaas.config=org.apache.kafka.common.security.plain.PlainLoginModule required username="broker" password="$BROKER_SECRET" user_broker="$BROKER_SECRET";
listener.name.broker.sasl.enabled.mechanisms=SCRAM-SHA-256
listener.name.broker.scram-sha-256.sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required username="broker" password="$BROKER_SECRET";
authorizer.class.name=org.apache.kafka.metadata.authorizer.StandardAuthorizer
super.users=User:broker;User:admin
allow.everyone.if.no.acl.found=false
auto.create.topics.enable=false
log.dirs=$work/state
offsets.topic.replication.factor=1
transaction.state.log.replication.factor=1
transaction.state.log.min.isr=1
group.initial.rebalance.delay.ms=0
EOF
cat > admin.properties <<EOF
security.protocol=SASL_PLAINTEXT
sasl.mechanism=SCRAM-SHA-256
sasl.jaas.config=org.apache.kafka.common.security.scram.ScramLoginModule required username="admin" password="$ADMIN_SECRET";
request.timeout.ms=5000
default.api.timeout.ms=8000
EOF
kafka-storage.sh format --standalone -t "$(kafka-storage.sh random-uuid)" -c server.properties \
 --add-scram "SCRAM-SHA-256=[name=broker,password=$BROKER_SECRET]" \
 --add-scram "SCRAM-SHA-256=[name=admin,password=$ADMIN_SECRET]" \
 --add-scram "SCRAM-SHA-256=[name=connect,password=$CONNECT_SECRET]" \
 --add-scram "SCRAM-SHA-256=[name=transfer,password=$TRANSFER_SECRET]" >format.log 2>&1 || { echo 'format failed';exit 1; }
kafka-server-start.sh server.properties >server.log 2>&1 &
pid=$!

broker=127.0.0.1:$BROKER_PORT
admin=(--bootstrap-server "$broker" --command-config admin.properties)
ready=false
for n in {1..12}; do
    if kafka-topics.sh "${admin[@]}" --list >/dev/null 2>client.log; then
        ready=true
        break
    fi
    kill -0 "$pid" 2>/dev/null || break
done
if [[ $ready != true ]]; then
    echo 'Kafka broker failed its authenticated readiness check' >&2
    grep -Eho '[A-Za-z.]+(Exception|Error)' client.log server.log | sort -u >&2 || true
    exit 1
fi
for topic in connect-configs addp.cdc.test; do
    kafka-topics.sh "${admin[@]}" --create --topic "$topic" --partitions 1 --replication-factor 1 >/dev/null
done
acl() { kafka-acls.sh "${admin[@]}" --add "$@" >/dev/null; }
acl --allow-principal User:connect --topic connect-configs --operation Read --operation Write --operation Describe
acl --allow-principal User:connect --topic addp.cdc. --resource-pattern-type prefixed --operation Write --operation Describe
acl --allow-principal User:transfer --topic addp.cdc. --resource-pattern-type prefixed --operation Read --operation Describe
acl --allow-principal User:transfer --group transfer. --resource-pattern-type prefixed --operation Read --operation Describe
acl --allow-principal User:transfer --topic __addp_dlq. --resource-pattern-type prefixed --operation Create --operation Write --operation Read --operation Describe --operation DescribeConfigs
echo 'Kafka topic and group ACLs applied'

export ADDP_TEST_KAFKA_BOOTSTRAP_SERVERS="$broker" ADDP_TEST_KAFKA_SECURITY_PROTOCOL=sasl_plaintext
export ADDP_TEST_KAFKA_SASL_MECHANISM=scram-sha-256 ADDP_TEST_KAFKA_USERNAME=admin ADDP_TEST_KAFKA_PASSWORD="$ADMIN_SECRET"
export INFRA_KAFKA_ADMIN_PASSWORD="$ADMIN_SECRET" INFRA_KAFKA_CONNECT_PASSWORD="$CONNECT_SECRET" INFRA_KAFKA_TRANSFER_PASSWORD="$TRANSFER_SECRET"
bash "$verify" --verify
