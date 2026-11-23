#!/usr/bin/env bash
# ADDP_T2_OWNED_SERVICES=postgres,mysql
# ADDP_T2_COMPOSE_FILE=scripts/test/docker-compose.agent-platform-database-t2.yml
# ADDP_T2_INPUT_FILES=common-python/addp_common/infra/ agent/backend/ copilot/backend/ scripts/test/python-platform-database.py
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
project="addp-python-disposable-$(openssl rand -hex 6)"
export ADDP_DATABASE_DEPLOY_USER=python_deploy ADDP_DATABASE_RUNTIME_USER=python_runtime TZ=Asia/Shanghai
ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 24)
PYTHON_GATE_MYSQL_ROOT_PASSWORD=$(openssl rand -hex 24)
export ADDP_DATABASE_DEPLOY_PASSWORD ADDP_DATABASE_RUNTIME_PASSWORD PYTHON_GATE_MYSQL_ROOT_PASSWORD
compose() { docker compose --env-file /dev/null -p "$project" -f "$root/scripts/test/docker-compose.agent-platform-database-t2.yml" "$@"; }
cleanup() { local status=$?; compose down --volumes --remove-orphans || status=1; exit "$status"; }
trap cleanup EXIT
for provider in postgres mysql; do
    export ADDP_INFRA_DATABASE_PROVIDER="$provider"
    compose up -d --wait "$provider"
    if [[ "$provider" == postgres ]]; then
        mapping=$(compose port postgres 5432)
        export POSTGRES_HOST=127.0.0.1 POSTGRES_PORT="${mapping##*:}" POSTGRES_DB=addp_python_disposable
        compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U python_deploy -d "$POSTGRES_DB" <<SQL
CREATE ROLE python_runtime LOGIN PASSWORD '$ADDP_DATABASE_RUNTIME_PASSWORD';
CREATE SCHEMA agent;
CREATE SCHEMA copilot;
GRANT USAGE ON SCHEMA agent,copilot TO python_runtime;
ALTER DEFAULT PRIVILEGES IN SCHEMA agent,copilot GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO python_runtime;
ALTER DEFAULT PRIVILEGES IN SCHEMA agent,copilot GRANT USAGE,SELECT ON SEQUENCES TO python_runtime;
SQL
    else
        mapping=$(compose port mysql 3306)
        export ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="${mapping##*:}"
        compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot' <<SQL
CREATE DATABASE agent CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
CREATE DATABASE copilot CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin;
CREATE USER 'python_deploy'@'%' IDENTIFIED BY '$ADDP_DATABASE_DEPLOY_PASSWORD';
CREATE USER 'python_runtime'@'%' IDENTIFIED BY '$ADDP_DATABASE_RUNTIME_PASSWORD';
GRANT ALL ON agent.* TO 'python_deploy'@'%';
GRANT ALL ON copilot.* TO 'python_deploy'@'%';
GRANT SELECT,INSERT,UPDATE,DELETE ON agent.* TO 'python_runtime'@'%';
GRANT SELECT,INSERT,UPDATE,DELETE ON copilot.* TO 'python_runtime'@'%';
SQL
    fi
    for owner in agent copilot; do
        if [[ "$owner" == agent ]]; then python=${ADDP_AGENT_PYTHON:-$root/agent/backend/venv/bin/python}; else python=${ADDP_COPILOT_PYTHON:-$root/copilot/backend/venv/bin/python}; fi
        (
            cd "$root/$owner/backend"
            export PYTHONPATH="$PWD:$root/common-python"
            runtime() {
                env -u ADDP_DATABASE_DEPLOY_USER -u ADDP_DATABASE_DEPLOY_PASSWORD \
                    "$python" "$root/scripts/test/python-platform-database.py" "$owner" "$1"
            }
            runtime unprepared
            if [[ "$owner" == agent ]]; then
                "$python" "$root/scripts/test/python-platform-database.py" agent prior
            fi
            "$python" -m prepare_database
            runtime write
            if [[ "$provider" == postgres ]]; then
                compose exec -T postgres psql -X -v ON_ERROR_STOP=1 -U python_deploy -d "$POSTGRES_DB" \
                    -c 'ALTER ROLE python_deploy SET default_transaction_read_only = on'
            fi
            "$python" -m prepare_database
            if [[ "$provider" == postgres ]]; then
                compose exec -T postgres env PGOPTIONS='-c default_transaction_read_only=off' \
                    psql -X -v ON_ERROR_STOP=1 -U python_deploy -d "$POSTGRES_DB" \
                    -c 'ALTER ROLE python_deploy RESET default_transaction_read_only'
            fi
            runtime read
            if [[ "$owner" == agent ]]; then
                "$python" "$root/scripts/test/python-platform-database.py" agent history
            fi
        )
    done
    compose down --volumes
done
