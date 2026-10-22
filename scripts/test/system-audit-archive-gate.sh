#!/usr/bin/env bash
# ADDP_T2_HOSTED_ONLY=postgres-worm
# ADDP_T2_INPUT_FILES=system/backend/internal/auditarchive/ system/backend/cmd/audit-archive/ scripts/infra/Dockerfile.minio
# Own disposable docker run / docker rm fixtures; never inspect or change local Infra.
set -euo pipefail
if [ "${GITHUB_ACTIONS:-}" != true ] || [ "${RUNNER_ENVIRONMENT:-}" != github-hosted ]; then
    echo "Independent audit archive T2 requires an exclusive GitHub Hosted runner" >&2
    exit 2
fi
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)
WORK_DIR=$(mktemp -d "${RUNNER_TEMP:?Hosted temporary directory required}/addp-audit-archive.XXXXXX")
OWNER=$(python3 -c 'import uuid; print(uuid.uuid4())')
IMAGE="addp-audit-archive-minio:$OWNER"
PG_ID=""
S3_ID=""
cleanup() {
    local status=$? identity
    trap - EXIT INT TERM
    for identity in "$PG_ID" "$S3_ID"; do
        [ -n "$identity" ] || continue
        if [ "$(docker inspect -f '{{index .Config.Labels "addp.test.audit-archive"}}' "$identity")" != "$OWNER" ]; then
            echo "Refusing a foreign audit fixture cleanup target" >&2
            status=1
            continue
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
python3 - "$WORK_DIR" <<'PY'
import json,sys
from datetime import datetime, timezone, timedelta
from pathlib import Path
root=Path(sys.argv[1]); base='arn:aws:s3:::audit-disposable'
source='sources/a21db94b-6dd5-446a-b66a-df379ea51dc8/'
queries='queries/748d2bb7-8071-4a85-83b8-873f5c8e1fd7/'
def statement(actions,resources,condition=None):
    s={'Effect':'Allow','Action':actions,'Resource':resources}
    if condition:s['Condition']=condition
    return s
def policy(name,statements):
    (root/(name+'.json')).write_text(json.dumps({'Version':'2012-10-17','Statement':statements}))
listing=statement(['s3:ListBucketVersions'],[base],{'StringLike':{'s3:prefix':[source+'*']}})
read=statement(['s3:GetObjectVersion','s3:GetObjectRetention','s3:GetObjectLegalHold'],[base+'/'+source+'*'])
now=datetime.now(timezone.utc)
try: control=now.replace(year=now.year+20)
except ValueError: control=now.replace(year=now.year+20,month=3,day=1)
def retained(paths,deadline):
    return statement(['s3:PutObject','s3:PutObjectRetention'],[base+'/'+p for p in paths],{
        'StringEquals':{'s3:object-lock-mode':'COMPLIANCE'},
        'DateGreaterThanEquals':{'s3:object-lock-retain-until-date':deadline.strftime('%Y-%m-%dT%H:%M:%SZ')}})
def no_hold(paths):
    return {'Effect':'Deny','Action':['s3:PutObjectLegalHold'],'Resource':[base+'/'+p for p in paths]}
# These dated lower bounds belong only to this short-lived job. Production
# deployment must establish and verify its own current retention policy.
policy('writer',[listing,read,
    retained([source+'data/*'],now+timedelta(days=365)),
    retained([source+'root.json',source+'checkpoints/*'],control),
    no_hold([source+'*'])])
policy('reader',[listing,read,
    statement(['s3:ListBucketVersions'],[base],{'StringLike':{'s3:prefix':[queries+'*']}}),
    statement(['s3:GetObjectVersion','s3:GetObjectRetention','s3:GetObjectLegalHold'],[base+'/'+queries+'*']),
    retained([queries+'*'],now+timedelta(days=365)),no_hold([source+'*',queries+'*'])])
PY
for identity in writer reader; do
    docker exec -i "$S3_ID" mc admin policy create owned "audit-$identity-policy" /dev/stdin < "$WORK_DIR/$identity.json" >/dev/null
    docker exec "$S3_ID" mc admin policy attach owned "audit-$identity-policy" --user "audit-$identity" >/dev/null
done
PG_PORT=$(docker port "$PG_ID" 5432/tcp | sed -n 's/^127\.0\.0\.1://p')
S3_PORT=$(docker port "$S3_ID" 9000/tcp | sed -n 's/^127\.0\.0\.1://p')
[[ "$PG_PORT" =~ ^[0-9]+$ && "$S3_PORT" =~ ^[0-9]+$ ]] || exit 1
export ADDP_AUDIT_ARCHIVE_INTEGRATION=1
export ADDP_AUDIT_POSTGRES_TEST_DSN="postgres://owned_audit_admin:owned-pg-test@127.0.0.1:$PG_PORT/addp_audit_disposable?sslmode=disable"
export ADDP_AUDIT_S3_TEST_ENDPOINT="127.0.0.1:$S3_PORT"
(cd "$ROOT_DIR/system/backend" && go test ./internal/auditarchive -run '^TestIndependentAuditArchiveAgainstPostgres$' -count=1 -timeout=5m -v) 2>&1 | tee "$WORK_DIR/integration.log"
! grep -q -- '--- SKIP:' "$WORK_DIR/integration.log"
grep -q -- '--- PASS: TestIndependentAuditArchiveAgainstPostgres ' "$WORK_DIR/integration.log"
echo "AUDIT_ARCHIVE_PROTOCOL_T2_PASS production_independent_management_domain=unverified"
