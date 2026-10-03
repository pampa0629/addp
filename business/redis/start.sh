#!/bin/sh
set -eu
: "${BUSINESS_REDIS_ADMIN_PASSWORD:?Business Redis admin password required}"
: "${BUSINESS_REDIS_READER_PASSWORD:?Business Redis reader password required}"
umask 077
mkdir -p /run/addp-redis
admin_hash=$(printf '%s' "$BUSINESS_REDIS_ADMIN_PASSWORD" | sha256sum | cut -d ' ' -f 1)
reader_hash=$(printf '%s' "$BUSINESS_REDIS_READER_PASSWORD" | sha256sum | cut -d ' ' -f 1)
cat > /run/addp-redis/users.acl <<EOF
user default off
user addp_business_admin on #$admin_hash ~* &* +@all
user addp_business_reader on #$reader_hash ~* -@all +hello +ping +select +client|setname +client|setinfo +dbsize +type +pttl +ttl +exists +scan +get +strlen +getrange +hget +hmget +hgetall +hlen +hscan +lrange +llen +smembers +scard +sscan +zrange +zcard +zscore +zscan +xrange +xlen
EOF
chown -R redis:redis /run/addp-redis
# Re-enter the official entrypoint so it drops privileges to the redis user.
exec /usr/local/bin/docker-entrypoint.sh redis-server \
    --bind 0.0.0.0 --protected-mode yes --databases 1 \
    --aclfile /run/addp-redis/users.acl --appendonly yes --save "" \
    --maxmemory 256mb --maxmemory-policy noeviction
