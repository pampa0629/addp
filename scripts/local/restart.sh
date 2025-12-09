#!/bin/bash
# =============================================================================
# ADDP Local Docker Deployment Restarter
# =============================================================================
# Description: Restart ADDP services by stopping and starting them
# Usage: ./scripts/local/restart.sh [OPTIONS]
#
# Options:
#   --all       Restart both application and infrastructure layers
#
# Default behavior: Restart application layer only (keep infrastructure running)
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Color codes
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Parse arguments
RESTART_ALL=""
if [[ "$1" == "--all" ]]; then
    RESTART_ALL="--all"
fi

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}🔄 ADDP Local Docker Restart${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Stop services (but keep volumes)
"$SCRIPT_DIR/stop.sh" $RESTART_ALL

# Wait for containers to fully stop
echo ""
echo "⏳ Waiting 3 seconds for containers to fully stop..."
sleep 3

# Start services
echo ""
"$SCRIPT_DIR/start.sh"
