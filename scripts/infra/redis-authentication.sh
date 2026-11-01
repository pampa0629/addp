#!/bin/sh
# Redis native ACL bootstrap; observation records live only in the Redis volume.
set -eu
scope=${1:?Explicit Redis scope required}
shift
case "$scope" in infra|business) ;; *) echo 'Redis authentication scope rejected' >&2; exit 1 ;; esac
umask 077
directory=/data/.addp-redis-observation
mkdir -p /run/addp-redis
acl=/run/addp-redis/users.acl
hash() { printf %s "$1" | sha256sum | cut -d ' ' -f 1; }
if [ "$scope" = infra ]; then
  : "${REDIS_PASSWORD:?Redis deployment password required}"
  printf 'user default on #%s ~* &* +@all\n' "$(hash "$REDIS_PASSWORD")" > "$acl"
else
  : "${BUSINESS_REDIS_ADMIN_PASSWORD:?Business Redis admin password required}"
  : "${BUSINESS_REDIS_READER_PASSWORD:?Business Redis reader password required}"
  printf 'user default off\nuser addp_business_admin on #%s ~* &* +@all\n' "$(hash "$BUSINESS_REDIS_ADMIN_PASSWORD")" > "$acl"
  printf 'user addp_business_reader on #%s ~* -@all +hello +ping +select +client|setname +client|setinfo +dbsize +type +pttl +ttl +exists +scan +get +strlen +getrange +hget +hmget +hgetall +hlen +hscan +lrange +llen +smembers +scard +sscan +zrange +zcard +zscore +zscan +xrange +xlen\n' "$(hash "$BUSINESS_REDIS_READER_PASSWORD")" >> "$acl"
fi
append_observer() {
  if [ -e "$directory" ] || [ -L "$directory" ]; then
    [ ! -L "$directory" ] && [ -d "$directory" ] &&
      [ "$(stat -c %u "$directory")" = "$(id -u redis)" ] && [ "$(stat -c %a "$directory")" = 700 ] || return 1
  else
    mkdir "$directory" && chown redis:redis "$directory" && chmod 700 "$directory" || return 1
  fi
  record="$directory/account.record"
  if [ -e "$record" ] || [ -L "$record" ]; then
  [ ! -L "$record" ] && [ -f "$record" ] &&
    [ "$(stat -c %u "$record")" = "$(id -u redis)" ] &&
    [ "$(stat -c %a "$record")" = 600 ] && [ "$(stat -c %h "$record")" = 1 ] &&
    [ "$(stat -c %s "$record")" -le 1024 ] && [ "$(wc -l < "$record" | tr -d ' ')" = 2 ] &&
    head -n 1 "$record" | grep -Eq "^addp[.]redis-observation/v1:$scope:[0-9a-f]{64}$" || return 1
  rule=$(tail -n 1 "$record")
  printf '%s\n' "$rule" | grep -Eq "^user addp_${scope}_observer reset (off|on #[0-9a-f]{64} \+hello \+ping \+info)$" || return 1
  printf '%s\n' "$rule" >> "$acl"
  fi
}
append_observer || echo 'Redis observation record rejected; optional observer disabled, core authentication preserved' >&2
chown -R redis:redis /run/addp-redis
exec /usr/local/bin/docker-entrypoint.sh redis-server --bind 0.0.0.0 --protected-mode yes --aclfile "$acl" "$@"
