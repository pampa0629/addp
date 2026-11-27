# Invoke a module's installation commands through its native or Compose launcher.
# System and Common must both be ready before System serves other modules.
addp_prepare_module_database() {
  local module=$1 launch=$2
  case "$module" in
    system)
      "$launch" system go prepare-database system || return $?
      "$launch" system go prepare-database common ;;
    meta|gateway|inference|workbench|manager|security|orchestrator|monitor|standard|model|service|asset|catalog|graph|ontology|develop|quality|transfer) "$launch" "$module" go prepare-database ;;
    agent|copilot) "$launch" "$module" python -m prepare_database ;;
    *) echo "Unknown platform database module: $module" >&2; return 1 ;;
  esac
}

# Prepare only services selected in the verified release after System/Common.
addp_prepare_selected_databases() {
  local launch=$1 database_services=$2 module service
  for module in meta gateway inference workbench manager security orchestrator monitor standard model service asset catalog graph ontology develop quality transfer agent copilot; do
    service="$module-backend"
    if [[ "$module" == gateway ]]; then service=gateway; fi
    if grep -Fxq "$service" <<< "$database_services"; then
      addp_prepare_module_database "$module" "$launch" || return $?
    fi
  done
}

# A stopped System container still records the provider of its deployment.
addp_select_release_database() {
  local scope="${ADDP_DEPLOYMENT_SCOPE:-full}" project network default_provider=""
  case "$scope" in
    full)
      project="${COMPOSE_PROJECT_NAME:-addp-platform}"
      network=addp-network
      default_provider=postgres
      ;;
    system-meta-gateway)
      project="${ADDP_DEPLOYMENT_PROJECT:?}"
      network="${ADDP_DEPLOYMENT_NETWORK:?}"
      ;;
    *) echo "未知的生产发布范围 $scope。" >&2; return 1 ;;
  esac
  local ids id provider selected="" binding
  ids=$(docker ps -aq --filter "label=com.docker.compose.project=$project" \
    --filter 'label=com.docker.compose.service=system-backend' \
    --filter 'label=com.docker.compose.oneoff=False') || return 1
  for id in $ids; do
    binding=$(docker inspect --format '{{range $name, $_ := .NetworkSettings.Networks}}{{println $name}}{{end}}' "$id") || return 1
    grep -Fxq -- "$network" <<< "$binding" || {
      echo "已有 System 容器的部署网络与配置不符。" >&2; return 1
    }
    provider=$(docker inspect --format '{{range .Config.Env}}{{if eq (index (split . "=") 0) "ADDP_INFRA_DATABASE_PROVIDER"}}{{println (index (split . "=") 1)}}{{end}}{{end}}' "$id") || return 1
    provider="${provider:-$default_provider}"
    case "$provider" in postgres|mysql) ;; *) echo "无法识别已有 System 的平台数据库。" >&2; return 1 ;; esac
    if [[ -n "$selected" && "$selected" != "$provider" ]]; then
      echo "部署中 System 容器的平台数据库选型不一致。" >&2; return 1
    fi
    selected=$provider
  done
  if [[ -n "$selected" && -n "${ADDP_INFRA_DATABASE_PROVIDER:-}" && "$selected" != "$ADDP_INFRA_DATABASE_PROVIDER" ]]; then
    echo "已有部署使用 ${selected}，与指定的平台数据库 ${ADDP_INFRA_DATABASE_PROVIDER} 冲突。" >&2; return 1
  fi
  export ADDP_INFRA_DATABASE_PROVIDER="${selected:-${ADDP_INFRA_DATABASE_PROVIDER:-$default_provider}}"
  case "$ADDP_INFRA_DATABASE_PROVIDER" in
    postgres|mysql) ;;
    *) echo "部署须指定平台数据库 postgres 或 mysql。" >&2; return 1 ;;
  esac
  if [[ "$scope" == full && "$ADDP_INFRA_DATABASE_PROVIDER" == mysql ]]; then
    if [[ -z "${ADDP_MYSQL_HOST:-}" || ! "${ADDP_MYSQL_PORT:-}" =~ ^[1-9][0-9]{0,4}$ ]] ||
      (( ADDP_MYSQL_PORT > 65535 )); then
      echo "全量 MySQL 发布需要外置实例的 ADDP_MYSQL_HOST 和有效 ADDP_MYSQL_PORT。" >&2
      return 1
    fi
  fi
  echo "平台数据库 · $project · $ADDP_INFRA_DATABASE_PROVIDER"
}

# Check the managed project before starting Infra. A full MySQL release
# manages the other Infra services and connects to its pre-provisioned database.
addp_select_managed_database() {
  local inventory service selected=""
  inventory=$(docker ps -a --filter label=com.docker.compose.project=addp-infra \
    --filter "label=com.docker.compose.project.working_dir=$1" \
    --format '{{.Label "com.docker.compose.service"}}') || return 1
  while IFS= read -r service; do
    case "$service" in postgres|mysql)
      if [[ -n "$selected" ]]; then echo "本工作区有多个平台数据库容器，无法确定升级目标。" >&2; return 1; fi
      selected=$service ;;
    esac
  done <<< "$inventory"
  if [[ -n "$selected" && -n "${ADDP_INFRA_DATABASE_PROVIDER:-}" && "$selected" != "$ADDP_INFRA_DATABASE_PROVIDER" ]]; then
    echo "已有 Infra 数据库 $selected 与配置 $ADDP_INFRA_DATABASE_PROVIDER 不一致。" >&2; return 1
  fi
  export ADDP_INFRA_DATABASE_PROVIDER="${selected:-${ADDP_INFRA_DATABASE_PROVIDER:-postgres}}"
  if [[ "$ADDP_INFRA_DATABASE_PROVIDER" == mysql && -n "${ADDP_RELEASE_MANIFEST:-}" && "${ADDP_DEPLOYMENT_SCOPE:-full}" == full ]]; then
    return 0
  fi
  if [[ "$ADDP_INFRA_DATABASE_PROVIDER" != postgres ]]; then
    echo "开发 Infra 的服务清单只定义了 PostgreSQL；$ADDP_INFRA_DATABASE_PROVIDER 部署请使用已适配的发布入口。" >&2
    return 1
  fi
}
