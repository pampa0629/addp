#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTAINER="${ORACLE_CONTAINER:-business-oracle}"
ORACLE_USER="${ORACLE_APP_USER:-business}"
ORACLE_PASSWORD="${ORACLE_APP_PASSWORD:-business_oracle_password}"
ORACLE_SERVICE="${ORACLE_SERVICE_NAME:-FREEPDB1}"

echo "=== Oracle 普通表测试数据初始化开始 ==="
docker exec -i \
  -e ADDP_ORACLE_USER="${ORACLE_USER}" \
  -e ADDP_ORACLE_PASSWORD="${ORACLE_PASSWORD}" \
  -e ADDP_ORACLE_SERVICE="${ORACLE_SERVICE}" \
  "${CONTAINER}" bash -c \
  'sqlplus -s "${ADDP_ORACLE_USER}/${ADDP_ORACLE_PASSWORD}@//localhost:1521/${ADDP_ORACLE_SERVICE}"' \
  < "${SCRIPT_DIR}/init.sql"

echo "Oracle 普通表测试数据初始化完成"
