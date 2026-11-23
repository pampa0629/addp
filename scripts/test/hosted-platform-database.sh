# Database preparation for fresh addp_online installations. The Hosted owner has
# admitted an empty addp-infra project and takes responsibility for its cleanup.
hosted_start_platform_infra() {
  [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "$POSTGRES_DB" = addp_online ] && [ "$infra_owned" = 1 ] || return 1
  POSTGRES_PASSWORD=$(openssl rand -hex 32)
  export POSTGRES_PASSWORD
  run_logged bash scripts/infra/up.sh
  hosted_deploy_password=$(openssl rand -hex 32)
  ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 32)
  export ADDP_DATABASE_RUNTIME_USER=platform_runtime ADDP_DATABASE_RUNTIME_PASSWORD
  # The installer owns the namespaces. Only the install
  # identity owns them; tables and sequences inherit the platform runtime grants.
  docker exec -i addp-postgres sh -ec '
    test "$POSTGRES_DB" = addp_online
    export PGPASSWORD="$POSTGRES_PASSWORD"
    exec psql -Xq -v ON_ERROR_STOP=1 -h 127.0.0.1 -U "$POSTGRES_USER" -d addp_online
  ' <<SQL
BEGIN;
CREATE ROLE platform_deploy LOGIN PASSWORD '$hosted_deploy_password';
CREATE ROLE platform_runtime LOGIN PASSWORD '$ADDP_DATABASE_RUNTIME_PASSWORD';
GRANT CREATE ON DATABASE addp_online TO platform_deploy;
CREATE SCHEMA inference AUTHORIZATION platform_deploy;
DO \$roles\$
DECLARE ns text;
BEGIN
  FOREACH ns IN ARRAY ARRAY['system','common','meta','gateway','inference','workbench','agent','copilot'] LOOP
    EXECUTE format('ALTER SCHEMA %I OWNER TO platform_deploy', ns);
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO platform_runtime', ns);
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE platform_deploy IN SCHEMA %I GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO platform_runtime', ns);
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE platform_deploy IN SCHEMA %I GRANT USAGE, SELECT ON SEQUENCES TO platform_runtime', ns);
  END LOOP;
END \$roles\$;
COMMIT;
SQL
}

hosted_prepare_compose_database() (
  export ADDP_DATABASE_DEPLOY_USER=platform_deploy ADDP_DATABASE_DEPLOY_PASSWORD="$hosted_deploy_password"
  local service=$1
  shift
  compose_app run --rm -T --no-deps --entrypoint /app/server \
    -e ADDP_DATABASE_DEPLOY_USER -e ADDP_DATABASE_DEPLOY_PASSWORD "$service" prepare-database "$@"
)

hosted_prepare_native_database() (
  source "$ROOT_DIR/scripts/dev/build-identity.sh"
  source "$ROOT_DIR/scripts/infra/ports.sh"
  export GOPATH="$ROOT_DIR/.gopath" GOMODCACHE="$ROOT_DIR/.gomodcache" GOTOOLCHAIN=local
  POSTGRES_PORT=$(addp_infra_mapped_port addp-postgres 5432)
  export POSTGRES_HOST=127.0.0.1 POSTGRES_PORT
  local owner dir entry binary
  for owner in system meta gateway "$@"; do
    case "$owner" in agent|copilot) continue ;; esac
    dir="$owner/backend"
    entry=./cmd/server
    if [ "$owner" = gateway ]; then dir=gateway; entry=./cmd/gateway; fi
    binary=".dev-bins/addp-$owner"
    if ! addp_go_build_is_current "$dir" "$binary" "$entry"; then
      addp_atomic_go_build "$owner" "$dir" "$binary" "$entry"
    fi
  done
  install_owner() {
    local owner=$1
    shift
    if [[ "$owner" = agent || "$owner" = copilot ]]; then
      source "$ROOT_DIR/scripts/dev/lifecycle-lock.sh"
      addp_prepare_python_backend "$ROOT_DIR" "$owner"
      (cd "$ROOT_DIR/$owner/backend" && ADDP_DATABASE_DEPLOY_USER=platform_deploy \
        ADDP_DATABASE_DEPLOY_PASSWORD="$hosted_deploy_password" ./venv/bin/python -m prepare_database)
      return
    fi
    ADDP_DATABASE_DEPLOY_USER=platform_deploy ADDP_DATABASE_DEPLOY_PASSWORD="$hosted_deploy_password" \
      ".dev-bins/addp-$owner" prepare-database "$@"
  }
  install_owner system system
  install_owner system common
  run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh --exact-process -system
  install_owner meta
  install_owner gateway
  for owner in "$@"; do install_owner "$owner"; done
)
