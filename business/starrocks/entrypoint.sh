#!/usr/bin/env bash
# Keep the official FE/BE and director; initialize credentials before director starts.
set -euo pipefail

validate_config() {
    [[ "${STARROCKS_USER:-}" =~ ^[a-z][a-z0-9_]{0,31}$ ]] && [ "$STARROCKS_USER" != root ] || {
        echo 'STARROCKS_USER must be a non-root ordinary account.' >&2; return 1;
    }
    [ "${#STARROCKS_PASSWORD}" -ge 12 ] && [ "${#STARROCKS_APP_PASSWORD}" -ge 12 ] &&
        [ "$STARROCKS_PASSWORD" != "$STARROCKS_APP_PASSWORD" ] || {
        echo 'Distinct administrator and reader passwords of at least 12 characters required.' >&2; return 1;
    }
}
sql_string() {
    local value=$1
    value=${value//\\/\\\\}
    value=${value//\'/\'\'}
    printf "'%s'" "$value"
}
sql() {
    MYSQL_PWD="$STARROCKS_PASSWORD" mysql --connect-timeout=2 --default-character-set=utf8mb4 -h127.0.0.1 -P9030 -uroot --batch --skip-column-names --silent
}
backend_ready() {
    # Header names must be used: backend column positions change across versions.
    printf 'SHOW BACKENDS;\n' | MYSQL_PWD="$STARROCKS_PASSWORD" mysql --connect-timeout=2 -h127.0.0.1 -P9030 -uroot --batch --column-names |
        awk -F '\t' 'NR==1 {for(i=1;i<=NF;i++) if($i=="Alive") alive=i; next} {n++; if(!alive || $alive!="true") bad=1} END {exit !(n==1 && !bad)}'
}
health() {
    [ -f "$SR_HOME/fe/meta/.addp-business-initialized" ] || return 1
    printf 'SELECT COUNT(*) FROM business.source;\n' | MYSQL_PWD="$STARROCKS_APP_PASSWORD" mysql --connect-timeout=2 -h127.0.0.1 -P9030 -u"$STARROCKS_USER" --batch --silent >/dev/null 2>&1 || return 1
    backend_ready
}
main() {
    validate_config
    export SR_HOME=${SR_HOME:-/data/deploy/starrocks}
    [ "${1:-}" != --health ] || { health; return; }
    local marker="$SR_HOME/fe/meta/.addp-admin-initialized"
    local fresh=0
    mkdir -p "$SR_HOME/fe/meta"
    if [ ! -f "$marker" ]; then
        [ -z "$(find "$SR_HOME/fe/meta" -mindepth 1 -maxdepth 1 -print -quit)" ] || {
            echo 'Existing StarRocks metadata has no ADDP initialization marker; refusing authentication reset.' >&2; return 1;
        }
        fresh=1
    fi
    # The official director inherits MYSQL_PWD. Disable only its autostart so
    # an empty initial root password cannot race with its password check.
    sed -i '/^\[program:director\]$/,$s/^autostart=true$/autostart=false/' "$SR_HOME/supervisor/supervisord.conf"
    export MYSQL_PWD="$STARROCKS_PASSWORD"
    bash /data/deploy/entrypoint.sh &
    local server_pid=$!
    trap "kill -TERM $server_pid 2>/dev/null || true; wait $server_pid 2>/dev/null || true" EXIT
    trap 'exit 143' TERM
    trap 'exit 130' INT
    local ready=0
    for ((attempt=0; attempt<150; attempt++)); do
        kill -0 "$server_pid" 2>/dev/null || { echo 'Official StarRocks process exited.' >&2; return 1; }
        if [ "$fresh" = 1 ]; then
            if printf 'SELECT 1;\n' | MYSQL_PWD='' mysql --connect-timeout=2 -h127.0.0.1 -P9030 -uroot --batch --silent >/dev/null 2>&1; then ready=1; break; fi
        elif printf 'SELECT 1;\n' | sql >/dev/null 2>&1; then ready=1; break
        fi
        sleep 2
    done
    [ "$ready" = 1 ] || { echo 'StarRocks FE authentication/readiness failed; passwords were not reset.' >&2; return 1; }
    echo 'ADDP_STARROCKS_PHASE fe-ready'
    if [ "$fresh" = 1 ]; then
        printf "SET PASSWORD FOR 'root' = PASSWORD(%s);\n" "$(sql_string "$STARROCKS_PASSWORD")" |
            MYSQL_PWD='' mysql --connect-timeout=2 -h127.0.0.1 -P9030 -uroot --batch --silent >/dev/null
        printf 'administrator-initialized\n' > "$marker"
    fi
    echo 'ADDP_STARROCKS_PHASE admin-ready'
    supervisorctl -c "$SR_HOME/supervisor/supervisord.conf" start director >/dev/null
    ready=0
    for ((attempt=0; attempt<150; attempt++)); do
        if backend_ready; then ready=1; break; fi
        kill -0 "$server_pid" 2>/dev/null || return 1
        sleep 2
    done
    [ "$ready" = 1 ] || { echo 'StarRocks BE readiness failed.' >&2; return 1; }
    echo 'ADDP_STARROCKS_PHASE backend-ready'
    sql < /addp/starrocks/init.sql >/dev/null
    echo 'ADDP_STARROCKS_PHASE schema-ready'
    printf 'CREATE USER IF NOT EXISTS %s IDENTIFIED BY %s;\nGRANT SELECT ON ALL TABLES IN DATABASE business TO USER %s;\n' \
        "$(sql_string "$STARROCKS_USER")" "$(sql_string "$STARROCKS_APP_PASSWORD")" "$(sql_string "$STARROCKS_USER")" | sql >/dev/null
    # An existing account is not silently rotated; prove the configured password.
    printf 'SELECT COUNT(*) FROM business.source;\n' | MYSQL_PWD="$STARROCKS_APP_PASSWORD" mysql --connect-timeout=2 -h127.0.0.1 -P9030 -u"$STARROCKS_USER" --batch --silent >/dev/null
    printf 'initialized\n' > "$SR_HOME/fe/meta/.addp-business-initialized"
    echo 'ADDP_STARROCKS_PHASE reader-ready'
    echo 'Business StarRocks initialized with native FE/BE and SELECT-only reader.'
    local result=0
    wait "$server_pid" || result=$?
    trap - EXIT TERM INT
    return "$result"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then main "$@"; fi
