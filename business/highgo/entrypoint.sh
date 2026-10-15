#!/usr/bin/env bash
# 固定社区镜像的唯一 Business 初始化入口；不执行镜像内的安全降级脚本。
set -Eeo pipefail

addp_highgo_validate_password() {
    if [ -z "${POSTGRES_PASSWORD:-}" ] || [[ "$POSTGRES_PASSWORD" == *$'\n'* ]] || [[ "$POSTGRES_PASSWORD" == *$'\r'* ]]; then
        echo 'HIGHGO_PASSWORD 必填，且不得包含换行。' >&2
        return 1
    fi
}

addp_highgo_install_initializers() {
    docker_init_database_dir() {
        local password_file result=0
        addp_highgo_validate_password || return 1
        password_file=$(mktemp) || return 1
        if ! chmod 0600 "$password_file" || ! printf '%s\n%s\n%s\n' "$POSTGRES_PASSWORD" "$POSTGRES_PASSWORD" "$POSTGRES_PASSWORD" > "$password_file"; then
            rm -f "$password_file"
            return 1
        fi
        # 三权账号直接用部署密码初始化，不经过硬编码口令或 SQL 插值。
        initdb --pwfile="$password_file" -A sm3 -E UTF8 || result=$?
        rm -f "$password_file" || return 1
        [ "$result" -eq 0 ] || return "$result"
        cp /home/highgo/root.crt /home/highgo/server.crt /home/highgo/server.key "$PGDATA/" || return 1
        chmod 0600 "$PGDATA/server.key"
    }

    pg_setup_hba_conf() {
        cat > "$PGDATA/pg_hba.conf" <<'HBA' || return 1
local   all all                 sm3
hostssl all all 0.0.0.0/0       sm3
hostssl all all ::/0            sm3
HBA
        chmod 0600 "$PGDATA/pg_hba.conf"
    }
}

addp_highgo_main() {
    [ "${1:-}" = postgres ] || { echo '瀚高入口只接受 postgres 启动命令。' >&2; return 1; }
    # shellcheck source=/dev/null
    source /usr/local/bin/docker-entrypoint.sh
    addp_highgo_install_initializers
    docker_setup_env
    addp_highgo_validate_password || return 1
    if [ "$(id -u)" = 0 ]; then
        docker_create_db_directories
        # 从本入口重新进入，确保降权后仍使用已替换的初始化函数。
        exec gosu highgo bash "$0" "$@"
    fi
    _main "$@"
}

if [[ "${BASH_SOURCE[0]}" = "$0" ]]; then
    addp_highgo_main "$@"
fi
