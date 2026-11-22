#!/usr/bin/env bash
# Invoked by the gate that owns the MySQL container and the gateway database.
set -euo pipefail
work=$1
container=$2
export ADDP_INFRA_DATABASE_PROVIDER=mysql ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$3"
admin() {
    docker exec -i "$container" sh -c 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=TCP -h127.0.0.1 -uroot -NB gateway'
}
"$work/gateway" prepare-database
"$work/gateway" prepare-database
unset ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD
(cd "$(dirname "$0")/../../gateway" && ADDP_TEST_GATEWAY_MYSQL=1 GOWORK=off go test ./internal/router -run '^TestGatewayMySQLRuntime$' -count=1 -v) | tee "$work/gateway-runtime.log"
grep -q '^--- PASS: TestGatewayMySQLRuntime ' "$work/gateway-runtime.log"
test "$(admin <<<"SELECT COUNT(*) FROM api_access_logs WHERE service_name='gateway-install-probe'")" = 1
echo 'Gateway runtime verified the installed revision and appended an access log'
