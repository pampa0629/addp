#!/usr/bin/env bash
# Prepare the selected immutable Compose release, then use the production entry.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
  echo 'Usage: bash scripts/prod/deploy.sh [VERSION] [--registry HOST[:PORT]/PATH] [--skip-deploy]'
  exit 0
fi

version="$(git rev-parse --short HEAD)"
if [[ $# -gt 0 && "$1" != --* ]]; then version=$1; shift; fi
registry="${REGISTRY:-localhost:5001}"
skip_deploy=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --registry) registry="${2:?--registry requires a registry}"; shift 2 ;;
    --skip-deploy) skip_deploy=true; shift ;;
    *) echo "Unknown deployment option: $1" >&2; exit 2 ;;
  esac
done

bash scripts/prod/setup-env.sh
PROJECT_ROOT="$ROOT_DIR"
source scripts/utils/observability-env.sh
addp_prepare_observability_env "$ROOT_DIR/.env"
python3 scripts/prod/release-images.py prepare --tag "$version" --registry "$registry"
if [[ "$skip_deploy" == false ]]; then
  bash scripts/prod/start.sh --prepare-databases
fi
