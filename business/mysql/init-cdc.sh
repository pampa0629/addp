#!/bin/bash
# Idempotently provision the dedicated Debezium MySQL account.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ -f "$PROJECT_ROOT/.env" ]; then
    set -a
    # shellcheck disable=SC1091
    source "$PROJECT_ROOT/.env"
    set +a
fi

MYSQL_CDC_USER="${MYSQL_CDC_USER:-addp_cdc}"
MYSQL_CDC_PASSWORD="${MYSQL_CDC_PASSWORD:-}"
MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-password}"

if [[ ! "$MYSQL_CDC_USER" =~ ^[A-Za-z0-9_]+$ ]]; then
    echo "MySQL CDC 用户名只能包含字母、数字和下划线" >&2
    exit 1
fi
if [ -z "$MYSQL_CDC_PASSWORD" ]; then
    echo "MYSQL_CDC_PASSWORD 不能为空" >&2
    exit 1
fi
if ! docker ps --filter "name=^/business-mysql$" --format '{{.Names}}' | grep -qx 'business-mysql'; then
    echo "business-mysql 容器未运行" >&2
    exit 1
fi

PASSWORD_HEX="$(printf '%s' "$MYSQL_CDC_PASSWORD" | od -An -tx1 | tr -d ' \n')"

docker exec -i -e MYSQL_PWD="$MYSQL_ROOT_PASSWORD" business-mysql \
    mysql -h127.0.0.1 -uroot --protocol=TCP <<SQL
SET @cdc_password = CONVERT(0x${PASSWORD_HEX} USING utf8mb4);
SET @create_user = CONCAT(
  "CREATE USER IF NOT EXISTS '${MYSQL_CDC_USER}'@'%' IDENTIFIED BY ",
  QUOTE(@cdc_password)
);
PREPARE create_user_stmt FROM @create_user;
EXECUTE create_user_stmt;
DEALLOCATE PREPARE create_user_stmt;

SET @alter_user = CONCAT(
  "ALTER USER '${MYSQL_CDC_USER}'@'%' IDENTIFIED BY ",
  QUOTE(@cdc_password)
);
PREPARE alter_user_stmt FROM @alter_user;
EXECUTE alter_user_stmt;
DEALLOCATE PREPARE alter_user_stmt;

REVOKE ALL PRIVILEGES, GRANT OPTION FROM '${MYSQL_CDC_USER}'@'%';
GRANT SELECT, RELOAD, SHOW DATABASES, REPLICATION SLAVE, REPLICATION CLIENT, LOCK TABLES
  ON *.* TO '${MYSQL_CDC_USER}'@'%';
SQL

echo "MySQL CDC 用户 ${MYSQL_CDC_USER}@% 已初始化"
