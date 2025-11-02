#!/bin/bash
# Binary Compiler
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

ARCH="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
MULTI_ARCH=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --arch)
            ARCH="$2"
            [[ "$ARCH" == "both" ]] && MULTI_ARCH=true && ARCH="amd64,arm64"
            shift 2
            ;;
        *)
            echo -e "${RED}Unknown: $1${NC}"
            exit 1
            ;;
    esac
done

echo -e "${BLUE}ADDP Binary Compiler${NC}"
echo -e "Architecture: ${GREEN}${ARCH}${NC}\n"

compile_service() {
    local name=$1 dir=$2 arch=$3
    echo -e "${YELLOW}Compiling ${name} for linux/${arch}...${NC}"
    cd "$dir"

    # Detect entry point and output name
    local entry_point="./cmd/server"
    local output="server"

    if [ "$name" = "transfer-worker" ]; then
        # Special case for transfer-worker
        entry_point="./cmd/worker"
        output="worker"
    elif [ ! -d "cmd/server" ] && [ -d "cmd/gateway" ]; then
        entry_point="./cmd/gateway"
    fi

    [[ "$MULTI_ARCH" == true ]] && output="${output}-${arch}"

    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -ldflags="-s -w" -o "$output" $entry_point

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Compiled ${name} ($(du -h $output | cut -f1))${NC}"
        cd - > /dev/null
        return 0
    else
        echo -e "${RED}✗ Failed ${name}${NC}"
        cd - > /dev/null
        return 1
    fi
}

SERVICES=("system-backend:system/backend" "manager-backend:manager/backend" "meta-backend:meta/backend" "transfer-backend:transfer/backend" "transfer-worker:transfer/backend" "gateway:gateway")
failed=()

for svc in "${SERVICES[@]}"; do
    name="${svc%%:*}"
    dir="${svc#*:}"
    echo -e "\n${BLUE}Building: ${name}${NC}"
    
    if [[ "$MULTI_ARCH" == true ]]; then
        IFS=',' read -ra ARCHS <<< "$ARCH"
        for a in "${ARCHS[@]}"; do
            compile_service "$name" "$dir" "$a" || failed+=("$name ($a)")
        done
    else
        compile_service "$name" "$dir" "$ARCH" || failed+=("$name")
    fi
done

echo -e "\n${BLUE}Summary${NC}"
if [ ${#failed[@]} -eq 0 ]; then
    echo -e "${GREEN}✓ All compiled successfully${NC}"
    exit 0
else
    echo -e "${RED}✗ Failed: ${failed[*]}${NC}"
    exit 1
fi
