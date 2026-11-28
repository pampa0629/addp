#!/usr/bin/env bash
# Resolve the platform S3 service for this installation after loading its environment.
addp_select_objectstore() {
  objectstore_service=${ADDP_INFRA_OBJECTSTORE_PROVIDER:-minio}
  case "$objectstore_service" in
    minio) objectstore_health_path=/minio/health/live; objectstore_client=mc ;;
    rustfs) objectstore_health_path=/health; objectstore_client=rc ;;
    *) echo "Unsupported platform object store provider: $objectstore_service" >&2; return 1 ;;
  esac
}

addp_objectstore_compose_files() {
  COMPOSE_FILES=(-f docker-compose.infra.yml)
  if [[ "$objectstore_service" == rustfs ]]; then
    COMPOSE_FILES+=(-f scripts/infra/rustfs-platform.yml)
  fi
}
