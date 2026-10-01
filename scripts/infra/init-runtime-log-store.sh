#!/bin/sh
# Runs only in the owned MinIO initialization container; never prints credentials.
set -eu
# Loki runs as the fixed image UID 10001; initialize its persistent volume.
mkdir -p /loki
chown 10001:10001 /loki
chmod 750 /loki
case "$LOKI_BUCKET" in *[!a-z0-9-]*|'') echo 'invalid runtime log bucket' >&2; exit 1;; esac
mc alias set logs "$LOKI_S3_ENDPOINT" "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null
mc mb --ignore-existing "logs/$LOKI_BUCKET" >/dev/null
printf '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:ListBucket","s3:GetBucketLocation"],"Resource":["arn:aws:s3:::%s"]},{"Effect":"Allow","Action":["s3:GetObject","s3:PutObject","s3:DeleteObject"],"Resource":["arn:aws:s3:::%s/*"]}]}' "$LOKI_BUCKET" "$LOKI_BUCKET" >/tmp/runtime-policy.json
mc admin policy create logs runtime-log-store /tmp/runtime-policy.json >/dev/null
mc admin user add logs "$LOKI_S3_ACCESS_KEY" "$LOKI_S3_SECRET_KEY" >/dev/null
mc admin policy attach logs runtime-log-store --user "$LOKI_S3_ACCESS_KEY" >/dev/null
echo 'Runtime log bucket and least-privilege account ready'
