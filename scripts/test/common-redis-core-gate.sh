#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=redis-infra,redis-core-source
# ADDP_T2_LIFECYCLE_SCRIPT=scripts/test/redis-owned-fixture.sh
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.redis-t2.yml
# ADDP_T2_INPUT_FILES=scripts/test/redis-core-http-contract.json scripts/test/redis-core-http-online.py scripts/test/redis-core-http-online_test.py scripts/test/redis-core-transport.py scripts/test/redis-core-identity.patch scripts/test/redis-core-identity.go scripts/test/redis-core-identity-test.go scripts/test/redis-core-system-wiring.go scripts/test/redis-core-gateway-wiring.go scripts/test/redis-core-http-preparation.py scripts/test/online-hosted-redis-core-gate.sh scripts/test/redis-core-gate_test.py common/infra/bootstrap/ common/infra/redistest/ common/config/ common/modulelifecycle/ common/middleware/ratelimit/ system/backend/ gateway/ scripts/test/redis-owned-fixture.sh scripts/test/common-redis-core-contract.sh scripts/test/docker-compose.redis-t2.yml scripts/infra/redis-authentication.sh
set -euo pipefail
: "${ADDP_SYSTEM_POSTGRES_TEST_DSN:?disposable System IAM PostgreSQL gate DSN required}"
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
export ADDP_REDIS_CORE_T2=1 ADDP_REDIS_CORE_SOURCE_T2=1
bash "$ROOT_DIR/scripts/test/redis-owned-fixture.sh" bash "$ROOT_DIR/scripts/test/common-redis-core-contract.sh"
