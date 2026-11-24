#!/usr/bin/env bash
# Both provider gates pass the ID of their own fresh database container.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
container=${1:?expected gate-owned container ID}
provider=${ADDP_INFRA_DATABASE_PROVIDER:?}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT


case "$provider" in
  postgres)
    # This fixture creates roles. Only the dedicated Actions service can enter.
    [[ ${GITHUB_ACTIONS:-} == true && $container == "${ORCHESTRATOR_CI_PG_ID:?}" && $container =~ ^[a-f0-9]{64}$ ]]
    # Leave the service's original database empty for the legacy migration suite.
    docker exec "$container" psql -Xw -v ON_ERROR_STOP=1 -U addp_ci -d postgres \
      -c 'CREATE DATABASE addp_orchestrator_deployment'
    export POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=5432 POSTGRES_DB=addp_orchestrator_deployment
    export ADDP_DATABASE_DEPLOY_USER=orchestrator_install ADDP_DATABASE_RUNTIME_USER=platform_run
    ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
    ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 24)
    export ADDP_DATABASE_DEPLOY_PASSWORD ADDP_DATABASE_RUNTIME_PASSWORD
    pg() { docker exec -i "$container" psql -Xw -v ON_ERROR_STOP=1 -U addp_ci -d "$POSTGRES_DB" "$@"; }
    pg <<SQL
CREATE ROLE orchestrator_install LOGIN PASSWORD '$ADDP_DATABASE_DEPLOY_PASSWORD';
CREATE ROLE platform_run LOGIN PASSWORD '$ADDP_DATABASE_RUNTIME_PASSWORD';
GRANT CREATE ON DATABASE addp_orchestrator_deployment TO orchestrator_install;
CREATE SCHEMA common AUTHORIZATION orchestrator_install;
CREATE SCHEMA orchestrator AUTHORIZATION orchestrator_install;
GRANT USAGE ON SCHEMA common,orchestrator TO platform_run;
ALTER DEFAULT PRIVILEGES FOR ROLE orchestrator_install IN SCHEMA common,orchestrator
    GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO platform_run;
ALTER DEFAULT PRIVILEGES FOR ROLE orchestrator_install IN SCHEMA common,orchestrator
    GRANT SELECT,USAGE ON SEQUENCES TO platform_run;
SQL
    snapshot() {
      docker exec "$container" pg_dump -s -n common -n orchestrator -U addp_ci "$POSTGRES_DB" |
        sed -E '/^\\(un)?restrict /d'
    }
    ;;
  mysql)
    [[ $container == addp-common-mysql-t2-* && ${ADDP_MYSQL_HOST:?} == 127.0.0.1 ]]
    snapshot() {
      docker exec "$container" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysqldump -uroot --no-data --skip-dump-date --no-tablespaces --databases common orchestrator'
    }
    ;;
  *) echo 'The gate requires a disposable PG or MySQL database' >&2; exit 2 ;;
esac

(cd "$root/system/backend" && GOWORK=off go build -o "$work/system" ./cmd/server)
(cd "$root/orchestrator/backend" && GOWORK=off go build -o "$work/orchestrator" ./cmd/server)
cd "$work"
runtime() { python3 "$root/scripts/test/orchestrator-platform-startup.py" "$work/orchestrator" "$@"; }
# Use the real Orchestrator executable as the install entrypoint. The fixture
# reuses the database container's image and network, so the gate needs no registry.
install_dir="$work/linux"
mkdir "$install_dir"
arch=$(docker image inspect "$(docker inspect -f '{{.Image}}' "$container")" -f '{{.Architecture}}')
(cd "$root/orchestrator/backend" && CGO_ENABLED=0 GOWORK=off GOOS=linux GOARCH="$arch" go build -o "$install_dir/server" ./cmd/server)
export ORCHESTRATOR_GATE_IMAGE
ORCHESTRATOR_GATE_IMAGE=$(docker inspect -f '{{.Config.Image}}' "$container")
export ORCHESTRATOR_GATE_CONTAINER="$container" ORCHESTRATOR_GATE_BIN="$install_dir"
cat >compose.yml <<'YAML'
services:
  orchestrator-backend:
    image: ${ORCHESTRATOR_GATE_IMAGE:?}
    pull_policy: never
    network_mode: container:${ORCHESTRATOR_GATE_CONTAINER:?}
    volumes:
      - ${ORCHESTRATOR_GATE_BIN:?}:/app:ro
    environment:
      ADDP_INFRA_DATABASE_PROVIDER: ${ADDP_INFRA_DATABASE_PROVIDER:?}
      POSTGRES_HOST: 127.0.0.1
      POSTGRES_PORT: "5432"
      POSTGRES_DB: ${POSTGRES_DB:-addp}
      ADDP_MYSQL_HOST: 127.0.0.1
      ADDP_MYSQL_PORT: "3306"
YAML
runtime missing-credentials
snapshot >before-common.sql
runtime missing common
snapshot >refused.sql
cmp before-common.sql refused.sql
"$work/system" prepare-database common
snapshot >before-owner.sql
runtime missing orchestrator
snapshot >refused.sql
cmp before-owner.sql refused.sql
# The same release database phase and the same module command run on both providers.
bash "$root/scripts/prod/prepare-module-databases.sh" selected -p "orch-install-$$" -f "$work/compose.yml"
snapshot >installed.sql
grep -qi 'CREATE TABLE.*orchestrations' installed.sql
for cycle in 1 2; do
  runtime ready
  snapshot >restarted.sql
  cmp installed.sql restarted.sql
  echo "$provider: runtime cycle $cycle exited normally; Common and Orchestrator schema SHA-256 $(sha256sum restarted.sql | cut -d' ' -f1)"
done
