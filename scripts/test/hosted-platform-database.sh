# Database preparation for disposable test projects. Hosted sources these functions
# after admission. Private local gates execute the Compose entry at the end.
hosted_start_platform_infra() {
  [ "${ADDP_ONLINE_HOSTED:-}" = 1 ] && [ "$POSTGRES_DB" = addp_online ] && [ "$infra_owned" = 1 ] || return 1
  unset ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD hosted_deploy_password
  POSTGRES_PASSWORD=$(openssl rand -hex 32)
  export POSTGRES_PASSWORD
  run_logged env "$@" bash scripts/infra/up.sh
  source "$ROOT_DIR/scripts/infra/ports.sh"
  addp_infra_verify_container postgres addp-postgres || return 1
  hosted_deploy_password=$(openssl rand -hex 32)
  ADDP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 32)
  export ADDP_DATABASE_RUNTIME_USER=platform_runtime ADDP_DATABASE_RUNTIME_PASSWORD
  fixture_create_database_roles hosted_database_psql
}

# Both profiles feed SQL through stdin to the PostgreSQL container they own.
fixture_create_database_roles() {
  "$1" <<SQL
BEGIN;
CREATE ROLE platform_deploy LOGIN PASSWORD '$hosted_deploy_password';
CREATE ROLE platform_runtime LOGIN PASSWORD '$ADDP_DATABASE_RUNTIME_PASSWORD';
GRANT CREATE ON DATABASE :"db" TO platform_deploy;
COMMIT;
SQL
}

hosted_database_psql() {
  local pg_exec=(docker exec -i addp-postgres)
  fixture_database_psql addp_online "$@"
}

fixture_database_psql() {
  local expected=$1
  shift
  "${pg_exec[@]}" env "EXPECTED_DATABASE=$expected" sh -ec '
    test "$POSTGRES_DB" = "$EXPECTED_DATABASE"
    export PGPASSWORD="$POSTGRES_PASSWORD"
    exec psql -Xq -v ON_ERROR_STOP=1 -v db="$POSTGRES_DB" -v ns="${1:-}" -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"
  ' _ "$@"
}

# The registry supplies the installer command and the actual schema name.
fixture_prepare_namespace() {
  local namespace=$1
  if [[ "$1" == system ]]; then namespace=$4; fi
  "$database_psql" "$namespace" <<'SQL'
BEGIN;
CREATE SCHEMA IF NOT EXISTS :"ns" AUTHORIZATION platform_deploy;
ALTER SCHEMA :"ns" OWNER TO platform_deploy;
GRANT USAGE ON SCHEMA :"ns" TO platform_runtime;
SELECT has_schema_privilege('platform_runtime', :'ns', 'USAGE')
   AND NOT has_schema_privilege('platform_runtime', :'ns', 'CREATE')
   AND NOT has_database_privilege('platform_runtime', current_database(), 'CREATE') AS restricted \gset
\if :restricted
\else
  DO $$ BEGIN RAISE EXCEPTION 'Runtime has incorrect schema privileges'; END $$;
\endif
ALTER DEFAULT PRIVILEGES FOR ROLE platform_deploy IN SCHEMA :"ns"
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO platform_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE platform_deploy IN SCHEMA :"ns"
  GRANT USAGE, SELECT ON SEQUENCES TO platform_runtime;
COMMIT;
SQL
}

hosted_install_database() (
  local owner=$1 language=$2 namespace=$1
  shift 2
  if [[ "$owner" == system ]]; then namespace=$2; fi
  echo "Preparing Hosted platform database: $namespace"
  local command
  if [[ -n "${hosted_compose:-}" ]]; then
    local service="$owner-backend" entry=/app/server
    if [[ "$owner" == gateway ]]; then service=gateway; fi
    if [[ "$language" == python ]]; then entry=python; fi
    command=("$hosted_compose" run "${installer_options[@]}" -T --no-deps --entrypoint "$entry"
      -e ADDP_DATABASE_DEPLOY_USER -e ADDP_DATABASE_DEPLOY_PASSWORD "$service")
  elif [[ "$language" == python ]]; then
    source "$ROOT_DIR/scripts/dev/lifecycle-lock.sh"
    addp_prepare_python_backend "$ROOT_DIR" "$owner" || exit 1
    cd "$ROOT_DIR/$owner/backend" || exit 1
    command=(./venv/bin/python)
  else
    local dir="$owner/backend" entry=./cmd/server binary=".dev-bins/addp-$owner"
    if [[ "$owner" == gateway ]]; then dir=gateway; entry=./cmd/gateway; fi
    # The registry invokes System then Common on the same System binary.
    if [[ "$namespace" != common ]] && ! addp_go_build_is_current "$dir" "$binary" "$entry"; then
      addp_atomic_go_build "$owner" "$dir" "$binary" "$entry" || exit 1
    fi
    command=("$ROOT_DIR/$binary")
  fi
  ADDP_DATABASE_DEPLOY_USER=platform_deploy ADDP_DATABASE_DEPLOY_PASSWORD="${hosted_deploy_password:?}" \
    "${command[@]}" "$@"
)

