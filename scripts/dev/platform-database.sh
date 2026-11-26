# Native development uses the database owned by this workspace's Infra Compose project.
source "$ROOT_DIR/scripts/infra/platform-database.sh"

addp_dev_select_database() {
  [[ "${ADDP_ONLINE_HOST:-0}" != 1 ]] || return 0
  local endpoint=${DOCKER_HOST:-}
  case "${POSTGRES_HOST:-localhost}" in
    localhost|127.0.0.1|::1) ;;
    *) echo '外部平台库请使用正式发布入口。' >&2; return 1 ;;
  esac
  if [[ -z "$endpoint" || -n "${DOCKER_CONTEXT:-}" ]]; then
    endpoint=$(docker context inspect --format '{{.Endpoints.docker.Host}}') || return 1
  fi
  case "$endpoint" in
    unix://*) ;;
    *) echo '自动准备平台库要求本机 Docker。' >&2; return 1 ;;
  esac
  addp_select_managed_database "$ROOT_DIR"
}

addp_dev_prepare_database() (
  [[ "${ADDP_ONLINE_HOST:-0}" != 1 ]] || return 0
  local module=$1 container mapping db_name
  container=$(docker ps -q --no-trunc --filter label=com.docker.compose.project=addp-infra \
    --filter label=com.docker.compose.service=postgres \
    --filter "label=com.docker.compose.project.working_dir=$ROOT_DIR") || return 1
  if [[ ! "$container" =~ ^[a-f0-9]{64}$ ]]; then
    echo '无法唯一确定当前工作区的运行中 PostgreSQL 容器。' >&2; return 1
  fi
  addp_infra_verify_container postgres "$container" || return 1
  mapping=$(docker port "$container" 5432/tcp) || return 1
  if ! grep -Fxq "127.0.0.1:${POSTGRES_PORT:?}" <<< "$mapping" &&
     ! grep -Fxq "0.0.0.0:$POSTGRES_PORT" <<< "$mapping"; then
    echo '平台数据库的本机端口与 Infra 容器映射不符。' >&2; return 1
  fi
  db_name=$(docker exec "$container" printenv POSTGRES_DB) || return 1
  if [[ -z "$db_name" || "$db_name" != "${POSTGRES_DB:-addp}" ]]; then
    echo '平台库名与本机 Infra 容器不符。' >&2; return 1
  fi
  export POSTGRES_HOST=127.0.0.1 POSTGRES_DB="$db_name"
  if [[ -z "${ADDP_DATABASE_DEPLOY_USER:-}${ADDP_DATABASE_DEPLOY_PASSWORD:-}" ]]; then
    ADDP_DATABASE_DEPLOY_USER=$(docker exec "$container" printenv POSTGRES_USER) || return 1
    ADDP_DATABASE_DEPLOY_PASSWORD=$(docker exec "$container" printenv POSTGRES_PASSWORD) || return 1
  fi
  if [[ -z "${ADDP_DATABASE_DEPLOY_USER:-}" || -z "${ADDP_DATABASE_DEPLOY_PASSWORD:-}" ]]; then
    echo '平台部署凭据不完整。' >&2; return 1
  fi
  export ADDP_DATABASE_DEPLOY_USER ADDP_DATABASE_DEPLOY_PASSWORD
  launch_dev_installer() {
    local owner=$1 language=$2
    shift 2
    if [[ "$language" == python ]]; then
      (cd "$ROOT_DIR/$owner/backend" && ./venv/bin/python "$@")
    else
      if [[ "$owner" == system && "${2:-}" == system ]]; then
        set -- "$@" --require-complete-history
      fi
      "$ROOT_DIR/.dev-bins/addp-$owner" "$@"
    fi
  }
  echo "准备本机 PostgreSQL · $POSTGRES_DB · $module"
  addp_prepare_module_database "$module" launch_dev_installer || return $?
  # Core schemas already have an explicit identity handoff for existing PG installations.
  case "$module" in
    system|meta|gateway) ;;
    *) addp_dev_grant_database_runtime "$container" "$module" ;;
  esac
)

