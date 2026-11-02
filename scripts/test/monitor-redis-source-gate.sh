#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=redis,redis-infra,promtool
# ADDP_T2_LIFECYCLE_SCRIPT=scripts/test/redis-owned-fixture.sh
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.redis-t2.yml
# ADDP_T2_INPUT_FILES=monitor/backend/internal/sourceserver/ scripts/utils/metrics-source.py scripts/test/metrics-source-fixture.py monitor/backend/internal/resourcequery/ monitor/backend/internal/redissource/ monitor/backend/cmd/redis-metrics-source/ monitor/backend/go.mod monitor/backend/go.sum common/go.mod common/go.sum business/redis/ business/docker-compose.yml business/scripts/ business/.env.example scripts/infra/redis-metrics.py scripts/infra/redis-authentication.sh scripts/infra/Dockerfile.redis-metrics scripts/utils/private-state.py scripts/utils/deployment-docker.py scripts/utils/deployment-inventory.py scripts/test/redis-owned-fixture.sh scripts/test/common-redis-contract.sh scripts/test/docker-compose.redis-t2.yml scripts/test/redis-metrics-lifecycle-probe.py scripts/test/redis-metrics-lifecycle_test.py docker-compose.infra.yml scripts/infra/up.sh scripts/infra/down.sh .env.example
# Deployment-side fixed INFO/ACL proof; no personal instance or platform identity.
set -euo pipefail
[[ $# == 0 ]] || { echo 'Redis observation T2: unexpected arguments' >&2; exit 2; }
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
python3 -m unittest "$ROOT_DIR/scripts/test/redis-metrics-lifecycle_test.py"
export ADDP_REDIS_OBSERVATION_T2=1
bash "$ROOT_DIR/scripts/test/redis-owned-fixture.sh" bash "$ROOT_DIR/scripts/test/common-redis-contract.sh" --monitor-only
