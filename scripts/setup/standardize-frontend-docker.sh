#!/bin/bash
# =============================================================================
# Frontend Docker Standardization Script
# =============================================================================
# Description: Ensures all frontend modules have standardized Docker build setup
# Usage: ./standardize-frontend-docker.sh [--fix]
# Options:
#   --fix    Automatically create missing files
# =============================================================================

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Get project root
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$PROJECT_ROOT"

# Flag to auto-fix issues
FIX_MODE=false
if [[ "$1" == "--fix" ]]; then
    FIX_MODE=true
fi

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Frontend Docker Standardization Check${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
if [ "$FIX_MODE" = true ]; then
    echo -e "${YELLOW}Running in FIX mode - will create missing files${NC}"
else
    echo -e "${YELLOW}Running in CHECK mode - use --fix to auto-create files${NC}"
fi
echo ""

# Define all frontend modules
FRONTENDS=(
    "portal/frontend"
    "system/frontend"
    "manager/frontend"
    "meta/frontend"
    "transfer/frontend"
    "orchestrator/frontend"
    "develop/frontend"
)

# Track issues
ISSUES_FOUND=0
ISSUES_FIXED=0

# Standard .dockerignore content
read -r -d '' DOCKERIGNORE_CONTENT <<'EOF' || true
# Dependencies
node_modules/
npm-debug.log*
yarn-debug.log*
yarn-error.log*
pnpm-debug.log*

# Build outputs
dist/
dist-ssr/
.output/
.nuxt/

# IDE
.vscode/
.idea/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db

# Environment
.env
.env.local
.env.*.local

# Testing
coverage/
.nyc_output/

# Temp
*.log
.cache/
EOF

# Function to check and fix a frontend module
check_frontend() {
    local frontend_path=$1
    local module_name=$(echo "$frontend_path" | cut -d'/' -f1)

    echo -e "${BLUE}Checking: ${frontend_path}${NC}"

    # Check if directory exists
    if [ ! -d "$frontend_path" ]; then
        echo -e "  ${YELLOW}⚠ Directory not found, skipping${NC}"
        return
    fi

    local has_issues=false

    # Check 1: Dockerfile exists
    if [ ! -f "$frontend_path/Dockerfile" ]; then
        echo -e "  ${RED}✗ Missing Dockerfile${NC}"
        has_issues=true
        ISSUES_FOUND=$((ISSUES_FOUND + 1))
    else
        echo -e "  ${GREEN}✓ Dockerfile exists${NC}"

        # Verify base image
        if ! grep -q "FROM node:18-alpine AS builder" "$frontend_path/Dockerfile"; then
            echo -e "  ${YELLOW}⚠ Warning: Not using node:18-alpine base image${NC}"
        fi

        # Verify multi-stage build
        if ! grep -q "FROM nginx:alpine" "$frontend_path/Dockerfile"; then
            echo -e "  ${YELLOW}⚠ Warning: Not using nginx:alpine for production stage${NC}"
        fi
    fi

    # Check 2: nginx.conf exists
    if [ ! -f "$frontend_path/nginx.conf" ]; then
        echo -e "  ${RED}✗ Missing nginx.conf${NC}"
        has_issues=true
        ISSUES_FOUND=$((ISSUES_FOUND + 1))
    else
        echo -e "  ${GREEN}✓ nginx.conf exists${NC}"

        # Verify SPA fallback
        if ! grep -q "try_files.*index.html" "$frontend_path/nginx.conf"; then
            echo -e "  ${YELLOW}⚠ Warning: nginx.conf may not have SPA fallback${NC}"
        fi
    fi

    # Check 3: .dockerignore exists
    if [ ! -f "$frontend_path/.dockerignore" ]; then
        echo -e "  ${RED}✗ Missing .dockerignore${NC}"
        has_issues=true
        ISSUES_FOUND=$((ISSUES_FOUND + 1))

        if [ "$FIX_MODE" = true ]; then
            echo "$DOCKERIGNORE_CONTENT" > "$frontend_path/.dockerignore"
            echo -e "  ${GREEN}✓ Created .dockerignore${NC}"
            ISSUES_FIXED=$((ISSUES_FIXED + 1))
        fi
    else
        echo -e "  ${GREEN}✓ .dockerignore exists${NC}"
    fi

    # Check 4: package.json exists
    if [ ! -f "$frontend_path/package.json" ]; then
        echo -e "  ${RED}✗ Missing package.json${NC}"
        has_issues=true
        ISSUES_FOUND=$((ISSUES_FOUND + 1))
    else
        echo -e "  ${GREEN}✓ package.json exists${NC}"

        # Verify build script
        if ! grep -q '"build"' "$frontend_path/package.json"; then
            echo -e "  ${YELLOW}⚠ Warning: No 'build' script in package.json${NC}"
        fi
    fi

    echo ""
}

# Check all frontends
for frontend in "${FRONTENDS[@]}"; do
    check_frontend "$frontend"
done

# Summary
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Summary${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

if [ $ISSUES_FOUND -eq 0 ]; then
    echo -e "${GREEN}✓ All frontends are properly configured!${NC}"
    exit 0
else
    echo -e "${YELLOW}Found ${ISSUES_FOUND} issue(s)${NC}"

    if [ "$FIX_MODE" = true ]; then
        echo -e "${GREEN}Fixed ${ISSUES_FIXED} issue(s)${NC}"

        if [ $ISSUES_FIXED -lt $ISSUES_FOUND ]; then
            echo ""
            echo -e "${YELLOW}Some issues require manual intervention:${NC}"
            echo "  - Missing Dockerfile: Copy from a working module"
            echo "  - Missing nginx.conf: Copy from a working module"
            echo "  - Missing package.json: Frontend may not be initialized"
        fi
    else
        echo ""
        echo -e "${YELLOW}Run with --fix to automatically create missing .dockerignore files${NC}"
    fi

    exit 1
fi
