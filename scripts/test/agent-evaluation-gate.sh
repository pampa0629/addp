#!/bin/bash
# agent-evaluation-gate.sh - Run the ADDP Agent evaluation gate.
#
# Usage: bash scripts/test/agent-evaluation-gate.sh [offline|release|compare|compare-release]
#
# release requires:
#   ADDP_AGENT_READ_ONLY_EVIDENCE
#   ADDP_AGENT_APPROVAL_EVIDENCE
#   ADDP_AGENT_REJECTION_EVIDENCE
#
# compare requires:
#   ADDP_AGENT_EVAL_BASELINE
#   ADDP_AGENT_EVAL_CURRENT

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
PYTHON="$ROOT_DIR/agent/backend/venv/bin/python"
COMMON_PYTHON="$ROOT_DIR/common-python/.venv/bin/python"
MODE=${1:-offline}
REPORT=${ADDP_AGENT_EVAL_REPORT:-/tmp/addp-agent-evaluation-gate-${MODE}.json}

if [ ! -x "$PYTHON" ]; then
    echo "Agent Python runtime not found: $PYTHON" >&2
    exit 1
fi
require_common_python() {
    if [ ! -x "$COMMON_PYTHON" ]; then
        echo "Common-Python test runtime not found: $COMMON_PYTHON" >&2
        echo "Run: cd common-python && uv sync --extra dev" >&2
        exit 1
    fi
}

case "$MODE" in
    offline)
        require_common_python
        exec "$PYTHON" "$ROOT_DIR/evals/agent-scenarios/gate.py" --output "$REPORT"
        ;;
    release)
        require_common_python
        : "${ADDP_AGENT_READ_ONLY_EVIDENCE:?ADDP_AGENT_READ_ONLY_EVIDENCE is required}"
        : "${ADDP_AGENT_APPROVAL_EVIDENCE:?ADDP_AGENT_APPROVAL_EVIDENCE is required}"
        : "${ADDP_AGENT_REJECTION_EVIDENCE:?ADDP_AGENT_REJECTION_EVIDENCE is required}"
        exec "$PYTHON" "$ROOT_DIR/evals/agent-scenarios/gate.py" \
            --require-online \
            --online "read-only-query=$ADDP_AGENT_READ_ONLY_EVIDENCE" \
            --online "approval-execution=$ADDP_AGENT_APPROVAL_EVIDENCE" \
            --online "rejection-and-forbidden=$ADDP_AGENT_REJECTION_EVIDENCE" \
            --output "$REPORT"
        ;;
    compare|compare-release)
        : "${ADDP_AGENT_EVAL_BASELINE:?ADDP_AGENT_EVAL_BASELINE is required}"
        : "${ADDP_AGENT_EVAL_CURRENT:?ADDP_AGENT_EVAL_CURRENT is required}"
        if [ "$MODE" = "compare-release" ]; then
            exec "$PYTHON" "$ROOT_DIR/evals/agent-scenarios/gate.py" \
                --compare "$ADDP_AGENT_EVAL_BASELINE" "$ADDP_AGENT_EVAL_CURRENT" \
                --require-release-ready \
                --output "$REPORT"
        fi
        exec "$PYTHON" "$ROOT_DIR/evals/agent-scenarios/gate.py" \
            --compare "$ADDP_AGENT_EVAL_BASELINE" "$ADDP_AGENT_EVAL_CURRENT" \
            --output "$REPORT"
        ;;
    *)
        echo "Usage: bash scripts/test/agent-evaluation-gate.sh [offline|release|compare|compare-release]" >&2
        exit 2
        ;;
esac
