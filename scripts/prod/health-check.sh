#!/bin/bash
# 检查 Compose 容器健康状态与唯一公开入口，不依赖内部服务的宿主机端口。
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root_dir"
failed=0
if [[ -z "${ADDP_INFRA_OBJECTSTORE_PROVIDER+x}" && -f .env ]]; then
  ADDP_INFRA_OBJECTSTORE_PROVIDER=$(source .env && printf '%s' "${ADDP_INFRA_OBJECTSTORE_PROVIDER:-minio}")
fi
source scripts/infra/platform-objectstore.sh
addp_select_objectstore
addp_objectstore_compose_files

compose() {
  local compose_file="$1"
  shift
  local images
  case "$compose_file" in
    docker-compose.infra.yml) images=infra ;;
    docker-compose.yml) images=platform ;;
    docker-compose.runtimes.yml) images=runtimes ;;
  esac
  local files=(-f "$compose_file")
  if [[ "$images" == infra ]]; then files=("${COMPOSE_FILES[@]}"); fi
  docker compose "${files[@]}" -f "dist/compose-release/$images-images.yml" "$@"
}

check_service() {
  local compose_file="$1" service="$2" container_id status exit_code
  container_id="$(compose "$compose_file" ps -a -q "$service")"
  if [ -z "$container_id" ]; then
    echo "✗ $service: 未启动"
    failed=1
    return
  fi
  status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container_id")"
  if [ "$compose_file" = docker-compose.infra.yml ] && [ "$service" = redpanda-init ] && [ "$status" = exited ]; then
    exit_code="$(docker inspect --format '{{.State.ExitCode}}' "$container_id")"
    if [ "$exit_code" = 0 ]; then
      echo "✓ $service: 初始化完成"
      return
    fi
  fi
  if [ "$status" = healthy ] || [ "$status" = running ]; then
    echo "✓ $service: $status"
  else
    echo "✗ $service: $status"
    failed=1
  fi
}

for compose_file in docker-compose.infra.yml docker-compose.yml docker-compose.runtimes.yml; do
  echo "=== $compose_file ==="
  services="$(compose "$compose_file" config --services)"
  while IFS= read -r service; do
    check_service "$compose_file" "$service"
  done <<< "$services"
done

published_port="$(compose docker-compose.yml port nginx 80 2>/dev/null | sed 's/.*://')"
if [ -n "$published_port" ] && curl -fsS "http://127.0.0.1:${published_port}/health" > /dev/null 2>&1; then
  echo "✓ Nginx 入口: http://127.0.0.1:${published_port}/health"
else
  echo "✗ Nginx 统一入口不可达"
  failed=1
fi

exit "$failed"
