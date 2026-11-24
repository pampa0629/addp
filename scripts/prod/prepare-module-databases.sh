#!/usr/bin/env bash
# Database phase of the root Compose release. Arguments after the phase select
# the same Compose project, profiles and immutable image overrides as startup.
set -euo pipefail
phase=${1:?expected system or selected}
shift
root=$(cd "$(dirname "$0")/../.." && pwd)
source "$root/scripts/infra/platform-database.sh"
compose=(docker compose "$@")
launch_compose_installer() {
  local module=$1 language=$2 service="$1-backend" entry=/app/server
  shift 2
  if [[ "$module" == gateway ]]; then service=gateway; fi
  if [[ "$language" == python ]]; then entry=python; fi
  "${compose[@]}" run --rm -T --no-deps --entrypoint "$entry" \
    -e ADDP_DATABASE_DEPLOY_USER -e ADDP_DATABASE_DEPLOY_PASSWORD "$service" "$@"
}
case "$phase" in
  system) addp_prepare_module_database system launch_compose_installer ;;
  selected)
    services=$("${compose[@]}" config --services)
    addp_prepare_selected_databases launch_compose_installer "$services"
    ;;
  *) echo 'Expected database phase system or selected' >&2; exit 2 ;;
esac
