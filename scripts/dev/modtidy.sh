#!/bin/bash

# modtidy.sh - Run go mod tidy on all backend Go modules (concurrent)
# Usage: ./scripts/dev/modtidy.sh

set -e

# 加载颜色定义
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../utils/colors.sh"

# Project root directory
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

echo -e "${BLUE}=== ADDP Go Modules Tidy (并发模式) ===${NC}\n"

# List of all Go backend modules
GO_MODULES=(
    "common"
    "system/backend"
    "manager/backend"
    "meta/backend"
    "transfer/backend"
    "orchestrator/backend"
    "develop/backend"
    "service/backend"
    "gateway"
)

# Temporary directory for storing results
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

# Track valid modules
VALID_MODULES=()

# Phase 1: Validate modules
echo -e "${YELLOW}🔍 检查模块...${NC}"
for module in "${GO_MODULES[@]}"; do
    MODULE_PATH="$PROJECT_ROOT/$module"

    if [ ! -d "$MODULE_PATH" ]; then
        echo -e "${RED}  ⏭  跳过 $module (目录不存在)${NC}"
        continue
    fi

    if [ ! -f "$MODULE_PATH/go.mod" ]; then
        echo -e "${RED}  ⏭  跳过 $module (无 go.mod)${NC}"
        continue
    fi

    VALID_MODULES+=("$module")
    echo -e "${GREEN}  ✓ $module${NC}"
done

if [ ${#VALID_MODULES[@]} -eq 0 ]; then
    echo -e "${RED}❌ 没有找到有效的 Go 模块${NC}"
    exit 1
fi

echo ""
echo -e "${YELLOW}📦 并发执行 go mod tidy (${#VALID_MODULES[@]} 个模块)...${NC}"
echo ""

# Phase 2: Concurrent execution
tidy_module() {
    local module=$1
    local MODULE_PATH="$PROJECT_ROOT/$module"
    local result_file="$TEMP_DIR/$(echo $module | tr '/' '_').result"
    local start_time=$(date +%s)

    cd "$MODULE_PATH"

    # Capture both stdout and stderr (no timeout, rely on go mod tidy's own timeout)
    if GOPROXY=https://goproxy.cn,direct go mod tidy > "$result_file" 2>&1; then
        local end_time=$(date +%s)
        local duration=$((end_time - start_time))
        echo "SUCCESS" > "$result_file.status"
        echo -e "${GREEN}✓ $module${NC} (${duration}s)"
    else
        local exit_code=$?
        echo "FAILED" > "$result_file.status"
        echo -e "${RED}✗ $module${NC} (失败, exit code: $exit_code)"
    fi
}

# Export function and variables for parallel execution
export -f tidy_module
export PROJECT_ROOT TEMP_DIR GREEN RED NC YELLOW BLUE

# Launch all go mod tidy in parallel (background jobs)
declare -a PIDS
for module in "${VALID_MODULES[@]}"; do
    tidy_module "$module" &
    PIDS+=($!)
done

# Phase 3: Wait for all background jobs with progress indicator
echo -e "${YELLOW}等待所有任务完成...${NC}"
echo ""

# Monitor progress every second
completed_prev=0
spinner_chars=("⠋" "⠙" "⠹" "⠸" "⠼" "⠴" "⠦" "⠧" "⠇" "⠏")
spinner_idx=0

while true; do
    # Count completed jobs
    completed=0
    running_modules=()
    success_count=0
    fail_count=0

    for module in "${VALID_MODULES[@]}"; do
        result_file="$TEMP_DIR/$(echo $module | tr '/' '_').result"
        status_file="$result_file.status"
        if [ -f "$status_file" ]; then
            status=$(cat "$status_file")
            if [ "$status" = "SUCCESS" ]; then
                ((success_count++))
            else
                ((fail_count++))
            fi
            ((completed++))
        else
            running_modules+=("$module")
        fi
    done

    # Check if all completed
    if [ $completed -eq ${#VALID_MODULES[@]} ]; then
        break
    fi

    # Show progress with spinner
    spinner="${spinner_chars[$spinner_idx]}"
    spinner_idx=$(( (spinner_idx + 1) % ${#spinner_chars[@]} ))

    # Clear line and show progress
    echo -ne "\r\033[K"  # Clear entire line
    echo -ne "${spinner} ${YELLOW}[进度 $completed/${#VALID_MODULES[@]}]${NC}"
    echo -ne " ${GREEN}成功: $success_count${NC}"
    if [ $fail_count -gt 0 ]; then
        echo -ne " ${RED}失败: $fail_count${NC}"
    fi

    # Show first 3 running modules
    if [ ${#running_modules[@]} -gt 0 ]; then
        display_modules=("${running_modules[@]:0:3}")
        echo -ne " | 执行中: ${display_modules[*]}"
        if [ ${#running_modules[@]} -gt 3 ]; then
            echo -ne " (+$((${#running_modules[@]} - 3)) 个)"
        fi
    fi

    sleep 0.5
done

# Final wait to ensure all processes finish
wait

echo -e "\n${GREEN}✓ 所有任务已完成${NC}"

# Phase 4: Collect results
echo ""
echo -e "${BLUE}=== 汇总结果 ===${NC}"

SUCCESS_COUNT=0
FAILED_MODULES=()

for module in "${VALID_MODULES[@]}"; do
    result_file="$TEMP_DIR/$(echo $module | tr '/' '_').result"
    status_file="$result_file.status"

    if [ -f "$status_file" ]; then
        status=$(cat "$status_file")
        if [ "$status" = "SUCCESS" ]; then
            SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        else
            FAILED_MODULES+=("$module")
        fi
    else
        FAILED_MODULES+=("$module")
    fi
done

echo -e "${GREEN}✓ 成功: $SUCCESS_COUNT/${#VALID_MODULES[@]} 模块${NC}"

if [ ${#FAILED_MODULES[@]} -gt 0 ]; then
    echo -e "${RED}✗ 失败: ${#FAILED_MODULES[@]} 模块${NC}"
    for failed in "${FAILED_MODULES[@]}"; do
        echo -e "  ${RED}- $failed${NC}"

        # Show error details
        result_file="$TEMP_DIR/$(echo $failed | tr '/' '_').result"
        if [ -f "$result_file" ]; then
            echo -e "${YELLOW}    错误详情:${NC}"
            sed 's/^/    /' "$result_file"
        fi
    done
    echo ""
    exit 1
else
    echo -e "${GREEN}🎉 所有模块整理完成!${NC}"
fi
