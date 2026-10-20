#!/bin/sh
# Only the selected deployment initializer handles its own bucket/credential.
set -eu
case "$TEMPO_BUCKET" in *[!a-z0-9-]*|'') echo 'invalid trace bucket' >&2; exit 1;; esac
mkdir -p /var/tempo
chown 10001:10001 /var/tempo
chmod 750 /var/tempo
mc alias set traces "$TEMPO_S3_ENDPOINT" "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null
mc mb --ignore-existing "traces/$TEMPO_BUCKET" >/dev/null
mc quota set "traces/$TEMPO_BUCKET" --size 20GiB >/dev/null
printf '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListBucket","s3:GetBucketLocation"],"Resource":["arn:aws:s3:::%s"]},{"Effect":"Allow","Action":["s3:GetObject","s3:PutObject","s3:DeleteObject"],"Resource":["arn:aws:s3:::%s/*"]}]}' "$TEMPO_BUCKET" "$TEMPO_BUCKET" >/tmp/trace-policy.json
mc admin policy create traces trace-store /tmp/trace-policy.json >/dev/null
mc admin user add traces "$TEMPO_S3_ACCESS_KEY" "$TEMPO_S3_SECRET_KEY" >/dev/null
mc admin policy attach traces trace-store --user "$TEMPO_S3_ACCESS_KEY" >/dev/null
echo 'Trace store initialized with independent credential and bucket quota'
