#!/usr/bin/env bash
# MySQL limited Compose release, on a disposable GitHub runner.
set -euo pipefail
umask 077
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
[[ "${GITHUB_ACTIONS:-}" == true && "${GITHUB_EVENT_NAME:-}" == workflow_dispatch &&
   "${RUNNER_ENVIRONMENT:-}" == github-hosted && "${RUNNER_OS:-}" == Linux && "${RUNNER_ARCH:-}" == X64 ]]
[[ $(git rev-parse HEAD) == "${GITHUB_SHA:?}" && -z $(git status --porcelain) && ! -e .env ]]
[[ "${GITHUB_RUN_ID:?}" =~ ^[0-9]+$ && "${GITHUB_RUN_ATTEMPT:?}" =~ ^[0-9]+$ ]]
docker info >/dev/null

export ADDP_DEPLOYMENT_SCOPE=system-meta-gateway ADDP_DEPLOYMENT_MODULES=
export ADDP_DEPLOYMENT_PROJECT="addp-mysql-platform-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
export ADDP_DEPLOYMENT_NETWORK="$ADDP_DEPLOYMENT_PROJECT" ADDP_INFRA_DATABASE_PROVIDER=mysql
export NGINX_BIND_HOST=127.0.0.1 NGINX_PORT=18080 ADDP_PUBLIC_ORIGIN=http://127.0.0.1:18080
export ALLOWED_ORIGINS="$ADDP_PUBLIC_ORIGIN" CONSOLE_URL="$ADDP_PUBLIC_ORIGIN" PUBLIC_API_URL="$ADDP_PUBLIC_ORIGIN"
export ADDP_MYSQL_HOST=mysql ADDP_MYSQL_PORT=3306 ADDP_MYSQL_SSLMODE=disable
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
fixture_project="$ADDP_DEPLOYMENT_PROJECT-infra"
fixture() { docker compose --env-file /dev/null -p "$fixture_project" -f "$work/compose.yml" "$@"; }
app=(docker compose --profile '*' -f scripts/prod/system-meta-gateway.yml)
infra_started=false
release_started=false
stage=fixture
cleanup() {
  local status=$? cleanup_status=passed project remaining kind
  trap - EXIT INT TERM
  set +e
  if [ "$release_started" = true ]; then
    "${app[@]}" down --volumes --remove-orphans || cleanup_status=failed
  fi
  if [ "$infra_started" = true ]; then
    fixture down --volumes --remove-orphans || cleanup_status=failed
  fi
  for project in "$ADDP_DEPLOYMENT_PROJECT" "$fixture_project"; do
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
# Read generated fixture credentials. Keep the deployment and platform overrides
# in the invoking environment, which Compose prioritizes over the generated file.
for key in REDIS_PASSWORD MINIO_ROOT_USER MINIO_ROOT_PASSWORD IAM_MFA_ENCRYPTION_KEY; do
  value=$(source .env && printf '%s' "${!key}")
  [[ -n "$value" ]]
  export "$key=$value"
done
unset value
export MYSQL_FIXTURE_IMAGE
MYSQL_FIXTURE_IMAGE=$(docker compose -f scripts/test/docker-compose.local-macos-ci.yml config --images mysql)
cat > "$work/compose.yml" <<'YAML'
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
  registry:
    image: registry:2
    ports: ["127.0.0.1:5001:5000"]
volumes:
  database:
networks:
  default:
    name: ${ADDP_DEPLOYMENT_NETWORK:?}
YAML

infra_started=true
fixture up -d --build --wait --wait-timeout 180
curl --fail --silent --show-error --retry 10 --retry-connrefused --max-time 5 \
  http://127.0.0.1:5001/v2/ >/dev/null
ADDP_INFRA_RESOLVED=1 bash scripts/infra/init-minio.sh --compose-file "$work/compose.yml" </dev/null
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
CREATE DATABASE common;
CREATE DATABASE meta;
CREATE DATABASE gateway;
GRANT ALL ON \`system\`.* TO 'platform_deploy'@'%' WITH GRANT OPTION;
GRANT ALL ON common.* TO 'platform_deploy'@'%';
GRANT ALL ON meta.* TO 'platform_deploy'@'%';
GRANT ALL ON gateway.* TO 'platform_deploy'@'%';
GRANT SELECT,INSERT,UPDATE,DELETE ON common.* TO 'platform_runtime'@'%';
GRANT SELECT,INSERT,UPDATE,DELETE ON meta.* TO 'platform_runtime'@'%';
GRANT SELECT,INSERT,UPDATE,DELETE ON gateway.* TO 'platform_runtime'@'%';
SQL
for schema in system common meta gateway; do
  [[ $(admin <<<"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$schema'") == 0 ]]
done

stage=deploy
release_started=true
bash scripts/prod/deploy.sh "$GITHUB_SHA"
unset ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD
stage=runtime
python3 - "$artifacts" <<'PY'
import json
import os
from pathlib import Path
import subprocess
import sys

manifest = json.loads(Path("dist/compose-release/manifest.json").read_text())
assert manifest["source_commit"] == os.environ["GITHUB_SHA"]
assert manifest["selection"]["ADDP_INFRA_DATABASE_PROVIDER"] == "mysql"
assert manifest["selection"]["ADDP_DEPLOYMENT_SCOPE"] == "system-meta-gateway"
ids = subprocess.check_output(
    ["docker", "ps", "-q", "--filter", f"label=com.docker.compose.project={os.environ['ADDP_DEPLOYMENT_PROJECT']}"],
    text=True,
).split()
containers = json.loads(subprocess.check_output(["docker", "inspect", *ids], text=True))
services = {item["Config"]["Labels"]["com.docker.compose.service"]: item for item in containers}
assert set(services) == set(manifest["projects"]["platform"])
for name in ("system-backend", "meta-backend", "meta-worker", "gateway"):
    container = services[name]
    settings = dict(value.split("=", 1) for value in container["Config"]["Env"])
    assert settings["ADDP_INFRA_DATABASE_PROVIDER"] == "mysql", name
    assert settings["ADDP_MYSQL_HOST"] == "mysql", name
    assert settings["ADDP_DATABASE_RUNTIME_USER"] == "platform_runtime", name
    assert settings["ADDP_DATABASE_RUNTIME_PASSWORD"] == os.environ["ADDP_DATABASE_RUNTIME_PASSWORD"], name
    assert not any(key.startswith("ADDP_DATABASE_DEPLOY_") for key in settings), name
    assert container["Image"] == manifest["image_ids"][manifest["projects"]["platform"][name]], name
    assert container["State"]["Running"] and container["State"].get("Health", {}).get("Status", "none") != "unhealthy", name
Path(sys.argv[1], "runtime.json").write_text(json.dumps({"provider": "mysql", "verified_services": sorted(services)}))
PY
printf 'server_version=%s\n' "$(admin <<<'SELECT VERSION()')" | tee "$artifacts/mysql-versions.txt"
for schema in system common meta gateway; do
  versions=$(runtime <<<"SELECT owner, version, pending_version FROM \`$schema\`.startup_schema_revision ORDER BY owner")
  [[ -n "$versions" ]]
  awk -F '\t' 'NF != 3 || $2 < 1 || $3 != 0 { exit 1 }' <<<"$versions"
  printf '[%s]\n%s\n' "$schema" "$versions" | tee -a "$artifacts/mysql-versions.txt"
  if runtime <<<"CREATE TABLE \`$schema\`.hosted_ddl_probe (id INTEGER)" 2>"$work/ddl.err"; then
    printf 'Runtime unexpectedly created a table in %s\n' "$schema" >&2
    exit 1
  fi
  grep -q 'ERROR 1142 .*CREATE command denied' "$work/ddl.err"
done
printf 'restricted_runtime_ddl=denied\n' | tee -a "$artifacts/mysql-versions.txt"

stage=identity
export ADDP_TEST_MYSQL_MEMBER_PASSWORD ADDP_TEST_MYSQL_PORT
ADDP_TEST_MYSQL_MEMBER_PASSWORD=$(openssl rand -hex 24)
mapping=$(fixture port mysql 3306)
[[ "$mapping" =~ ^127\.0\.0\.1:([0-9]+)$ ]]
ADDP_TEST_MYSQL_PORT="${BASH_REMATCH[1]}"
make build-iam-bootstrap
ln -s "$PWD/dist/release-linux-amd64/addp-iam-bootstrap" "$work/iam-bootstrap"
ADDP_MYSQL_HOST=127.0.0.1 ADDP_MYSQL_PORT="$ADDP_TEST_MYSQL_PORT" \
  python3 system/backend/cmd/iam-bootstrap/testdata/mysql_cli.py "$work"
ADDP_TEST_MYSQL_USER=root ADDP_TEST_MYSQL_PASSWORD="$MYSQL_ROOT_PASSWORD" ADDP_TEST_MYSQL_TENANT_FACTS="$work/tenant.json" \
  GOWORK=off go -C system/backend test ./internal/migration -run '^TestPrepareMySQLGatewayTenant$' -count=1 -v
[[ -s "$work/tenant.json" ]]

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

def call(label, method, path, expected, body=None, token=None):
    headers = {"Content-Type": "application/json", "Origin": origin}
    if token:
        headers["Authorization"] = "Bearer " + token
    request = Request(origin + "/api/v1/system" + path, method=method, headers=headers,
                      data=json.dumps(body).encode() if body is not None else None)
    try:
        response = opener.open(request, timeout=10)
    except HTTPError as error:
        response = error
    with response:
        assert response.status == expected, f"{label}: expected HTTP {expected}, got {response.status}"
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
print(json.dumps(checks))
PY
stage=complete
