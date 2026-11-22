#!/usr/bin/env bash
# Invoked by the gate that owns the MySQL container and the gateway database.
set -euo pipefail
work=$1
container=$2
export ADDP_INFRA_DATABASE_PROVIDER=mysql ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$3"
export ADDP_MYSQL_INSTALL_USER=gateway_setup ADDP_MYSQL_GATEWAY_USER=gateway_runtime
ADDP_MYSQL_INSTALL_PASSWORD=$(openssl rand -hex 24)
ADDP_MYSQL_GATEWAY_PASSWORD=${ADDP_TEST_GATEWAY_RUNTIME_PASSWORD:-$(openssl rand -hex 24)}
export ADDP_MYSQL_INSTALL_PASSWORD ADDP_MYSQL_GATEWAY_PASSWORD
admin() {
    docker exec -i "$container" sh -c 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot -NB gateway'
}
admin <<SQL
CREATE USER 'gateway_setup'@'%' IDENTIFIED BY '$ADDP_MYSQL_INSTALL_PASSWORD';
GRANT CREATE, SELECT, INSERT, UPDATE ON gateway.* TO 'gateway_setup'@'%';
CREATE USER 'gateway_runtime'@'%' IDENTIFIED BY '$ADDP_MYSQL_GATEWAY_PASSWORD';
SQL
"$work/gateway" install-mysql-gateway
"$work/gateway" install-mysql-gateway
unset ADDP_MYSQL_INSTALL_USER ADDP_MYSQL_INSTALL_PASSWORD
admin <<'SQL'
GRANT SELECT ON gateway.startup_schema_revision TO 'gateway_runtime'@'%';
GRANT INSERT ON gateway.api_access_logs TO 'gateway_runtime'@'%';
SQL
(cd "$(dirname "$0")/../../gateway" && ADDP_TEST_GATEWAY_MYSQL=1 GOWORK=off go test ./internal/router -run '^TestGatewayMySQLRuntime$' -count=1 -v) | tee "$work/gateway-runtime.log"
grep -q '^--- PASS: TestGatewayMySQLRuntime ' "$work/gateway-runtime.log"
test "$(admin <<<"SELECT COUNT(*) FROM api_access_logs WHERE service_name='gateway-install-probe'")" = 1
for forbidden in 'DELETE FROM api_access_logs WHERE 1=0' 'UPDATE startup_schema_revision SET version=0 WHERE 1=0' 'SELECT id FROM api_access_logs LIMIT 0'; do
    if MYSQL_PWD="$ADDP_MYSQL_GATEWAY_PASSWORD" docker exec -i -e MYSQL_PWD "$container" mysql -h127.0.0.1 -ugateway_runtime gateway <<<"$forbidden" 2>"$work/gateway-denied.err"; then
        echo "unexpected Gateway permission for: $forbidden" >&2
        exit 1
    fi
    grep -Eq '^ERROR (1142|1143)' "$work/gateway-denied.err"
done
echo 'Gateway runtime: verified installed revision and appended access log with isolated credentials'