# Existing schemas and objects keep their owners. The selected module is the
# only namespace eligible for the development runtime grant.
addp_dev_grant_database_runtime() {
  local container=$1 owner=$2
  local psql=(docker exec -i -e PGPASSWORD -e PGOPTIONS "$container" psql
    -XwAtq -h 127.0.0.1 -d "$POSTGRES_DB" -v ON_ERROR_STOP=1)
  local runtime
  runtime=$(PGPASSWORD="${ADDP_DATABASE_RUNTIME_PASSWORD:?}" "${psql[@]}" \
    -U "${ADDP_DATABASE_RUNTIME_USER:?}" -c 'SELECT current_user') || return 1
  [[ "$runtime" == "$ADDP_DATABASE_RUNTIME_USER" && "$runtime" != "$ADDP_DATABASE_DEPLOY_USER" ]] || {
    echo '平台部署账号与运行账号须为不同的 PostgreSQL 角色。' >&2; return 1
  }
  PGPASSWORD="$ADDP_DATABASE_DEPLOY_PASSWORD" "${psql[@]}" -U "$ADDP_DATABASE_DEPLOY_USER" \
    -v owner="$owner" -v runtime="$runtime" <<'SQL'
BEGIN;
SET LOCAL search_path = pg_catalog;
SELECT set_config('addp.grant_schema', :'owner', true) AS selected_schema,
       set_config('addp.grant_role', :'runtime', true) AS selected_role \gset
DO $grant$
DECLARE
    target_schema text := current_setting('addp.grant_schema');
    runtime_role text := current_setting('addp.grant_role');
    object record;
    permissions text[];
    permission text;
BEGIN
    IF has_schema_privilege(runtime_role, target_schema, 'CREATE') THEN
        RAISE EXCEPTION 'Platform runtime must not have CREATE on schema %', target_schema;
    END IF;
    IF NOT has_schema_privilege(runtime_role, target_schema, 'USAGE') THEN
        EXECUTE format('GRANT USAGE ON SCHEMA %I TO %I', target_schema, runtime_role);
        IF NOT has_schema_privilege(runtime_role, target_schema, 'USAGE') THEN
            RAISE EXCEPTION 'Deployment role could not grant USAGE on schema %', target_schema;
        END IF;
    END IF;

    FOR object IN
        SELECT c.oid, c.relname, c.relkind
        FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = target_schema AND c.relkind IN ('r','p','S','v','m')
    LOOP
        IF object.relkind = 'S' THEN
            IF NOT has_sequence_privilege(runtime_role, object.oid, 'USAGE')
                OR NOT has_sequence_privilege(runtime_role, object.oid, 'SELECT') THEN
                EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %I.%I TO %I',
                               target_schema, object.relname, runtime_role);
                IF NOT has_sequence_privilege(runtime_role, object.oid, 'USAGE')
                    OR NOT has_sequence_privilege(runtime_role, object.oid, 'SELECT') THEN
                    RAISE EXCEPTION 'Deployment role could not grant access to sequence %.%', target_schema, object.relname;
                END IF;
            END IF;
        ELSE
            permissions := ARRAY['SELECT'];
            IF object.relkind IN ('r','p') AND object.relname <> 'startup_schema_revision' THEN
                permissions := ARRAY['SELECT','INSERT','UPDATE','DELETE'];
            END IF;
            FOREACH permission IN ARRAY permissions LOOP
                IF NOT has_table_privilege(runtime_role, object.oid, permission) THEN
                    EXECUTE format('GRANT %s ON TABLE %I.%I TO %I',
                                   permission, target_schema, object.relname, runtime_role);
                    IF NOT has_table_privilege(runtime_role, object.oid, permission) THEN
                        RAISE EXCEPTION 'Deployment role could not grant % on %.%', permission, target_schema, object.relname;
                    END IF;
                END IF;
            END LOOP;
        END IF;
    END LOOP;
END
$grant$;
COMMIT;
SQL
}
