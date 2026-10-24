#!/usr/bin/env bash
# ADDP_T2_HOSTED_ONLY=postgres-worm
# ADDP_T2_INPUT_FILES=system/backend/internal/auditarchive/ system/backend/cmd/audit-archive/ scripts/infra/Dockerfile.minio
# Own disposable docker run / docker rm fixtures; never inspect or change local Infra.
set -euo pipefail
if [ "${GITHUB_ACTIONS:-}" != true ] || [ "${RUNNER_ENVIRONMENT:-}" != github-hosted ]; then
    echo "Platform audit archive T2 requires an exclusive GitHub Hosted runner" >&2
    exit 2
fi
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
WORK_DIR=$(mktemp -d "${RUNNER_TEMP:?Hosted temporary directory required}/addp-audit-archive.XXXXXX")
OWNER=$(python3 -c 'import uuid; print(uuid.uuid4())')
IMAGE="addp-audit-archive-minio:$OWNER"
PG_ID=""
S3_ID=""
A_ID=""
cleanup() {
    local status=$? identity
    trap - EXIT INT TERM
    for identity in "$PG_ID" "$S3_ID" "$A_ID"; do
        [ -n "$identity" ] || continue
        if [ "$(docker inspect -f '{{index .Config.Labels "addp.test.audit-archive"}}' "$identity")" != "$OWNER" ]; then
            echo "Refusing a foreign audit fixture cleanup target" >&2
            status=1
            continue
        fi
        if [ "$status" -ne 0 ] && [ "$identity" = "$A_ID" ]; then
            # Inspect only this job's source queue before destroying it. Counts
            # distinguish pending replay from an empty store; never dump events.
            queue_counts=$(docker exec "$identity" sh -c 'printf "files=%s kib=%s" "$(find /queue -type f | wc -l | tr -d " ")" "$(du -sk /queue | cut -f1)"' 2>/dev/null) || queue_counts=unavailable
            printf 'AUDIT_NATIVE_QUEUE_FAILURE %s\n' "$queue_counts"
        fi
        docker rm -fv "$identity" >/dev/null || status=1
    done
    if [ -n "$(docker ps -aq --filter "label=addp.test.audit-archive=$OWNER")" ]; then status=1; fi
    if docker image inspect "$IMAGE" >/dev/null 2>&1; then
        if [ "$(docker image inspect -f '{{index .Config.Labels "addp.test.audit-archive"}}' "$IMAGE")" = "$OWNER" ]; then
            docker image rm "$IMAGE" >/dev/null || status=1
        else status=1; fi
    fi
    printf 'AUDIT_ARCHIVE_CLEANUP exit=%s owned_containers_remaining=%s\n' "$status" "$(docker ps -aq --filter "label=addp.test.audit-archive=$OWNER" | wc -l | tr -d ' ')"
    # Evidence survives in RUNNER_TEMP and is uploaded by the workflow.
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker build --label "addp.test.audit-archive=$OWNER" -t "$IMAGE" -f "$ROOT_DIR/scripts/infra/Dockerfile.minio" "$ROOT_DIR/scripts/infra"
PG_ID=$(docker run -d --label "addp.test.audit-archive=$OWNER" --tmpfs /var/lib/postgresql/data:rw --memory 768m \
    -e POSTGRES_DB=addp_audit_disposable -e POSTGRES_USER=owned_audit_admin -e POSTGRES_PASSWORD=owned-pg-test \
    -p 127.0.0.1::5432 postgres:15@sha256:74e110c41804365e3915fcc09d5e7a1eff50161aaa94d5da0e58e0cd75ae509c \
    -c wal_level=logical -c max_replication_slots=2 -c max_slot_wal_keep_size=64MB)
S3_ID=$(docker run -d --label "addp.test.audit-archive=$OWNER" --tmpfs /data:rw --memory 768m \
    -e MINIO_ROOT_USER=owned-audit-root -e MINIO_ROOT_PASSWORD=owned-root-test-secret \
    -p 127.0.0.1::9000 "$IMAGE" server /data)
for iteration in $(seq 1 90); do
    if docker exec "$PG_ID" pg_isready -U owned_audit_admin -d addp_audit_disposable >/dev/null 2>&1 && \
       docker exec "$S3_ID" curl -fsS http://127.0.0.1:9000/minio/health/ready >/dev/null 2>&1; then break; fi
    [ "$iteration" -lt 90 ] || { echo "Audit fixtures not ready" >&2; exit 1; }
    sleep 1
done
# All credentials below are synthetic, fixture-owned and destroyed with the job.
docker exec "$S3_ID" mc alias set owned http://127.0.0.1:9000 owned-audit-root owned-root-test-secret >/dev/null
docker exec "$S3_ID" mc admin user add owned audit-writer owned-writer-test >/dev/null
docker exec "$S3_ID" mc admin user add owned audit-reader owned-reader-test >/dev/null
docker exec "$S3_ID" mc admin user add owned access-writer owned-access-writer-test >/dev/null
docker exec "$S3_ID" mc admin user add owned access-reader owned-access-reader-test >/dev/null
mkdir "$WORK_DIR/certs"
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj /CN=owned-receiver \
    -addext 'subjectAltName=IP:127.0.0.1' -keyout "$WORK_DIR/receiver-key.pem" -out "$WORK_DIR/certs/receiver.crt" >/dev/null 2>&1
chmod 600 "$WORK_DIR/receiver-key.pem"
docker exec "$S3_ID" mc admin user add owned tenant-user owned-tenant-test >/dev/null
python3 - "$WORK_DIR" <<'PY'
import json,sys
from datetime import datetime, timezone, timedelta
from pathlib import Path
root=Path(sys.argv[1]); base='arn:aws:s3:::platform-disposable'
source='audit/sources/a21db94b-6dd5-446a-b66a-df379ea51dc8/'
queries='audit/queries/748d2bb7-8071-4a85-83b8-873f5c8e1fd7/'
def statement(actions,resources,condition=None):
    s={'Effect':'Allow','Action':actions,'Resource':resources}
    if condition:s['Condition']=condition
    return s
def policy(name,statements):
    (root/(name+'.json')).write_text(json.dumps({'Version':'2012-10-17','Statement':statements}))
# Archive writer/auditor policies are generated by the actual offline CLI,
# below. B's separate historical protocol fixture retains its dated bounds.
now=datetime.now(timezone.utc)
try: control=now.replace(year=now.year+20)
except ValueError: control=now.replace(year=now.year+20,month=3,day=1)
policy('tenant',[
    statement(['s3:ListBucket','s3:ListBucketVersions'],[base],{'StringLike':{'s3:prefix':['tenant_17/*']}}),
    statement(['s3:GetObject','s3:GetObjectVersion','s3:PutObject','s3:DeleteObject','s3:DeleteObjectVersion'],[base+'/tenant_17/*'])])
access='arn:aws:s3:::audit-access-disposable'
prefixes=['access/a21db94b-6dd5-446a-b66a-df379ea51dc8/','access/aa1744e2-4fbe-45fb-aa91-2a65e26195a1/']
access_list=statement(['s3:ListBucketVersions'],[access],{'StringLike':{'s3:prefix':[p+'*' for p in prefixes]}})
access_read=statement(['s3:GetObjectVersion','s3:GetObjectRetention','s3:GetObjectLegalHold'],[access+'/'+p+'*' for p in prefixes])
def access_put(suffix,deadline):
    return statement(['s3:PutObject','s3:PutObjectRetention'],[access+'/'+p+suffix for p in prefixes],{
        'StringEquals':{'s3:object-lock-mode':'COMPLIANCE'},
        'DateGreaterThanEquals':{'s3:object-lock-retain-until-date':deadline.strftime('%Y-%m-%dT%H:%M:%SZ')}})
access_hold={'Effect':'Deny','Action':['s3:PutObjectLegalHold'],'Resource':[access+'/*']}
policy('access-writer',[access_list,access_read,access_put('data/*',now+timedelta(days=365)),access_put('root.json',control),access_put('receipts/*',control),access_hold])
policy('access-reader',[access_list,access_read,access_hold])
# Synthetic private configs are used only to emit the production IAM policy.
# The invalid authority endpoints must never be contacted by the policy action.
from urllib.parse import urlencode
config={'mode':'collector','source_id':'a21db94b-6dd5-446a-b66a-df379ea51dc8',
        'endpoint':'https://must-not-connect.invalid','region':'us-east-1',
        'bucket':'platform-disposable','access_key':'owned-policy-input',
        'secret_key':'owned-private-policy-input','retention_days':365,
        'postgres_dsn':'postgres://fixture:fixture@authority.invalid/test?'+urlencode({'sslmode':'verify-full','sslrootcert':str(root/'certs/receiver.crt')}),
        'slot':'audit_slot','publication':'audit_publication'}
(root/'collector.json').write_text(json.dumps(config));(root/'collector.json').chmod(0o600)
config.update(mode='auditor',actor='748d2bb7-8071-4a85-83b8-873f5c8e1fd7',root={'key':source+'root.json','version':'fixture','sha256':'0'*64})
for key in ['postgres_dsn','slot','publication']:config.pop(key)
(root/'auditor.json').write_text(json.dumps(config));(root/'auditor.json').chmod(0o600)


PY
make -C "$ROOT_DIR" build-audit-archive OUT_DIR=dist/audit-platform-bucket-t2 GOOS=linux GOARCH=amd64
CLI="$ROOT_DIR/dist/audit-platform-bucket-t2/release-linux-amd64/addp-audit-archive"
"$CLI" policy --config "$WORK_DIR/collector.json" > "$WORK_DIR/writer.json"
"$CLI" policy --config "$WORK_DIR/auditor.json" > "$WORK_DIR/reader.json"
docker exec -i "$S3_ID" mc admin policy create owned tenant-policy /dev/stdin < "$WORK_DIR/tenant.json" >/dev/null
docker exec "$S3_ID" mc admin policy attach owned tenant-policy --user tenant-user >/dev/null
for identity in writer reader; do
    docker exec -i "$S3_ID" mc admin policy create owned "audit-$identity-policy" /dev/stdin < "$WORK_DIR/$identity.json" >/dev/null
    docker exec "$S3_ID" mc admin policy attach owned "audit-$identity-policy" --user "audit-$identity" >/dev/null
done
for identity in access-writer access-reader; do
    docker exec -i "$S3_ID" mc admin policy create owned "$identity-policy" /dev/stdin < "$WORK_DIR/$identity.json" >/dev/null
    docker exec "$S3_ID" mc admin policy attach owned "$identity-policy" --user "$identity" >/dev/null
done
# A is a separate, owned source. It uses its unmodified native disk webhook
# queue; B cannot generate recursive audit events back into this queue.
read -r A_PORT RECEIVER_PORT < <(python3 - <<'PYPORT'
import socket
sockets=[socket.socket(),socket.socket()]
for s in sockets:s.bind(('127.0.0.1',0))
print(*(s.getsockname()[1] for s in sockets))
for s in sockets:s.close()
PYPORT
)
A_ID=$(docker run -d --label "addp.test.audit-archive=$OWNER" --network host --tmpfs /data:rw --tmpfs /queue:rw --memory 512m \
    -v "$WORK_DIR/certs:/root/.minio/certs/CAs:ro" \
    -e MINIO_ROOT_USER=owned-source-root -e MINIO_ROOT_PASSWORD=owned-source-test-secret \
    -e MINIO_AUDIT_WEBHOOK_ENABLE=on -e "MINIO_AUDIT_WEBHOOK_ENDPOINT=https://127.0.0.1:$RECEIVER_PORT/api/v1/internal/audit-receipts" \
    -e 'MINIO_AUDIT_WEBHOOK_AUTH_TOKEN=Bearer owned-fixture-receiver-token-32-characters' \
    -e MINIO_AUDIT_WEBHOOK_QUEUE_DIR=/queue -e MINIO_AUDIT_WEBHOOK_QUEUE_SIZE=10000 \
    -e MINIO_AUDIT_WEBHOOK_MAX_RETRY=0 -e MINIO_AUDIT_WEBHOOK_RETRY_INTERVAL=100ms \
    -e MINIO_AUDIT_WEBHOOK_HTTP_TIMEOUT=2s -e MINIO_AUDIT_WEBHOOK_BATCH_SIZE=1 \
    "$IMAGE" server --address "127.0.0.1:$A_PORT" --console-address 127.0.0.1:0 /data)
for iteration in $(seq 1 60); do
    if docker exec "$A_ID" curl -fsS "http://127.0.0.1:$A_PORT/minio/health/ready" >/dev/null 2>&1; then break; fi
    [ "$iteration" -lt 60 ] || { echo "Owned audit source not ready" >&2; exit 1; }
    sleep 1
done
docker exec "$A_ID" mc alias set owned-source "http://127.0.0.1:$A_PORT" owned-source-root owned-source-test-secret >/dev/null
A_DEPLOYMENT=$(docker exec "$A_ID" mc admin info --json owned-source | python3 -c 'import json,sys; p=json.load(sys.stdin); d=p["info"]["deploymentID"]; assert d; print(d)')
export ADDP_AUDIT_A_TEST_ENDPOINT="127.0.0.1:$A_PORT"
export ADDP_AUDIT_A_TEST_DEPLOYMENT="$A_DEPLOYMENT"
export ADDP_AUDIT_RECEIVER_TEST_ADDRESS="127.0.0.1:$RECEIVER_PORT"
export ADDP_AUDIT_RECEIVER_TEST_CERT="$WORK_DIR/certs/receiver.crt"
export ADDP_AUDIT_RECEIVER_TEST_KEY="$WORK_DIR/receiver-key.pem"
PG_PORT=$(docker port "$PG_ID" 5432/tcp | sed -n 's/^127\.0\.0\.1://p')
S3_PORT=$(docker port "$S3_ID" 9000/tcp | sed -n 's/^127\.0\.0\.1://p')
[[ "$PG_PORT" =~ ^[0-9]+$ && "$S3_PORT" =~ ^[0-9]+$ ]] || exit 1
export ADDP_AUDIT_ARCHIVE_INTEGRATION=1
export ADDP_AUDIT_POSTGRES_TEST_DSN="postgres://owned_audit_admin:owned-pg-test@127.0.0.1:$PG_PORT/addp_audit_disposable?sslmode=disable"
export ADDP_AUDIT_S3_TEST_ENDPOINT="127.0.0.1:$S3_PORT"
(cd "$ROOT_DIR/system/backend" && go test ./internal/auditarchive -run '^TestIndependentAudit(Archive|Receiver)AgainstPostgres$' -count=1 -timeout=7m -v) 2>&1 | tee "$WORK_DIR/integration.log"
! grep -q -- '--- SKIP:' "$WORK_DIR/integration.log"
grep -q -- '--- PASS: TestIndependentAuditArchiveAgainstPostgres ' "$WORK_DIR/integration.log"
grep -q -- '--- PASS: TestIndependentAuditReceiverAgainstPostgres ' "$WORK_DIR/integration.log"
echo "AUDIT_ARCHIVE_PROTOCOL_T2_PASS platform_bucket_isolation=verified actual_infra_deployment=unverified trusted_writer_deadlines=application_verified"
