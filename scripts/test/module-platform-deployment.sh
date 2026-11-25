#!/usr/bin/env bash
# Both provider gates pass the ID of their own fresh database container.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
container=${1:?expected gate-owned container ID}
owner=${2:?expected database owner}
case "$owner" in asset|service|model|standard|monitor|orchestrator) ;; *) exit 2 ;; esac
provider=${ADDP_INFRA_DATABASE_PROVIDER:?}
work=$(mktemp -d)
install_volume=""
cleanup() {
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
if [[ $owner == model || $owner == service ]]; then
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
if [[ ( $owner == standard || $owner == model || $owner == service || $owner == asset ) && $provider == mysql ]]; then
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
snapshot >installed.sql
if [[ "$owner" == monitor ]]; then
  grep -qi 'CREATE TABLE.*platform_alert_incidents' installed.sql
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
