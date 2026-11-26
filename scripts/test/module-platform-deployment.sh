#!/usr/bin/env bash
# Both provider gates pass the ID of their own fresh database container.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
container=${1:?expected gate-owned container ID}
owner=${2:?expected database owner}
case "$owner" in transfer|quality|develop|ontology|graph|catalog|asset|service|model|standard|monitor|orchestrator) ;; *) exit 2 ;; esac
provider=${ADDP_INFRA_DATABASE_PROVIDER:?}
work=$(mktemp -d)
install_volume=""
search_container=""
falkor_project=""
cleanup() {
  if [[ -n "$falkor_project" ]]; then docker compose -p "$falkor_project" -f "$root/scripts/infra/falkordb.yml" -f "$work/falkor.yml" down -v >/dev/null; fi
  if [[ -n "$search_container" ]]; then docker rm -fv "$search_container" >/dev/null; fi
  if [[ -n "$install_volume" ]]; then docker volume rm "$install_volume" >/dev/null; fi
  rm -rf "$work"
}
trap cleanup EXIT


case "$provider" in
  postgres)
    # This fixture creates roles. Only the dedicated Actions service can enter.
    [[ ${GITHUB_ACTIONS:-} == true && $container == "${PLATFORM_CI_PG_ID:?}" && $container =~ ^[a-f0-9]{64}$ ]]
    # Leave the service's original database empty for the legacy migration suite.
    docker exec "$container" psql -Xw -v ON_ERROR_STOP=1 -U addp_ci -d postgres \
      -c "CREATE DATABASE addp_${owner}_deployment"
    export POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=5432 POSTGRES_DB=addp_${owner}_deployment
    export ADDP_DATABASE_DEPLOY_USER=platform_install ADDP_DATABASE_RUNTIME_USER=platform_run
    ADDP_DATABASE_DEPLOY_PASSWORD=$(openssl rand -hex 24)
    ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 24)
    export ADDP_DATABASE_DEPLOY_PASSWORD ADDP_DATABASE_RUNTIME_PASSWORD
    pg() { docker exec -i "$container" psql -Xw -v ON_ERROR_STOP=1 -U addp_ci -d "$POSTGRES_DB" "$@"; }
    pg <<SQL
CREATE ROLE platform_install LOGIN PASSWORD '$ADDP_DATABASE_DEPLOY_PASSWORD';
CREATE ROLE platform_run LOGIN PASSWORD '$ADDP_DATABASE_RUNTIME_PASSWORD';
GRANT CREATE ON DATABASE addp_${owner}_deployment TO platform_install;
CREATE SCHEMA common AUTHORIZATION platform_install;
CREATE SCHEMA $owner AUTHORIZATION platform_install;
GRANT USAGE ON SCHEMA common,$owner TO platform_run;
ALTER DEFAULT PRIVILEGES FOR ROLE platform_install IN SCHEMA common,$owner
    GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO platform_run;
ALTER DEFAULT PRIVILEGES FOR ROLE platform_install IN SCHEMA common,$owner
    GRANT SELECT,USAGE ON SEQUENCES TO platform_run;
SQL
    snapshot() {
      docker exec "$container" pg_dump -s -n common -n "$owner" -U addp_ci "$POSTGRES_DB" |
        sed -E '/^\\(un)?restrict /d'
    }
    ;;
  mysql)
    [[ $container == addp-common-mysql-t2-* && ${ADDP_MYSQL_HOST:?} == 127.0.0.1 ]]
    snapshot() {
      docker exec "$container" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysqldump -uroot --no-data --skip-dump-date --no-tablespaces --databases common "$1"' dump "$owner" | sed -E 's/ AUTO_INCREMENT=[0-9]+//g'
    }
    ;;
  *) echo 'The gate requires a disposable PG or MySQL database' >&2; exit 2 ;;
esac

