#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=redis
# ADDP_T2_LIFECYCLE_SCRIPT=scripts/test/redis-owned-fixture.sh
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.redis-t2.yml
# ADDP_T2_INPUT_FILES=business/redis/ business/docker-compose.yml scripts/test/redis-owned-fixture.sh scripts/test/common-redis-contract.sh common/engine/plugin/ common/engine/plugins/redis/ common/datatype/ common/resourcetree/ system/backend/ meta/backend/internal/scanruntime/ manager/backend/internal/preview/ manager/backend/internal/api/explorer_handler.go manager/backend/internal/models/models.go scripts/utils/register-business.sh
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
bash "$ROOT_DIR/scripts/test/redis-owned-fixture.sh" bash "$ROOT_DIR/scripts/test/common-redis-contract.sh"
