#!/usr/bin/env bash
# ADDP_T2_OWNER_MANAGED=supermap
# ADDP_T2_INPUT_FILES=scripts/test/engines-supermap-sgm-gate.sh scripts/test/supermap-sgm-gate.py scripts/test/supermap-sgm-probe.py scripts/test/supermap-sgm-gate_test.py engines/supermap-workflow/ engines/model3d-workflow/ common-python/ scripts/infra/Dockerfile.minio
# Licensed disposable lifecycle: Python owns docker create / docker rm and records
# MEDIA_SHA256 (private SDK base image) and LICENSE_SHA256; never archives media.
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
exec python3 "$ROOT_DIR/scripts/test/supermap-sgm-gate.py"
