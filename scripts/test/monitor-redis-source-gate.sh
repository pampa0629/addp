#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=redis
# ADDP_T2_LIFECYCLE_SCRIPT=scripts/test/redis-owned-fixture.sh
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.redis-t2.yml
# ADDP_T2_INPUT_FILES=monitor/backend/internal/redissource/ monitor/backend/go.mod monitor/backend/go.sum business/redis/ scripts/test/redis-owned-fixture.sh scripts/test/common-redis-contract.sh scripts/test/docker-compose.redis-t2.yml
# Deployment-side fixed INFO/ACL proof; no personal instance or platform identity.
set -euo pipefail
[[ $# == 0 ]] || { echo 'Redis observation T2: unexpected arguments' >&2; exit 2; }
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
bash "$ROOT_DIR/scripts/test/redis-owned-fixture.sh" bash "$ROOT_DIR/scripts/test/common-redis-contract.sh" --monitor-only
