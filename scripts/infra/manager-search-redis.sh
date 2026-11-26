#!/bin/sh
set -eu
umask 077
: "${MANAGER_SEARCH_REDIS_PASSWORD:?Manager search credential required}"
directory=/run/addp-redis
mkdir -p "$directory"
digest=$(printf %s "$MANAGER_SEARCH_REDIS_PASSWORD" | sha256sum | cut -d ' ' -f 1)
printf 'user default off\nuser manager_search reset on #%s ~manager:search:* +hello +ping +client|setname +client|setinfo +get +set +del +expireat +pexpireat +hget +hset +hdel +hmget +hgetall +zadd +zrem +zrangebyscore +zcard +time +eval +evalsha +script|load\n' \
  "$digest" > "$directory/users.acl"
unset MANAGER_SEARCH_REDIS_PASSWORD digest
chown -R redis:redis "$directory"
exec /usr/local/bin/docker-entrypoint.sh redis-server --aclfile "$directory/users.acl" --save "" --appendonly no --dir /data --maxmemory-policy noeviction