(cd "$root/system/backend" && GOWORK=off go build -o "$work/system" ./cmd/server)
(cd "$root/$owner/backend" && GOWORK=off go build -o "$work/$owner" ./cmd/server)
cd "$work"
runtime() { python3 "$root/scripts/test/module-platform-startup.py" "$work/$owner" "$@"; }
# Use the real module executable as the install entrypoint. The fixture
# reuses the database container's image and network, so the gate needs no registry.
install_dir="$work/linux"
mkdir "$install_dir"
arch=$(docker image inspect "$(docker inspect -f '{{.Image}}' "$container")" -f '{{.Architecture}}')
if [[ $owner == model || $owner == service || $owner == graph || $owner == develop || $owner == quality || $owner == transfer ]]; then
  # The registered engine plugins include the native PostgreSQL parser.
  # Link its C runtime statically so the installer also runs in the DB fixture image.
  build=(go build -tags netgo,osusergo -ldflags '-linkmode external -extldflags -static')
  if [[ $(go env GOHOSTOS)/$(go env GOHOSTARCH) == "linux/$arch" ]]; then
    (cd "$root/$owner/backend" && CGO_ENABLED=1 GOWORK=off "${build[@]}" -o "$install_dir/server" ./cmd/server)
  else
    docker run --rm --platform "linux/$arch" -v "$root:/workspace:ro" \
      -v "${ADDP_BUILD_GOMODCACHE:-$(go env GOMODCACHE)}:/go/pkg/mod" \
      -v "${ADDP_BUILD_GOCACHE:-$(go env GOCACHE)}:/root/.cache/go-build" \
      -e CGO_ENABLED=1 -e GOWORK=off -w "/workspace/$owner/backend" golang:1.24 \
      sh -ec '"$@" -o /tmp/server ./cmd/server >&2; cat /tmp/server' build "${build[@]}" >"$install_dir/server"
  fi
else
  (cd "$root/$owner/backend" && CGO_ENABLED=0 GOWORK=off GOOS=linux GOARCH="$arch" go build -o "$install_dir/server" ./cmd/server)
fi
export MODULE_GATE_IMAGE
MODULE_GATE_IMAGE=$(docker inspect -f '{{.Config.Image}}' "$container")
install_volume=$(docker volume create --label "addp.deployment-gate=$owner")
docker run --rm -i --network none -v "$install_volume:/app" --entrypoint sh "$MODULE_GATE_IMAGE" \
    -ec 'cat >/app/server && chmod 755 /app/server' <"$install_dir/server"
export MODULE_GATE_CONTAINER="$container" MODULE_GATE_VOLUME="$install_volume"
cat >compose.yml <<'YAML'
services:
  OWNER-backend:
    image: ${MODULE_GATE_IMAGE:?}
    pull_policy: never
    network_mode: container:${MODULE_GATE_CONTAINER:?}
    volumes:
      - installer:/app:ro
    environment:
      ADDP_INFRA_DATABASE_PROVIDER: ${ADDP_INFRA_DATABASE_PROVIDER:?}
      POSTGRES_HOST: 127.0.0.1
      POSTGRES_PORT: "5432"
      POSTGRES_DB: ${POSTGRES_DB:-addp}
      ADDP_MYSQL_HOST: 127.0.0.1
      ADDP_MYSQL_PORT: "3306"
volumes:
  installer:
    external: true
    name: ${MODULE_GATE_VOLUME:?}
YAML
sed "s/OWNER-backend/${owner}-backend/" compose.yml >install.yml
runtime missing-credentials
snapshot >before-common.sql
runtime missing common
snapshot >refused.sql
cmp before-common.sql refused.sql
"$work/system" prepare-database common
snapshot >before-owner.sql
runtime missing "$owner"
snapshot >refused.sql
cmp before-owner.sql refused.sql
# The same release database phase and the same module command run on both providers.
prepare=(bash "$root/scripts/prod/prepare-module-databases.sh" selected -p "owner-install-$$" -f "$work/install.yml")
if [[ ( $owner == standard || $owner == model || $owner == service || $owner == asset || $owner == catalog || $owner == graph || $owner == ontology || $owner == develop || $owner == quality || $owner == transfer ) && $provider == mysql ]]; then
  status=0
  "${prepare[@]}" >"$work/installer.out" || status=$?
  case "$status" in
    0) ;;
    3)
      sed -n "/^USE \`$owner\`;$/,/^DELIMITER ;$/p" "$work/installer.out" >"$work/triggers.sql"
      grep -q '^CREATE DEFINER=' "$work/triggers.sql"
      docker exec -i "$container" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql -uroot' <"$work/triggers.sql"
      "${prepare[@]}" ;;
    *) cat "$work/installer.out"; exit "$status" ;;
  esac
else
  "${prepare[@]}"
fi
if [[ $owner == monitor && $provider == mysql ]]; then
  # Revision 1 contained all existing Monitor tables except the two lock tables. Revision 2 added the alert identity lock.
  gate_mysql() {
    docker exec -i "$container" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql -uroot -N -B monitor'
  }
  [[ $(gate_mysql <<<"SELECT COUNT(*) FROM configuration_locks WHERE configuration_key='monitor.monitoring-targets'") == 1 ]]
  gate_mysql <<'SQL'
