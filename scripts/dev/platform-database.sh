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
  container=$(docker inspect --format '{{.Id}}' addp-postgres) || return 1
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
  addp_prepare_module_database "$module" launch_dev_installer
)
