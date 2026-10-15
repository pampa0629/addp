#!/usr/bin/env bash
# 新卷与已有卷共用的 Business 普通账号初始化；不关闭三权、安全策略或审计。
set -euo pipefail
HIGHGO_USER=${HIGHGO_USER:-business}
HIGHGO_APP_VALID_UNTIL=${HIGHGO_APP_VALID_UNTIL:-}
[[ "$HIGHGO_USER" =~ ^[a-z][a-z0-9_]{0,62}$ ]] && [[ "$HIGHGO_USER" != pg_* ]] &&
    [[ "$HIGHGO_USER" != sysdba && "$HIGHGO_USER" != syssso && "$HIGHGO_USER" != syssao ]] || {
    echo 'HIGHGO_USER 必须是普通业务账号名。' >&2; exit 1;
}
for variable in POSTGRES_PASSWORD HIGHGO_APP_PASSWORD; do
    password=${!variable:-}
    [[ -n "$password" && "$password" != *$'\n'* && "$password" != *$'\r'* ]] || {
        echo "$variable 必填，且不得包含换行。" >&2; exit 1;
    }
done
[ "$POSTGRES_PASSWORD" != "$HIGHGO_APP_PASSWORD" ] || { echo '业务密码必须与管理员密码不同。' >&2; exit 1; }
[[ -z "$HIGHGO_APP_VALID_UNTIL" || "$HIGHGO_APP_VALID_UNTIL" = infinity ]] || {
    echo 'HIGHGO_APP_VALID_UNTIL 只接受空值或 infinity。' >&2; exit 1;
}

case "${1:-}" in
    --check-config) exit 0 ;;
    '') [ "$#" -eq 0 ] || exit 1 ;;
    *) echo '用法：01-business.sh [--check-config]' >&2; exit 1 ;;
esac

sql() {
    local role=$1 password=$2
    shift 2
    PGPASSWORD="$password" psql -X --no-password -v ON_ERROR_STOP=1 -p 5866 -d highgo -U "$role" \
        --set=app_user="$HIGHGO_USER" "$@"
}
password_sql() {
    local role=$1 statement=$2 escaped_password
    escaped_password=${HIGHGO_APP_PASSWORD//\\/\\\\}
    escaped_password=${escaped_password//\'/\\\'}
    # psql 元命令与 SQL 分别负责变量输入和安全引用，不将密码放入 argv 或文件。
    # 密码语句的诊断可能含 SQL 原文，因此失败只向调用者返回不含秘密的错误。
    if ! {
        printf "\\set app_password '%s'\n" "$escaped_password"
        printf '%s\n' "$statement"
    } | sql "$role" "$POSTGRES_PASSWORD" -q >/dev/null 2>&1; then
        echo '业务账号密码初始化失败，请检查原生口令策略、密码历史和管理账号权限。' >&2
        return 1
    fi
}

state=$(sql sysdba "$POSTGRES_PASSWORD" -At <<'SQL'
SELECT CASE WHEN rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls
                   OR NOT rolcanlogin OR EXISTS (SELECT 1 FROM pg_auth_members WHERE member = r.oid)
            THEN 'unsafe' ELSE 'ordinary' END
FROM pg_roles r WHERE rolname = :'app_user';
SQL
)
case "$state" in
    '')
        password_sql sysdba "$(cat <<'SQL'
CREATE USER :"app_user" PASSWORD :'app_password';
SQL
)"
        rotate=false ;;
    ordinary)
        # 只做一次登录探测；不为重复启动反复触发口令历史检查。
        if sql "$HIGHGO_USER" "$HIGHGO_APP_PASSWORD" -Atc 'SELECT 1' >/dev/null 2>&1; then
            rotate=false
        else
            rotate=true
        fi ;;
    *) echo '同名账号不是无管理属性、无角色成员关系的普通账号，拒绝初始化。' >&2; exit 1 ;;
esac
if [ "$rotate" = true ]; then
    password_sql syssso "$(cat <<'SQL'
ALTER USER :"app_user" PASSWORD :'app_password';
SQL
)"
fi
if [ -n "$HIGHGO_APP_VALID_UNTIL" ]; then
    sql syssso "$POSTGRES_PASSWORD" -q --set=valid_until="$HIGHGO_APP_VALID_UNTIL" <<'SQL'
ALTER USER :"app_user" VALID UNTIL :'valid_until';
SQL
fi
sql sysdba "$POSTGRES_PASSWORD" -q <<'SQL'
BEGIN;
GRANT CONNECT, CREATE, TEMPORARY ON DATABASE highgo TO :"app_user";
CREATE SCHEMA IF NOT EXISTS addp_business AUTHORIZATION :"app_user";
SELECT format('ALTER SCHEMA addp_business OWNER TO %I', :'app_user')
FROM pg_namespace WHERE nspname = 'addp_business' AND pg_get_userbyid(nspowner) = 'sysdba'
\gexec
SELECT format('ALTER TABLE addp_business.addp_engine_probe OWNER TO %I', :'app_user')
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname='addp_business' AND c.relname='addp_engine_probe' AND pg_get_userbyid(c.relowner)='sysdba'
\gexec
COMMIT;
SQL
sql "$HIGHGO_USER" "$HIGHGO_APP_PASSWORD" -q -f "$(dirname "$0")/init.sql"
sql "$HIGHGO_USER" "$HIGHGO_APP_PASSWORD" -Atc 'SELECT engine_name FROM addp_business.addp_engine_probe WHERE id=1' |
    grep -Fxq 'HighGo V4.5 Business'
echo "瀚高业务账号 $HIGHGO_USER 和样例已就绪。"