INSERT INTO platform_alert_incidents (version,category,subject_kind,subject_id,`signal`,severity,status,opened_at,last_observed_at)
VALUES (1,'log_pipeline','observer','upgrade-fixture','delivery_probe','warning','resolved',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
DROP TABLE platform_alert_identity_locks, configuration_locks;
UPDATE startup_schema_revision SET version=1 WHERE owner='monitor' AND version=4;
SQL
  [[ $(gate_mysql <<<"SELECT version FROM startup_schema_revision WHERE owner='monitor'") == 1 ]]
  runtime outdated "$owner" 1 4
  "${prepare[@]}"
  # Revision 2 already has alert identity locks. Its independent configuration lock table is new in 3.
  gate_mysql <<'SQL'
DROP TABLE configuration_locks;
UPDATE startup_schema_revision SET version=2 WHERE owner='monitor' AND version=4;
SQL
  runtime outdated "$owner" 2 4
  "${prepare[@]}"
  # Revision 3 has configuration locks and no preinstalled monitoring-target key.
  gate_mysql <<'SQL'
DELETE FROM configuration_locks WHERE configuration_key='monitor.monitoring-targets';
INSERT INTO configuration_locks (configuration_key) VALUES ('monitor.resource-query-policy');
UPDATE startup_schema_revision SET version=3 WHERE owner='monitor' AND version=4;
SQL
  runtime outdated "$owner" 3 4
  "${prepare[@]}"
  [[ $(gate_mysql <<<"SELECT version FROM startup_schema_revision WHERE owner='monitor'") == 4 ]]
  [[ $(gate_mysql <<<"SELECT COUNT(*) FROM configuration_locks WHERE configuration_key IN ('monitor.monitoring-targets','monitor.resource-query-policy')") == 2 ]]
  echo 'MySQL Monitor upgrade 3→4 installed the target lock and preserved existing policy configuration locks'
  [[ $(gate_mysql <<<"SELECT COUNT(*) FROM platform_alert_incidents WHERE subject_id='upgrade-fixture' AND status='resolved'") == 1 ]]
fi
if [[ "$owner" == standard && "$provider" == mysql ]]; then
  snapshot >standard-v2-fresh.sql
  standard_mysql() {
    docker exec -i "$container" sh -ec 'export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"; exec mysql -uroot -N -B standard'
  }
  # Version 2 adds only the lock table. Recreate the version 1 footprint
  # with an existing owner record before running the formal upgrade.
  standard_mysql <<'SQL'
INSERT INTO metric_categories (tenant_id, name, code, created_by)
VALUES (53,'Existing category','upgrade_category',9);
DROP TABLE metric_dependency_locks;
UPDATE startup_schema_revision SET version=1 WHERE owner='standard' AND version=2;
SQL
  existing_category=$(standard_mysql <<<"SELECT id FROM metric_categories WHERE tenant_id=53 AND code='upgrade_category'")
  [[ -n "$existing_category" ]]
  runtime outdated standard 1 2
  "${prepare[@]}"
  [[ $(standard_mysql <<<"SELECT version FROM startup_schema_revision WHERE owner='standard'") == 2 ]]
  [[ $(standard_mysql <<<"SELECT id FROM metric_categories WHERE tenant_id=53 AND code='upgrade_category' AND name='Existing category'") == "$existing_category" ]]
  snapshot >standard-v2-upgraded.sql
  cmp standard-v2-fresh.sql standard-v2-upgraded.sql
  echo 'MySQL Standard upgrade 1→2 preserved the existing category and matched the fresh schema'
fi
snapshot >installed.sql
if [[ "$owner" == monitor ]]; then
  grep -qi 'CREATE TABLE.*platform_alert_incidents' installed.sql
elif [[ $owner == transfer ]]; then
  grep -qi 'CREATE TABLE.*dead_letters' installed.sql
elif [[ $owner == quality ]]; then
  grep -qi 'CREATE TABLE.*rule_revisions' installed.sql
elif [[ $owner == develop ]]; then
  grep -qi 'CREATE TABLE.*dev_tasks' installed.sql
elif [[ $owner == ontology ]]; then
  grep -qi 'CREATE TABLE.*platform_revisions' installed.sql
elif [[ $owner == graph ]]; then
  grep -qi 'CREATE TABLE.*graph_operations' installed.sql
elif [[ $owner == catalog ]]; then
  grep -qi 'CREATE TABLE.*fulfillment_checks' installed.sql
elif [[ $owner == asset ]]; then
  grep -qi 'CREATE TABLE.*asset_components' installed.sql
elif [[ $owner == service ]]; then
  grep -qi 'CREATE TABLE.*query_services' installed.sql
elif [[ $owner == model ]]; then
  grep -qi 'CREATE TABLE.*logical_tables' installed.sql
elif [[ $owner == standard ]]; then
  grep -qi 'CREATE TABLE.*element_revisions' installed.sql
else
  grep -qi 'CREATE TABLE.*orchestrations' installed.sql
fi
if [[ $owner == catalog ]]; then
  image=$(awk '/image: getmeili\/meilisearch:/ {print $2}' "$root/docker-compose.infra.yml")
  export MEILISEARCH_MASTER_KEY=$(openssl rand -hex 24)
  search_container=$(docker run -d --label addp.deployment-gate=catalog -e MEILI_MASTER_KEY="$MEILISEARCH_MASTER_KEY" -p 127.0.0.1::7700 "$image")
  export MEILISEARCH_URL="http://$(docker port "$search_container" 7700/tcp)"
fi
if [[ $owner == ontology ]]; then
  export INFRA_FALKORDB_PASSWORD=$(openssl rand -hex 24)
  cat >"$work/falkor.yml" <<'YAML'
services:
  falkordb:
    ports: ["127.0.0.1::6379"]
YAML
  falkor_project="addp-ontology-deployment-$$"
  falkor=(docker compose -p "$falkor_project" -f "$root/scripts/infra/falkordb.yml" -f "$work/falkor.yml")
  "${falkor[@]}" up -d --wait
  export INFRA_FALKORDB_ADDRESS=$("${falkor[@]}" port falkordb 6379)
fi
for cycle in 1 2; do
  runtime ready
  snapshot >restarted.sql
  cmp installed.sql restarted.sql
  echo "$provider: runtime cycle $cycle exited normally; Common and $owner schema SHA-256 $(sha256sum restarted.sql | cut -d' ' -f1)"
done
if [[ "$owner" == monitor ]]; then
  (cd "$root/monitor/backend" && ADDP_TEST_MONITOR_DEPLOYMENT=1 GOWORK=off go test ./internal/service -run '^TestInstalledActiveAlertUniqueness$' -count=1 -v)
fi
if [[ $owner == standard ]]; then
  (cd "$root/standard/backend" && ADDP_TEST_STANDARD_DEPLOYMENT=1 GOWORK=off go test ./internal/service -run '^TestInstalledElementPublication$' -count=1 -v)
  (cd "$root/standard/backend" && ADDP_TEST_STANDARD_DEPLOYMENT=1 GOWORK=off go test ./internal/repository -run '^TestInstalledMetricDependencyTransactions$' -count=1 -timeout=60s -v)
fi
if [[ $owner == model ]]; then
  (cd "$root/model/backend" && ADDP_TEST_MODEL_DEPLOYMENT=1 GOWORK=off go test ./internal/service -run '^TestInstalledModelEntity$' -count=1 -v)
fi
if [[ $owner == service ]]; then
  (cd "$root/service/backend" && ADDP_TEST_SERVICE_DEPLOYMENT=1 GOWORK=off go test ./internal/repository -run '^TestInstalledServiceCatalog$' -count=1 -v)
fi
if [[ "$owner" == asset ]]; then
  (cd "$root/asset/backend" && ADDP_TEST_ASSET_DEPLOYMENT=1 GOWORK=off go test ./internal/service -run '^TestInstalledAssetCategories$' -count=1 -v)
fi
if [[ "$owner" == catalog ]]; then
  (cd "$root/catalog/backend" && ADDP_TEST_CATALOG_DEPLOYMENT=1 GOWORK=off go test ./internal/repository -run '^TestInstalledCatalogFulfillment$' -count=1 -v)
fi
if [[ "$owner" == graph ]]; then
  (cd "$root/graph/backend" && ADDP_TEST_GRAPH_DEPLOYMENT=1 GOWORK=off go test ./internal/repository -run '^TestInstalledGraphOntology$' -count=1 -v)
fi
if [[ "$owner" == ontology ]]; then
  (cd "$root/ontology/backend" && ADDP_TEST_ONTOLOGY_DEPLOYMENT=1 GOWORK=off go test ./internal/service -run '^TestInstalledOntologyRevision$' -count=1 -v)
fi
if [[ "$owner" == develop ]]; then
  (cd "$root/develop/backend" && ADDP_TEST_DEVELOP_DEPLOYMENT=1 GOWORK=off go test ./internal/repository -run '^TestInstalledDevelopTask$' -count=1 -v)
fi
if [[ "$owner" == quality ]]; then
  (cd "$root/quality/backend" && ADDP_TEST_QUALITY_DEPLOYMENT=1 GOWORK=off go test ./internal/repository -run '^TestInstalledQualityStorage$' -count=1 -v)
fi
if [[ "$owner" == transfer ]]; then
  (cd "$root/transfer/backend" && ADDP_TEST_TRANSFER_DEPLOYMENT=1 GOWORK=off go test ./internal/repository ./internal/service -run '^TestInstalledTransfer(Storage|Approval)$' -count=1 -v)
fi
