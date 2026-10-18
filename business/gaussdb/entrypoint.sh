#!/usr/bin/env bash
# One lifecycle for persistent Business and disposable native certification.
set -euo pipefail
umask 077
DATA_DIR=/data

validate_config() {
    GAUSSDB_USER=${GAUSSDB_USER:-business}
    [[ "$GAUSSDB_USER" =~ ^[a-z][a-z0-9_]{0,62}$ && "$GAUSSDB_USER" != omm && "$GAUSSDB_USER" != postgres && "$GAUSSDB_USER" != pg_* ]] || {
        echo 'GAUSSDB_USER 必须是普通业务账号名。' >&2; return 1;
    }
    local name password
    for name in GAUSSDB_PASSWORD GAUSSDB_APP_PASSWORD; do
        password=${!name:-}
        [[ ${#password} -ge 8 && ${#password} -le 32 && "$password" != *$'\n'* && "$password" != *$'\r'* ]] || {
            echo "$name 必填，长度须为8至32，且不得包含换行。" >&2; return 1;
        }
    done
    [ "$GAUSSDB_PASSWORD" != "$GAUSSDB_APP_PASSWORD" ] || { echo '管理员与业务密码必须不同。' >&2; return 1; }
}

sql_literal() {
    local value=${1//\\/\\\\}
    value=${value//\'/\'\'}
    printf "E'%s'" "$value"
}

local_sql() {
    gsql -X -v ON_ERROR_STOP=1 -h "$DATA_DIR"/run -p 5432 -U omm -d "${1:-postgres}" -At
}

tcp_sql() {
    local user=$1 password=$2 database=$3
    # libpq password file escapes ':' and '\'; never put a password in argv.
    local escaped=${password//\\/\\\\}
    escaped=${escaped//:/\\:}
    local passfile result=0
    passfile=$(mktemp "$DATA_DIR"/run/password.XXXXXX)
    printf '127.0.0.1:5432:%s:%s:%s\n' "$database" "$user" "$escaped" > "$passfile"
    PGPASSFILE="$passfile" PGCONNECT_TIMEOUT=5 gsql -X -w -v ON_ERROR_STOP=1 -h 127.0.0.1 -p 5432 -U "$user" -d "$database" -At || result=$?
    rm -f "$passfile"
    return "$result"
}

check_volume() {
    if [ -f "$DATA_DIR"/db/PG_VERSION ]; then
        [ -f "$DATA_DIR"/business-ready ] && [ "$(cat "$DATA_DIR"/business-ready)" = "${GAUSSDB_VERSION:?}|business|$GAUSSDB_USER" ] || {
            echo '已有数据库未完成初始化，或版本/账号不一致；保留数据并拒绝启动。' >&2; return 1;
        }
        fresh=false
    else
        [ ! -e "$DATA_DIR"/business-ready ] && [ -z "$(find "$DATA_DIR"/db -mindepth 1 -print -quit 2>/dev/null)" ] || {
            echo '数据目录包含未完成初始化的数据；拒绝重新初始化。' >&2; return 1;
        }
        fresh=true
    fi
}

main() {
    validate_config
    if [ "${1:-}" = --check-config ]; then return; fi
    if [ "${1:-}" = --health ]; then
        [ /proc/1/exe -ef "$GAUSSHOME/bin/gaussdb" ] || return 1
        [ -f "$DATA_DIR"/business-ready ] || return 1
        [ "$(printf 'SELECT 1;\n' | tcp_sql "$GAUSSDB_USER" "$GAUSSDB_APP_PASSWORD" business)" = 1 ]
        return
    fi
    [ "$#" = 0 ] || return 1
    check_volume
    mkdir -p "$DATA_DIR"/run "$GAUSSLOG"
    chmod 700 "$DATA_DIR"/run
    if [ "$fresh" = true ]; then
        local initial_password password_file
        initial_password="Ai9@$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')"
        password_file=$(mktemp "$DATA_DIR"/run/initial.XXXXXX)
        printf '%s\n' "$initial_password" > "$password_file"
        if ! gs_initdb -D "$DATA_DIR"/db --nodename=addp_business -U omm --pwfile="$password_file" -E UTF8 --locale=C; then
            rm -f "$password_file"; return 1
        fi
        rm -f "$password_file"
        unset initial_password
        # Local bootstrap socket is confined to omm's 0700 directory.
        printf 'local all omm trust\nhost all all 0.0.0.0/0 sha256\n' > "$DATA_DIR"/db/gs_hba.conf
    fi
    server_pid=
    gaussdb --single_node -D "$DATA_DIR"/db -p 5432 -c listen_addresses=127.0.0.1 -c unix_socket_directory="$DATA_DIR"/run -c max_connections=32 -c shared_buffers=128MB -c max_process_memory=2GB -c logging_collector=off &
    server_pid=$!
    trap 'kill -TERM "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true' EXIT
    trap 'exit 143' TERM
    trap 'exit 130' INT
    local ready=false attempt
    for ((attempt=0; attempt<120; attempt++)); do
        local status=0
        if [ "$fresh" = true ]; then
            printf 'ALTER ROLE omm PASSWORD %s;\n' "$(sql_literal "$GAUSSDB_PASSWORD")" | local_sql >/dev/null 2>&1 || status=$?
        else
            printf 'SELECT 1;\n' | local_sql >/dev/null 2>&1 || status=$?
        fi
        if [ "$status" = 0 ]; then ready=true; break; fi
        [ "$status" != 3 ] || { echo 'GaussDB 初始化 SQL 被拒绝，停止重试。' >&2; return 1; }
        kill -0 "$server_pid" 2>/dev/null || return 1
        sleep 2
    done
    [ "$ready" = true ] || { echo 'GaussDB 本地初始化就绪超时。' >&2; return 1; }
    if [ "$fresh" = true ]; then
        # Suppress native diagnostics which may quote password SQL on failure.
        if ! {
            printf 'CREATE USER "%s" PASSWORD %s;\n' "$GAUSSDB_USER" "$(sql_literal "$GAUSSDB_APP_PASSWORD")"
            printf 'CREATE DATABASE business OWNER "%s";\n' "$GAUSSDB_USER"
        } | local_sql >/dev/null 2>&1; then
            echo 'GaussDB 初始改密或业务账号初始化失败，数据已保留。' >&2; return 1
        fi
    fi
    printf 'SELECT 1;\n' | tcp_sql omm "$GAUSSDB_PASSWORD" postgres >/dev/null 2>&1 || {
        echo '管理员密码校验失败；不自动重置已有数据库。' >&2; return 1;
    }
    if ! {
        printf 'CREATE SCHEMA IF NOT EXISTS addp_business AUTHORIZATION "%s";\nSET search_path=addp_business;\n' "$GAUSSDB_USER"
        cat /addp/gaussdb/init.sql
    } | tcp_sql "$GAUSSDB_USER" "$GAUSSDB_APP_PASSWORD" business >/dev/null 2>&1; then
        echo '普通账号登录或样例初始化失败；不覆盖现有数据。' >&2; return 1
    fi
    printf '%s|business|%s\n' "${GAUSSDB_VERSION:?}" "$GAUSSDB_USER" > "$DATA_DIR"/business-ready
    kill -TERM "$server_pid"
    wait "$server_pid"
    trap - EXIT TERM INT
    unset GAUSSDB_PASSWORD GAUSSDB_APP_PASSWORD
    exec gaussdb --single_node -D "$DATA_DIR"/db -p 5432 -c listen_addresses='*' -c unix_socket_directory="$DATA_DIR"/run -c max_connections=32 -c shared_buffers=128MB -c max_process_memory=2GB -c logging_collector=off
}

if [[ "${BASH_SOURCE[0]}" = "$0" ]]; then main "$@"; fi