# System migrations inspect Common even before its own installer has run.
hosted_prepare_registered_databases() {
  local owner action database_psql=hosted_database_psql
  source "$ROOT_DIR/scripts/infra/platform-database.sh"
  for action in fixture_prepare_namespace hosted_install_database; do
    for owner in "$@"; do
      addp_prepare_module_database "$owner" "$action" || return 1
    done
  done
}

hosted_prepare_compose_database() (
  local hosted_compose=$1 database_psql=hosted_database_psql installer_options=(--rm)
  fixture_prepare_compose_services "${2//,/$'\n'}"
)

fixture_prepare_compose_services() {
  local services=$1 action
  source "$ROOT_DIR/scripts/infra/platform-database.sh"
  for action in fixture_prepare_namespace hosted_install_database; do
    if grep -Fxq system-backend <<< "$services"; then
      addp_prepare_module_database system "$action" || return 1
    fi
    addp_prepare_selected_databases "$action" "$services" || return 1
  done
}

# The caller has rendered this run's private, labelled compose.json. Retain
# one-off exit states for the local sampler and the project's normal cleanup.
local_prepare_compose_database() (
  set -euo pipefail
  local project=$1 file=$2 label=$3
  shift 3
  local ROOT_DIR=$PWD database_psql=local_database_psql hosted_compose=local_database_compose
  local installer_options=(--label "$label.role=oneshot")
  local_database_compose() { docker compose --env-file /dev/null -p "$project" -f "$file" "$@"; }
  local_database_psql() {
    local pg_exec=(local_database_compose exec -T postgres)
    fixture_database_psql "$POSTGRES_DB" "$@"
  }
  local container hosted_deploy_password
  container=$(local_database_compose ps -q postgres)
  [[ -n "$container" && $(docker inspect --format "{{index .Config.Labels \"$label\"}}" "$container") == "$project" ]] || return 1
  [[ "$ADDP_DATABASE_RUNTIME_USER" == platform_runtime && "$ADDP_DATABASE_RUNTIME_PASSWORD" =~ ^[a-f0-9]{64}$ ]] || return 1
  hosted_deploy_password=$(openssl rand -hex 32)
  fixture_create_database_roles "$database_psql"
  fixture_prepare_compose_services "$(printf '%s\n' "$@")"
)

hosted_prepare_native_database() (
  source "$ROOT_DIR/scripts/dev/build-identity.sh"
  source "$ROOT_DIR/scripts/infra/ports.sh"
  export GOPATH="$ROOT_DIR/.gopath" GOMODCACHE="$ROOT_DIR/.gomodcache" GOTOOLCHAIN=local
  POSTGRES_PORT=$(addp_infra_mapped_port addp-postgres 5432)
  export POSTGRES_HOST=127.0.0.1 POSTGRES_PORT
  hosted_prepare_registered_databases "$@"
)

hosted_prepare_native_targets() {
  local target owner owners=(system meta gateway) targets=("$@")
  # Expand ordinary dev/start.sh selectors before querying the product registry.
  for target in "$@"; do
    case "$target" in
      -workbench) targets+=(-service) ;;
      -model|-quality) targets+=(-standard) ;;
      -gateway|-console)
        targets+=(-manager -transfer -orchestrator -develop -service -monitor -copilot
          -standard -model -quality -security -catalog -ontology)
        break ;;
    esac
  done
  source "$ROOT_DIR/scripts/infra/platform-database.sh"
  for target in "${targets[@]}"; do
    owner=${target#-}
    case " ${owners[*]} " in *" $owner "*) continue ;; esac
    if addp_prepare_module_database "$owner" : 2>/dev/null; then owners+=("$owner"); fi
  done
  hosted_prepare_native_database "${owners[@]}"
}

# Use the same targets for preparation and ordinary one-at-a-time startup.
hosted_start_native_modules() {
  local target
  hosted_prepare_native_targets "$@"
  unset hosted_deploy_password
  for target in "$@"; do
    run_daemon_launcher_logged env SKIP_MODTIDY=1 bash scripts/dev/start.sh "$target"
  done
}

# Split-checkout HTTP suites keep their current observers, but the product SHA
# determines which deployment commands actually exist.
hosted_start_selected_platform_infra() {
  if [[ -f "$ROOT_DIR/scripts/infra/platform-database.sh" ]]; then
    hosted_start_platform_infra
  elif [[ -f "$ROOT_DIR/scripts/test/hosted-platform-database.sh" ]]; then
    source "$ROOT_DIR/scripts/test/hosted-platform-database.sh"
    hosted_start_platform_infra
  else
    run_logged bash scripts/infra/up.sh
  fi
}

hosted_prepare_selected_native_database() {
  if [[ -f "$ROOT_DIR/scripts/infra/platform-database.sh" ]]; then
    hosted_prepare_native_targets "$@"
  elif [[ -f "$ROOT_DIR/scripts/test/hosted-platform-database.sh" ]]; then
    # These product revisions shipped the System/Common, Meta and Gateway installer.
    hosted_prepare_native_database
  fi
  unset hosted_deploy_password
}

# Local gates call this entry from their verified product checkout.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  local_prepare_compose_database "$@"
fi
