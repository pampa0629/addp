#!/usr/bin/env bash
# MySQL Compose release, on a disposable GitHub runner.
set -euo pipefail
umask 077
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
[[ "${GITHUB_ACTIONS:-}" == true && "${GITHUB_EVENT_NAME:-}" == workflow_dispatch &&
   "${RUNNER_ENVIRONMENT:-}" == github-hosted && "${RUNNER_OS:-}" == Linux && "${RUNNER_ARCH:-}" == X64 ]]
[[ $(git rev-parse HEAD) == "${GITHUB_SHA:?}" && -z $(git status --porcelain) && ! -e .env ]]
[[ "${GITHUB_RUN_ID:?}" =~ ^[0-9]+$ && "${GITHUB_RUN_ATTEMPT:?}" =~ ^[0-9]+$ ]]
docker info >/dev/null

export ADDP_DEPLOYMENT_SCOPE=system-meta-gateway ADDP_DEPLOYMENT_MODULES=
export ADDP_MYSQL_ONLINE_SUITE="${ADDP_MYSQL_ONLINE_SUITE:-gateway}"
case "$ADDP_MYSQL_ONLINE_SUITE" in
  gateway) ;;
  service-lineage) ADDP_DEPLOYMENT_MODULES=service ;;
  manager) ADDP_DEPLOYMENT_MODULES=manager,security ;;
  develop-file) ADDP_DEPLOYMENT_MODULES=develop,security ;;
  quality-worker) ADDP_DEPLOYMENT_MODULES=quality,monitor; export TZ=UTC ;;
  all-modules)
    ADDP_DEPLOYMENT_MODULES=agent,asset,catalog,copilot,develop,graph,inference,manager,model,monitor,ontology,orchestrator,portal,quality,security,service,standard,system,transfer,workbench
    export TZ=UTC POSTGRES_HOST=unavailable.invalid
    ;;
  full-release)
    export ADDP_DEPLOYMENT_SCOPE=full TZ=UTC POSTGRES_HOST=unavailable.invalid
    export ADDP_SUPERMAP_WORKFLOW_ENABLED=false INFRA_BIND_HOST=127.0.0.1
    ;;
  *) echo "unknown Hosted MySQL suite" >&2; exit 2 ;;
esac
export ADDP_DEPLOYMENT_PROJECT="addp-mysql-platform-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
export ADDP_DEPLOYMENT_NETWORK="$ADDP_DEPLOYMENT_PROJECT" ADDP_INFRA_DATABASE_PROVIDER=mysql
if [[ "$ADDP_MYSQL_ONLINE_SUITE" == full-release ]]; then
  export ADDP_DEPLOYMENT_PROJECT=addp-platform ADDP_DEPLOYMENT_NETWORK=addp-network
fi
export NGINX_BIND_HOST=127.0.0.1 NGINX_PORT=18080 ADDP_PUBLIC_ORIGIN=http://127.0.0.1:18080
export ALLOWED_ORIGINS="$ADDP_PUBLIC_ORIGIN" CONSOLE_URL="$ADDP_PUBLIC_ORIGIN" PUBLIC_API_URL="$ADDP_PUBLIC_ORIGIN"
export ADDP_MYSQL_HOST=mysql ADDP_MYSQL_PORT=3306 ADDP_MYSQL_SSLMODE=disable
export REDIS_HOST=redis REDIS_PORT=6379
export ADDP_DATABASE_RUNTIME_USER=platform_runtime ADDP_DATABASE_DEPLOY_USER=platform_deploy
export ADDP_DATABASE_RUNTIME_PASSWORD ADDP_DATABASE_DEPLOY_PASSWORD MYSQL_ROOT_PASSWORD
ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 24)
ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
MYSQL_ROOT_PASSWORD=$(openssl rand -hex 24)
export ADDP_OBSERVABILITY_LOGS_ENABLED=false

artifacts="${ADDP_ONLINE_ARTIFACT_DIR:?}"
mkdir -p "$artifacts"
work=$(mktemp -d "${RUNNER_TEMP:?}/addp-mysql-platform.XXXXXXXX")
export ADDP_ONLINE_SECRET_DIR="$work"
export ADDP_MYSQL_FIXTURE_PROJECT="addp-mysql-platform-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}-infra"
export ADDP_MYSQL_FIXTURE_NETWORK="$ADDP_DEPLOYMENT_NETWORK"
if [[ "$ADDP_MYSQL_ONLINE_SUITE" == full-release ]]; then
  export ADDP_MYSQL_FIXTURE_NETWORK="$ADDP_MYSQL_FIXTURE_PROJECT"
fi
if [[ "$ADDP_DEPLOYMENT_MODULES" == develop,security ]]; then
  export ADDP_ONLINE_TEST_RUN_ID="develop-file-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
  export GEOPYTHON_FIXTURE_TAG="hosted-${GITHUB_SHA}"
fi
develop_file_fixture() {
  # These flags select the fixture's Hosted guard. The limited release uses
  # the production environment it initialized in the disposable checkout.
  ADDP_ONLINE_HOST=1 ADDP_ONLINE_HOSTED=1 python3 -m scripts.test.mysql-develop-file-online "$@"
}
fixture_compose=(docker compose --env-file /dev/null -f "$work/compose.yml")
fixture() { "${fixture_compose[@]}" "$@"; }
app=(docker compose --env-file .env -f scripts/prod/system-meta-gateway.yml)
if [[ "$ADDP_MYSQL_ONLINE_SUITE" == full-release ]]; then
  app=(docker compose --env-file .env -f docker-compose.yml)
fi
if [[ -n "$ADDP_DEPLOYMENT_MODULES" ]]; then
  IFS=, read -r -a extra_modules <<< "$ADDP_DEPLOYMENT_MODULES"
  for module in "${extra_modules[@]}"; do
    app+=(--profile "module-$module")
  done
