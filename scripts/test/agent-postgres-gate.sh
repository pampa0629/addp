#!/usr/bin/env bash
# ADDP_T2_SERVICES=postgres
# ADDP_T2_REQUIRED_ENV=AGENT_POSTGRES_TEST_DSN
# Agent schema migration and session integration gate. Fixtures always roll back.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
: "${AGENT_POSTGRES_TEST_DSN:?AGENT_POSTGRES_TEST_DSN must select an allowed test database}"
source "$ROOT_DIR/scripts/infra/ports.sh"
addp_infra_verify_test_postgres_dsn "$AGENT_POSTGRES_TEST_DSN"
PYTHON=${ADDP_AGENT_PYTHON:-$ROOT_DIR/agent/backend/venv/bin/python}
export PYTHONPATH="$ROOT_DIR/agent/backend:$ROOT_DIR/common-python"
exec "$PYTHON" "$ROOT_DIR/agent/backend/integration/postgres_gate.py"