fi
infra_started=false
release_started=false
full_network_id=
stage=fixture
cleanup() {
  local status=$? cleanup_status=passed project remaining kind
  trap - EXIT INT TERM
  set +e
  if [[ "$release_started" == true && "$ADDP_MYSQL_ONLINE_SUITE" == full-release ]]; then
    docker compose --env-file .env -f docker-compose.runtimes.yml down --volumes --remove-orphans || cleanup_status=failed
  fi
  if [ "$release_started" = true ]; then
    if [[ "$status" != 0 && "$stage" == deploy ]]; then
      # Report application error messages before removing the failed containers.
      # JSON attributes may contain sensitive values, so retain only static messages.
      "${app[@]}" logs --no-color --no-log-prefix --tail 60 2>/dev/null | python3 -c '
import json
import sys

messages = set()
for line in sys.stdin:
    try:
        record = json.loads(line)
    except ValueError:
        continue
    if isinstance(record, dict) and record.get("level") == "ERROR" and isinstance(record.get("msg"), str):
        messages.add(record["msg"])
for message in sorted(messages):
    print(f"Application startup error: {message}")
'
    fi
    "${app[@]}" down --volumes --remove-orphans || cleanup_status=failed
  fi
  if [ "$infra_started" = true ]; then
    fixture down --volumes --remove-orphans || cleanup_status=failed
  fi
  if [[ -n "$full_network_id" ]]; then
    docker compose --env-file .env -f docker-compose.infra.yml down --volumes --remove-orphans || cleanup_status=failed
    if docker network inspect "$full_network_id" >/dev/null 2>&1; then
      docker network rm "$full_network_id" || cleanup_status=failed
    fi
  fi
  if [[ "$ADDP_DEPLOYMENT_MODULES" == develop,security ]]; then
    develop_file_fixture stop-nfs > "$artifacts/develop-nfs-cleanup.json" || cleanup_status=failed
  fi
  local projects=("$ADDP_DEPLOYMENT_PROJECT" "$ADDP_MYSQL_FIXTURE_PROJECT")
  if [[ -n "$full_network_id" ]]; then projects+=(addp-infra addp-runtimes); fi
  for project in "${projects[@]}"; do
    remaining=$(docker ps -aq --filter "label=com.docker.compose.project=$project") || cleanup_status=failed
    [[ -z "$remaining" ]] || cleanup_status=failed
    for kind in network volume; do
      remaining=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$project") || cleanup_status=failed
      [[ -z "$remaining" ]] || cleanup_status=failed
    done
  done
  rm -rf -- "$work" || cleanup_status=failed
  rm -f -- .env || cleanup_status=failed
  local result=failed
  if [[ "$status" == 0 && "$cleanup_status" == passed ]]; then result=passed; fi
  printf 'source_commit=%s\nprovider=mysql\nstage=%s\nresult=%s\ncleanup=%s\n' \
    "$GITHUB_SHA" "$stage" "$result" "$cleanup_status" | tee "$artifacts/summary.txt"
  [[ "$cleanup_status" == passed ]] || exit 1
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

bash scripts/prod/setup-env.sh
# The production entry sources this file again. Store the selected database and
# public endpoint here, keeping the installation credentials in the shell only.
python3 - <<'PY'
import os
from pathlib import Path
import shlex

keys = (
    "ADDP_DEPLOYMENT_SCOPE", "ADDP_DEPLOYMENT_MODULES", "ADDP_DEPLOYMENT_PROJECT",
    "ADDP_DEPLOYMENT_NETWORK", "ADDP_INFRA_DATABASE_PROVIDER", "ADDP_MYSQL_HOST",
    "ADDP_MYSQL_PORT", "ADDP_MYSQL_SSLMODE", "ADDP_DATABASE_RUNTIME_USER",
    "ADDP_DATABASE_RUNTIME_PASSWORD", "REDIS_HOST", "REDIS_PORT", "NGINX_BIND_HOST", "NGINX_PORT",
    "ADDP_PUBLIC_ORIGIN", "ALLOWED_ORIGINS", "CONSOLE_URL", "PUBLIC_API_URL",
    "ADDP_OBSERVABILITY_LOGS_ENABLED",
)
if os.environ["ADDP_MYSQL_ONLINE_SUITE"] in {"quality-worker", "all-modules", "full-release"}:
    keys += ("TZ",)
if os.environ["ADDP_MYSQL_ONLINE_SUITE"] in {"all-modules", "full-release"}:
    keys += ("POSTGRES_HOST",)
if os.environ["ADDP_MYSQL_ONLINE_SUITE"] == "full-release":
    keys += ("ADDP_SUPERMAP_WORKFLOW_ENABLED", "INFRA_BIND_HOST")
env_file = Path(".env")
lines = [line for line in env_file.read_text().splitlines() if line.partition("=")[0] not in keys]
lines.extend(f"{key}={shlex.quote(os.environ[key])}" for key in keys)
env_file.write_text("\n".join(lines) + "\n")
PY
source scripts/infra/platform-database.sh
schemas=(system common)
collect_database_schema() { schemas+=("$1"); }
application_services=$("${app[@]}" config --services)
addp_prepare_selected_databases collect_database_schema "$application_services"

for key in REDIS_PASSWORD MINIO_ROOT_USER MINIO_ROOT_PASSWORD IAM_MFA_ENCRYPTION_KEY MEILISEARCH_MASTER_KEY \
  MANAGER_SEARCH_REDIS_PASSWORD INFRA_FALKORDB_PASSWORD; do
  value=$(source .env && printf '%s' "${!key}")
  [[ -n "$value" ]]
  export "$key=$value"
done
unset value
export MYSQL_FIXTURE_IMAGE MEILISEARCH_FIXTURE_IMAGE POSTGRES_FIXTURE_IMAGE SEARCH_SOURCE_ADMIN_PASSWORD
MYSQL_FIXTURE_IMAGE=$(docker compose -f scripts/test/docker-compose.local-macos-ci.yml config --images mysql)
if [[ "$ADDP_DEPLOYMENT_MODULES" == manager,security || "$ADDP_DEPLOYMENT_MODULES" == quality,monitor ]]; then
  POSTGRES_FIXTURE_IMAGE=$(ADDP_INFRA_GATE_PASSWORD=fixture ADDP_INFRA_GATE_MANAGER_PASSWORD=fixture \
    docker compose --env-file /dev/null -f scripts/test/docker-compose.system-infra-configuration-t2.yml config --images postgres)
fi
if [[ "$ADDP_MYSQL_ONLINE_SUITE" == manager || "$ADDP_MYSQL_ONLINE_SUITE" == all-modules ]]; then
  MEILISEARCH_FIXTURE_IMAGE=$(docker compose --env-file .env -f docker-compose.infra.yml config --images meilisearch)
fi
if [[ "$ADDP_MYSQL_ONLINE_SUITE" == manager ]]; then
  SEARCH_SOURCE_ADMIN_PASSWORD=$(openssl rand -hex 24)
  export ADDP_TEST_MYSQL_SEARCH_OPERATOR_PASSWORD SEARCH_SOURCE_READER_PASSWORD
  ADDP_TEST_MYSQL_SEARCH_OPERATOR_PASSWORD=$(openssl rand -hex 24)
  SEARCH_SOURCE_READER_PASSWORD=$(openssl rand -hex 24)
  fixture_compose+=(--profile manager)
fi
if [[ "$ADDP_MYSQL_ONLINE_SUITE" == all-modules ]]; then
  for key in INFRA_KAFKA_ADMIN_PASSWORD INFRA_KAFKA_CONNECT_PASSWORD INFRA_KAFKA_TRANSFER_PASSWORD; do
    value=$(source .env && printf '%s' "${!key}")
    [[ -n "$value" ]]
    export "$key=$value"
  done
  unset value
  fixture_compose+=(--profile all-modules)
fi
if [[ "$ADDP_DEPLOYMENT_MODULES" == quality,monitor ]]; then
  export QUALITY_SOURCE_ADMIN_PASSWORD ADDP_TEST_QUALITY_SOURCE_PASSWORD
  QUALITY_SOURCE_ADMIN_PASSWORD=$(openssl rand -hex 24)
  ADDP_TEST_QUALITY_SOURCE_PASSWORD=$(openssl rand -hex 24)
  fixture_compose+=(--profile quality-worker)
fi
cat > "$work/compose.yml" <<'YAML'
name: ${ADDP_MYSQL_FIXTURE_PROJECT:?}
services:
  mysql:
    image: ${MYSQL_FIXTURE_IMAGE:?}
    environment:
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD:?}
      MYSQL_ROOT_HOST: "%"
    # Only this disposable instance permits installer-created triggers under binlog.
    command: [--log-bin-trust-function-creators=ON, --character-set-server=utf8mb4, --collation-server=utf8mb4_unicode_ci]
    ports: ["127.0.0.1::3306"]
    volumes: [database:/var/lib/mysql]
    healthcheck:
      test: ["CMD-SHELL", "MYSQL_PWD=$${MYSQL_ROOT_PASSWORD} mysql --protocol=tcp -h127.0.0.1 -uroot -e 'SELECT 1'"]
      interval: 3s
      timeout: 4s
      retries: 40
  redis:
    image: redis:7-alpine
    environment:
      REDIS_PASSWORD: ${REDIS_PASSWORD:?}
    command: ["sh", "/addp/redis-authentication.sh", "infra"]
    volumes:
      - ${GITHUB_WORKSPACE:?}/scripts/infra/redis-authentication.sh:/addp/redis-authentication.sh:ro
  minio:
    build:
      context: ${GITHUB_WORKSPACE:?}/scripts/infra
      dockerfile: Dockerfile.minio
    command: [server, /data]
    environment:
      MINIO_ROOT_USER: ${MINIO_ROOT_USER:?}
      MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD:?}
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9000/minio/health/live"]
      interval: 3s
      timeout: 4s
      retries: 30
  meilisearch:
    profiles: [manager, all-modules]
    image: ${MEILISEARCH_FIXTURE_IMAGE:-getmeili/meilisearch}
    environment:
      MEILI_ENV: production
      MEILI_MASTER_KEY: ${MEILISEARCH_MASTER_KEY:-}
      MEILI_NO_ANALYTICS: "true"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:7700/health"]
      interval: 3s
      timeout: 4s
      retries: 30
  manager-search-redis:
    profiles: [manager, all-modules]
    extends:
      file: ${GITHUB_WORKSPACE:?}/scripts/infra/manager-search-redis.yml
      service: manager-search-redis
  falkordb:
    profiles: [all-modules]
    extends:
      file: ${GITHUB_WORKSPACE:?}/scripts/infra/falkordb.yml
      service: falkordb
    volumes: [graph-data:/data]
  redpanda:
    profiles: [all-modules]
    extends:
      file: ${GITHUB_WORKSPACE:?}/docker-compose.infra.yml
      service: redpanda
    container_name: !reset null
    ports: !reset []
    volumes: !override [redpanda-data:/var/lib/redpanda/data]
    networks: !override [default]
  redpanda-init:
    profiles: [all-modules-init]
    extends:
      file: ${GITHUB_WORKSPACE:?}/docker-compose.infra.yml
      service: redpanda-init
    container_name: !reset null
    networks: !override [default]
  search-source:
    profiles: [manager]
    image: ${POSTGRES_FIXTURE_IMAGE:-postgres:15}
    environment:
      POSTGRES_DB: search_fixture
      POSTGRES_PASSWORD: ${SEARCH_SOURCE_ADMIN_PASSWORD:-}
    healthcheck:
      test: ["CMD", "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "search_fixture"]
      interval: 3s
      timeout: 4s
      retries: 30
  quality-source:
    profiles: [quality-worker]
    image: ${POSTGRES_FIXTURE_IMAGE:-postgres:15}
    environment:
      POSTGRES_DB: quality_fixture
      POSTGRES_PASSWORD: ${QUALITY_SOURCE_ADMIN_PASSWORD:-}
    volumes: [quality-source-data:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD", "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "quality_fixture"]
      interval: 3s
      timeout: 4s
      retries: 30
  geopython-workflow-engine:
    profiles: [develop-file]
    image: localhost:5001/addp-geopython-workflow-engine:${GEOPYTHON_FIXTURE_TAG:-unused}
    environment:
      RUNTIME_HOST: geopython-workflow-engine
      PORT: "8099"
      SYSTEM_URL: http://system-backend:8180
      GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET: ${GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET:-}
    healthcheck:
      test: ["CMD", "python", "-c", "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8099/health', timeout=3)"]
      interval: 5s
      timeout: 4s
      retries: 36
  registry:
    image: registry:2
    ports: ["127.0.0.1:5001:5000"]
volumes:
  database:
  graph-data:
  redpanda-data:
  quality-source-data:
networks:
  default:
    name: ${ADDP_MYSQL_FIXTURE_NETWORK:?}
YAML

if [[ "$ADDP_MYSQL_ONLINE_SUITE" == full-release ]]; then
  for project in addp-platform addp-infra addp-runtimes; do
    existing=$(docker ps -aq --filter "label=com.docker.compose.project=$project")
    [[ -z "$existing" ]] || { echo "Hosted Runner already contains project $project" >&2; exit 1; }
    for kind in network volume; do
      existing=$(docker "$kind" ls -q --filter "label=com.docker.compose.project=$project")
      [[ -z "$existing" ]] || { echo "Hosted Runner already contains $kind resources for $project" >&2; exit 1; }
    done
  done
  if docker network inspect "$ADDP_DEPLOYMENT_NETWORK" >/dev/null 2>&1; then
    echo "Hosted Runner already contains network $ADDP_DEPLOYMENT_NETWORK" >&2
    exit 1
  fi
  full_network_id=$(docker network create --driver bridge \
    --label com.docker.compose.project=addp-infra --label com.docker.compose.network=addp-network "$ADDP_DEPLOYMENT_NETWORK")
fi
infra_started=true
if [[ "$ADDP_MYSQL_ONLINE_SUITE" == full-release ]]; then
  fixture up -d --wait --wait-timeout 180 mysql registry
  docker network connect --alias "$ADDP_MYSQL_HOST" "$full_network_id" "$(fixture ps -q mysql)"
else
  fixture up -d --build --wait --wait-timeout 180
fi
if [[ "$ADDP_MYSQL_ONLINE_SUITE" == all-modules ]]; then
  fixture run --rm --no-deps -T redpanda-init
fi
curl --fail --silent --show-error --retry 10 --retry-connrefused --max-time 5 \
  http://127.0.0.1:5001/v2/ >/dev/null
if [[ "$ADDP_MYSQL_ONLINE_SUITE" != full-release ]]; then
  ADDP_INFRA_RESOLVED=1 bash scripts/infra/init-minio.sh --compose-file "$work/compose.yml" </dev/null
fi
admin() {
  fixture exec -T mysql sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql --protocol=tcp -h127.0.0.1 -uroot -NB'
}
runtime() {
  MYSQL_PWD="$ADDP_DATABASE_RUNTIME_PASSWORD" fixture exec -T -e MYSQL_PWD mysql mysql \
    --protocol=tcp -h127.0.0.1 -u"$ADDP_DATABASE_RUNTIME_USER" -NB
}
admin <<SQL
CREATE USER 'platform_deploy'@'%' IDENTIFIED BY '$ADDP_DATABASE_DEPLOY_PASSWORD';
CREATE USER 'platform_runtime'@'%' IDENTIFIED BY '$ADDP_DATABASE_RUNTIME_PASSWORD';
CREATE DATABASE \`system\`;
GRANT ALL ON \`system\`.* TO 'platform_deploy'@'%' WITH GRANT OPTION;
SQL
for schema in "${schemas[@]:1}"; do
  admin <<SQL
CREATE DATABASE \`$schema\`;
GRANT ALL ON \`$schema\`.* TO 'platform_deploy'@'%';
GRANT SELECT,INSERT,UPDATE,DELETE ON \`$schema\`.* TO 'platform_runtime'@'%';
SQL
done
for schema in "${schemas[@]}"; do
  [[ $(admin <<<"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$schema'") == 0 ]]
done

stage=deploy
release_started=true
# The image builder offers option 2 to use the Runner's existing Docker configuration.
bash scripts/prod/deploy.sh "$GITHUB_SHA" <<<2
unset ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD
inspect_runtime() {
python3 - "$1" "$(fixture ps -q mysql)" <<'PY'
import json
import os
from pathlib import Path
import subprocess
import sys
import time

manifest = json.loads(Path("dist/compose-release/manifest.json").read_text())
assert manifest["source_commit"] == os.environ["GITHUB_SHA"]
assert manifest["selection"]["ADDP_INFRA_DATABASE_PROVIDER"] == "mysql"
assert manifest["selection"]["ADDP_DEPLOYMENT_SCOPE"] == os.environ["ADDP_DEPLOYMENT_SCOPE"]
suite = os.environ["ADDP_MYSQL_ONLINE_SUITE"]
full = suite == "full-release"

def project_containers(project, stopped=False):
    ids = subprocess.check_output(["docker", "ps", "-aq" if stopped else "-q", "--no-trunc", "--filter",
                                  f"label=com.docker.compose.project={project}"], text=True).split()
    containers = json.loads(subprocess.check_output(["docker", "inspect", *ids], text=True))
    services = {item["Config"]["Labels"]["com.docker.compose.service"]: item for item in containers}
    assert len(services) == len(ids), f"duplicate containers for {project}"
    return services

services = project_containers(os.environ["ADDP_DEPLOYMENT_PROJECT"])
assert set(services) == set(manifest["projects"]["platform"])
backends = ["system-backend", "meta-backend", "gateway"]
requested = os.environ["ADDP_DEPLOYMENT_MODULES"]
requested_modules = requested.split(",") if requested else []
manifest_modules = manifest["selection"].get("ADDP_DEPLOYMENT_MODULES")
assert sorted(requested_modules) == (manifest_modules.split(",") if manifest_modules else []), "release modules differ from the Hosted selection"
backends.extend(f"{module}-backend" for module in requested_modules if module not in {"portal", "system"})
if full:
    backends = [name for name in services if name == "gateway" or (name.endswith("-backend") and name != "portal-backend")]
worker_runtimes = {
    "meta-worker": ("meta", "scan"),
    "quality-worker": ("quality", "quality-bounded"),
    "security-worker": ("security", "sensitive_data_discovery"),
    "transfer-bounded-worker": ("transfer", "sync"),
    "transfer-continuous-worker": ("transfer", "continuous_sync"),
}
workers = {service: identity for service, identity in worker_runtimes.items()
           if full or identity[0] == "meta" or identity[0] in requested_modules}
all_modules = full or suite == "all-modules"
for name in (*backends, *workers):
    container = services[name]
    settings = dict(value.split("=", 1) for value in container["Config"]["Env"])
    assert settings["ADDP_INFRA_DATABASE_PROVIDER"] == "mysql", name
    assert settings["ADDP_MYSQL_HOST"] == "mysql", name
    if not full:
        assert (settings["REDIS_HOST"], settings["REDIS_PORT"]) == ("redis", "6379"), name
    assert settings["ADDP_DATABASE_RUNTIME_USER"] == "platform_runtime", name
    assert settings["ADDP_DATABASE_RUNTIME_PASSWORD"] == os.environ["ADDP_DATABASE_RUNTIME_PASSWORD"], name
    assert not any(key.startswith("ADDP_DATABASE_DEPLOY_") for key in settings), name
    assert container["Image"] == manifest["image_ids"][manifest["projects"]["platform"][name]], name
    assert container["State"]["Running"] and container["State"].get("Health", {}).get("Status", "none") != "unhealthy", name
    if name == "manager-backend" or (all_modules and name in backends):
        assert container["State"]["Health"]["Status"] == "healthy", name
    if all_modules or (suite == "quality-worker" and name in {"quality-backend", "quality-worker", "monitor-backend"}):
        assert settings["TZ"] == "UTC", name
    if all_modules:
        assert settings["POSTGRES_HOST"] in ({"unavailable.invalid", "postgres"} if full else {"unavailable.invalid"}), name
        assert not settings.get("POSTGRES_PASSWORD"), name
        assert settings["ADDP_MYSQL_PORT"] == "3306", name
report = {"provider": "mysql", "verified_services": sorted(services)}
if all_modules:
    from scripts.utils.hosted_mysql_platform import mysql_read

    portal = services["portal-backend"]
    assert portal["State"]["Health"]["Status"] == "healthy"
    assert not any(value.startswith(("ADDP_DATABASE_", "ADDP_MYSQL_", "POSTGRES_")) for value in portal["Config"]["Env"])
    network = os.environ["ADDP_DEPLOYMENT_NETWORK"]
    fixture_project = os.environ["ADDP_MYSQL_FIXTURE_PROJECT"]
    attachments, = json.loads(subprocess.check_output(["docker", "network", "inspect", network], text=True))
    assert attachments["Labels"]["com.docker.compose.project"] == ("addp-infra" if full else fixture_project)
    fixture_services = project_containers(fixture_project)
    expected_fixtures = {"mysql", "registry"}
    if not full:
        expected_fixtures.update({"redis", "minio", "meilisearch", "manager-search-redis", "falkordb", "redpanda"})
    assert set(fixture_services) == expected_fixtures
    assert fixture_services["mysql"]["Id"] == sys.argv[2]
    if full:
        infra = project_containers("addp-infra", stopped=True)
        runtimes = project_containers("addp-runtimes", stopped=True)
        for role, group in (("infra", infra), ("runtimes", runtimes)):
            assert set(group) == set(manifest["projects"][role]), f"{role} differs from the fixed release manifest"
            for name, item in group.items():
                assert item["Image"] == manifest["image_ids"][manifest["projects"][role][name]], name
                if role == "infra" and name.endswith("-init"):
                    assert not item["State"]["Running"] and item["State"]["ExitCode"] == 0, name
                elif role == "infra" and name == "runtime-log-pruner":
                    assert item["State"]["Running"], name
                else:
                    assert item["State"]["Running"] and item["State"]["Health"]["Status"] == "healthy", name
                assert not any(value.startswith(("ADDP_DATABASE_DEPLOY_", "POSTGRES_PASSWORD=")) for value in item["Config"]["Env"]), name
        assert "postgres" not in infra, "the full release created an internal PostgreSQL service"
        attached = (*services.values(), fixture_services["mysql"],
                    *(item for name, item in infra.items() if not name.endswith("-init") and name != "runtime-log-pruner"),
                    *(item for name, item in runtimes.items() if name != "jupyter-launcher"))
        report.update(infra_services=sorted(infra), ready_runtime_services=sorted(runtimes),
                      platform_processes={name: item["Id"] for name, item in services.items()},
                      runtime_processes={name: item["Id"] for name, item in runtimes.items()},
                      mysql_container=fixture_services["mysql"]["Id"])
    else:
        attached = (*services.values(), *fixture_services.values())
    assert set(attachments["Containers"]) == {item["Id"] for item in attached}, "platform network has unexpected members"
    worker_instances = {}
    deadline = time.monotonic() + 60
    while True:
        rows = mysql_read(sys.argv[2], """
SELECT d.module_name, JSON_UNQUOTE(JSON_EXTRACT(i.metadata, '$.runtime_name')),
       i.runtime_hostname, i.instance_id
FROM system.module_runtime_instances i JOIN system.module_definitions d ON d.id=i.module_definition_id
WHERE i.role='worker' AND i.status='up' AND i.stopped_at IS NULL AND i.lease_expires_at>UTC_TIMESTAMP(6);
""").splitlines()
        observed = [row.split("\t") for row in rows]
        for service, identity in workers.items():
            hostname = services[service]["Config"]["Hostname"]
            matches = [instance for module, runtime, host, instance in observed if (module, runtime) == identity and host == hostname]
            if len(matches) == 1:
                worker_instances[service] = {"module": identity[0], "runtime": identity[1], "instance_id": matches[0]}
        if set(worker_instances) == set(workers):
            break
        assert time.monotonic() < deadline, f"missing active worker registrations: {sorted(workers.keys() - worker_instances.keys())}"
        worker_instances.clear()
        time.sleep(2)
    report.update(fixture_services=sorted(fixture_services), ready_http_services=sorted((*backends, "portal-backend")),
                  database_runtime_services=sorted((*backends, *workers)), worker_instances=worker_instances)
Path(sys.argv[1]).write_text(json.dumps(report) + "\n")
PY
}

verify_schema_versions() {
  (
    printf 'server_version=%s\n' "$(admin <<<'SELECT VERSION()')"
    for schema in "${schemas[@]}"; do
      versions=$(runtime <<<"SELECT owner, version, pending_version FROM \`$schema\`.startup_schema_revision ORDER BY owner")
      [[ -n "$versions" ]]
      awk -F '\t' 'NF != 3 || $2 < 1 || $3 != 0 { exit 1 }' <<<"$versions"
      printf '[%s]\n%s\n' "$schema" "$versions"
      if runtime <<<"CREATE TABLE \`$schema\`.hosted_ddl_probe (id INTEGER)" 2>"$work/ddl.err"; then
        printf 'Runtime unexpectedly created a table in %s\n' "$schema" >&2
        exit 1
      fi
      grep -q 'ERROR 1142 .*CREATE command denied' "$work/ddl.err"
    done
    printf 'restricted_runtime_ddl=denied\n'
    if [[ "$ADDP_MYSQL_ONLINE_SUITE" == all-modules || "$ADDP_MYSQL_ONLINE_SUITE" == full-release ]]; then
      expected=$(printf '%s\n' "${schemas[@]}" | LC_ALL=C sort)
      actual=$(admin <<'SQL' | LC_ALL=C sort
SELECT SCHEMA_NAME FROM information_schema.SCHEMATA
WHERE SCHEMA_NAME NOT IN ('information_schema','mysql','performance_schema','sys');
SQL
      )
      [[ "$actual" == "$expected" ]] || { echo 'MySQL schemas differ from the registered owners' >&2; exit 1; }
      printf 'platform_schemas=%s\n' "${#schemas[@]}"
    fi
  ) | tee "$1"
}

stage=runtime
if [[ "$ADDP_MYSQL_ONLINE_SUITE" == full-release ]]; then
  bash scripts/prod/health-check.sh | tee "$artifacts/initial-health.txt"
fi
inspect_runtime "$artifacts/runtime.json"
verify_schema_versions "$artifacts/mysql-versions.txt"
if [[ "$ADDP_MYSQL_ONLINE_SUITE" == all-modules ]]; then
  stage=complete
  exit 0
fi
if [[ "$ADDP_MYSQL_ONLINE_SUITE" == full-release ]]; then
  schema_digests() {
    local schema digest
    for schema in "${schemas[@]}"; do
      digest=$(fixture exec -T mysql sh -ec '
        export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"
        exec mysqldump -uroot --no-data --routines --events --skip-comments --skip-dump-date \
          --skip-lock-tables --no-tablespaces --set-gtid-purged=OFF --databases "$1"
      ' dump "$schema" | sed -E 's/ AUTO_INCREMENT=[0-9]+//g' | sha256sum)
      printf '%s\t%s\n' "$schema" "${digest%% *}"
    done
  }
  schema_digests > "$artifacts/mysql-schema-sha256.tsv"
  stage=restart-stop
  bash scripts/prod/stop.sh
  for project in addp-platform addp-runtimes; do
    [[ -z $(docker ps -aq --filter "label=com.docker.compose.project=$project") ]]
  done
  stage=restart-start
  bash scripts/prod/start.sh
  stage=restart-verification
  bash scripts/prod/health-check.sh | tee "$artifacts/restarted-health.txt"
  inspect_runtime "$artifacts/restarted-runtime.json"
  verify_schema_versions "$artifacts/restarted-mysql-versions.txt"
  schema_digests > "$artifacts/restarted-mysql-schema-sha256.tsv"
  cmp "$artifacts/mysql-versions.txt" "$artifacts/restarted-mysql-versions.txt"
  cmp "$artifacts/mysql-schema-sha256.tsv" "$artifacts/restarted-mysql-schema-sha256.tsv"
  python3 - "$artifacts" <<'PY'
import json
from pathlib import Path
import sys

artifacts = Path(sys.argv[1])
installed = json.loads((artifacts / "runtime.json").read_text())
restarted = json.loads((artifacts / "restarted-runtime.json").read_text())
assert installed["mysql_container"] == restarted["mysql_container"], "restart changed the external MySQL instance"
for group in ("platform_processes", "runtime_processes"):
    assert installed[group].keys() == restarted[group].keys(), group
    assert set(installed[group].values()).isdisjoint(restarted[group].values()), group
for service, worker in installed["worker_instances"].items():
    assert worker["instance_id"] != restarted["worker_instances"][service]["instance_id"], service
(artifacts / "restart.json").write_text(json.dumps({"external_mysql_unchanged": True,
    "platform_and_runtime_recreated": True, "worker_registrations_recreated": True,
    "owner_versions_unchanged": True, "owner_schema_digests_unchanged": True}) + "\n")
PY
  stage=complete
  exit 0
fi

stage=identity
export ADDP_TEST_MYSQL_MEMBER_PASSWORD ADDP_TEST_MYSQL_PORT
ADDP_TEST_MYSQL_MEMBER_PASSWORD=$(openssl rand -hex 24)
mapping=$(fixture port mysql 3306)
[[ "$mapping" =~ ^127\.0\.0\.1:([0-9]+)$ ]]
ADDP_TEST_MYSQL_PORT="${BASH_REMATCH[1]}"
if [[ "$ADDP_DEPLOYMENT_MODULES" == service ]]; then
  export ADDP_TEST_LINEAGE_PUBLISHER_PASSWORD
  ADDP_TEST_LINEAGE_PUBLISHER_PASSWORD=$(openssl rand -hex 24)
fi
if [[ "$ADDP_DEPLOYMENT_MODULES" == develop,security ]]; then
  export ADDP_TEST_MYSQL_DEVELOP_FILE_FACTS="$work/develop-file-tenant.json"
  export ADDP_TEST_MYSQL_DEVELOP_ACTOR_PASSWORD ADDP_TEST_MYSQL_DEVELOP_APPROVER_PASSWORD ADDP_TEST_MYSQL_DEVELOP_REGISTRAR_PASSWORD
  ADDP_TEST_MYSQL_DEVELOP_ACTOR_PASSWORD=$(openssl rand -hex 24)
  ADDP_TEST_MYSQL_DEVELOP_APPROVER_PASSWORD=$(openssl rand -hex 24)
  ADDP_TEST_MYSQL_DEVELOP_REGISTRAR_PASSWORD=$(openssl rand -hex 24)
fi
if [[ "$ADDP_DEPLOYMENT_MODULES" == quality,monitor ]]; then
  export ADDP_TEST_MYSQL_QUALITY_FACTS="$work/quality-tenant.json"
  export ADDP_TEST_MYSQL_QUALITY_ACTOR_PASSWORD ADDP_TEST_MYSQL_QUALITY_AUTHOR_PASSWORD ADDP_TEST_MYSQL_QUALITY_REGISTRAR_PASSWORD
  ADDP_TEST_MYSQL_QUALITY_ACTOR_PASSWORD=$(openssl rand -hex 24)
  ADDP_TEST_MYSQL_QUALITY_AUTHOR_PASSWORD=$(openssl rand -hex 24)
  ADDP_TEST_MYSQL_QUALITY_REGISTRAR_PASSWORD=$(openssl rand -hex 24)
fi
make build-iam-bootstrap
ln -s "$PWD/dist/release-linux-amd64/addp-iam-bootstrap" "$work/iam-bootstrap"
ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$ADDP_TEST_MYSQL_PORT" \
  python3 system/backend/cmd/iam-bootstrap/testdata/mysql_cli.py "$work"
ADDP_TEST_MYSQL_USER=root ADDP_TEST_MYSQL_PASSWORD="$MYSQL_ROOT_PASSWORD" ADDP_TEST_MYSQL_TENANT_FACTS="$work/tenant.json" \
  GOWORK=off go -C system/backend test ./internal/migration -run '^TestPrepareMySQLGatewayTenant$' -count=1 -v
[[ -s "$work/tenant.json" ]]

if [[ "$ADDP_DEPLOYMENT_MODULES" == manager,security ]]; then
  stage=manager-protection
  tenant_id=$(python3 -c 'import json, sys; print(int(json.load(sys.stdin)["tenant_id"]))' < "$work/tenant.json")
  for ((attempt=0; attempt<45; attempt++)); do
    initialized=$(runtime <<<"SELECT COUNT(*) FROM manager.protection_projection_checkpoints WHERE tenant_id=$tenant_id AND initialized=TRUE")
    [[ "$initialized" != 1 ]] || break
    sleep 2
  done
  [[ "$initialized" == 1 ]] || { echo "Manager protection initialization incomplete for Tenant $tenant_id" >&2; exit 1; }
  printf 'manager_protection_initialized_tenant=%s\n' "$tenant_id" | tee "$artifacts/manager-protection.txt"
  fixture exec -T search-source psql -X -v ON_ERROR_STOP=1 -U postgres -d search_fixture <<SQL
CREATE TABLE hostedcursor_orders (id integer PRIMARY KEY);
CREATE TABLE hostedcursor_shipments (id integer PRIMARY KEY);
INSERT INTO hostedcursor_orders VALUES (1);
INSERT INTO hostedcursor_shipments VALUES (2);
CREATE ROLE search_reader LOGIN PASSWORD '$SEARCH_SOURCE_READER_PASSWORD';
GRANT USAGE ON SCHEMA public TO search_reader;
GRANT SELECT ON hostedcursor_orders, hostedcursor_shipments TO search_reader;
SQL
fi

stage=gateway
python3 - "$work/tenant.json" "$artifacts" <<'PY'
import json
import os
from pathlib import Path
import secrets
import sys
from urllib.error import HTTPError
from urllib.request import ProxyHandler, Request, build_opener

origin = os.environ["ADDP_PUBLIC_ORIGIN"]
facts = json.loads(Path(sys.argv[1]).read_text())
opener = build_opener(ProxyHandler({}))
checks = {}

def call(label, method, path, expected, body=None, token=None, module="system"):
    headers = {"Content-Type": "application/json", "Origin": origin}
    if token:
        headers["Authorization"] = "Bearer " + token
    request = Request(origin + f"/api/v1/{module}" + path, method=method, headers=headers,
                      data=json.dumps(body).encode() if body is not None else None)
    try:
        response = opener.open(request, timeout=10)
    except HTTPError as error:
        response = error
    with response:
        if response.status != expected:
            error_code = response.headers.get("Content-Type", "")
            if error_code.startswith("application/json"):
                value = json.load(response).get("error_code")
                if isinstance(value, str) and value.replace("_", "").isalnum():
                    error_code = value[:80]
            raise AssertionError(f"{label}: expected HTTP {expected}, got {response.status} ({error_code})")
        checks[label] = response.status
        return json.load(response)

def login(label, password):
    result = call(label, "POST", "/login", 200, {"username": "scan-member", "password": password})
    assert result["next_action"] == "session_issued"
    return result["session"]["access_token"]

call("anonymous_profile", "GET", "/users/me", 401)
password = os.environ["ADDP_TEST_MYSQL_MEMBER_PASSWORD"]
token = login("initial_login", password)
identity = call("tenant_identity", "GET", "/auth/context", 200, token=token)
assert identity["principal"]["id"] == str(facts["principal_id"])
assert identity["context"]["tenant_id"] == str(facts["tenant_id"])
assert [role["role_key"] for role in identity["authorization"]["role_assignments"]] == ["tenant.data_steward"]
initial = call("initial_profile", "GET", "/users/me", 200, token=token)
assert initial["local_account"]["username"] == "scan-member"
updated_password = secrets.token_urlsafe(32)
rotation = call("rotate_password", "PUT", "/users/me/password", 200,
                {"current_password": password, "new_password": updated_password}, token)
assert rotation["revoked_family_count"] >= 1 and rotation["changed_at"]
call("old_session_revoked", "GET", "/users/me", 401, token=token)
replacement = login("login_with_new_password", updated_password)
after = call("profile_after_rotation", "GET", "/users/me", 200, token=replacement)
assert after["id"] == initial["id"] and after["local_account"] == initial["local_account"]
Path(sys.argv[2], "gateway.json").write_text(json.dumps(checks))
if os.environ["ADDP_DEPLOYMENT_MODULES"] == "service":
    Path(sys.argv[1]).with_name("lineage-reader.token").write_text(replacement)
if os.environ["ADDP_DEPLOYMENT_MODULES"] == "manager,security":
    permissions = identity["authorization"]["role_assignments"][0]["permissions"]
    assert {"manager.data_item.read", "manager.search.execute"} <= set(permissions)
    engines = call("manager_engines", "GET", "/engines", 200, token=replacement, module="manager")
    assert isinstance(engines["data"], list)
    term = "hosted_manager_mysql"
    result = call("manager_search", "GET", f"/search?q={term}", 200, token=replacement, module="manager")
    assert result["data"]["results"] == [] and result["data"]["exhausted"] is True
    history = call("manager_search_history", "GET", "/search/history", 200, token=replacement, module="manager")
    assert [item["query"] for item in history["data"]["items"]] == [term]
    call("anonymous_manager_engines", "GET", "/engines", 401, module="manager")
    Path(sys.argv[2], "manager.json").write_text(json.dumps({
        "tenant_id": facts["tenant_id"], "principal_id": facts["principal_id"],
        "visible_engines": len(engines["data"]), "search_term": term,
        "search_history_round_trip": True,
        "http_statuses": {label: status for label, status in checks.items() if "manager_" in label},
    }))
    Path(sys.argv[1]).with_name("manager-reader.token").write_text(replacement)
print(json.dumps(checks))
PY
if [[ "$ADDP_DEPLOYMENT_MODULES" == service ]]; then
  stage=service-lineage
  python3 scripts/test/mysql-service-lineage-online.py "$work" "$(fixture ps -q mysql)" "$artifacts"
fi
if [[ "$ADDP_DEPLOYMENT_MODULES" == manager,security ]]; then
  stage=manager-search-cursor
  python3 scripts/test/mysql-manager-search-online.py "$work" "$artifacts"
fi
if [[ "$ADDP_DEPLOYMENT_MODULES" == develop,security ]]; then
  stage=develop-runtime
  export GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET
  GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET=$(source .env && printf '%s' "$GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET")
  [[ "$GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET" =~ ^[0-9a-f]{64}$ ]]
  make build-images IMAGE_BUILD_ARGS="--services geopython-workflow-engine --tag $GEOPYTHON_FIXTURE_TAG --verify --jobs 1" </dev/null
  fixture_compose+=(--profile develop-file)
  fixture up -d --wait --wait-timeout 240 geopython-workflow-engine
  stage=develop-file-publication
  export ADDP_TEST_MYSQL_CONTAINER="$(fixture ps -q mysql)"
  export ADDP_TEST_GEOPYTHON_CONTAINER="$(fixture ps -q geopython-workflow-engine)"
  export ADDP_TEST_DEVELOP_CONTAINER="$("${app[@]}" ps -q develop-backend)"
  develop_file_fixture run "$artifacts"
fi
if [[ "$ADDP_DEPLOYMENT_MODULES" == quality,monitor ]]; then
  stage=quality-source
  fixture exec -T quality-source psql -X -v ON_ERROR_STOP=1 -U postgres -d quality_fixture <<SQL
CREATE TABLE public.quality_probe (id bigint PRIMARY KEY);
INSERT INTO public.quality_probe VALUES (1), (2), (3);
CREATE ROLE quality_source_reader LOGIN PASSWORD '$ADDP_TEST_QUALITY_SOURCE_PASSWORD';
GRANT USAGE ON SCHEMA public TO quality_source_reader;
GRANT SELECT ON public.quality_probe TO quality_source_reader;
SQL
  stage=quality-worker
  python3 -m scripts.test.mysql-quality-worker-online "$artifacts" "$(fixture ps -q mysql)" \
    "$(fixture ps -q quality-source)" "$("${app[@]}" ps -q quality-worker)"
fi
stage=complete
