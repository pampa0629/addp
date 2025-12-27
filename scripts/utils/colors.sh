#!/bin/bash

# colors.sh - 统一的颜色输出工具
# 使用方法: source scripts/utils/colors.sh
#
# 环境变量控制:
#   NO_COLOR=1     强制禁用颜色
#   FORCE_COLOR=1  强制启用颜色

# 自动检测终端是否支持颜色
# 检测条件:
# 1. NO_COLOR 未设置
# 2. (FORCE_COLOR 设置) 或 (输出到 TTY 且终端支持颜色)
if [ -z "$NO_COLOR" ] && { [ -n "$FORCE_COLOR" ] || { [ -t 1 ] && command -v tput >/dev/null 2>&1 && [ "$(tput colors 2>/dev/null || echo 0)" -ge 8 ]; }; }; then
  # 终端支持颜色
  GREEN='\033[0;32m'
  BLUE='\033[0;34m'
  YELLOW='\033[1;33m'
  RED='\033[0;31m'
  CYAN='\033[0;36m'
  MAGENTA='\033[0;35m'
  WHITE='\033[1;37m'
  NC='\033[0m' # No Color
else
  # 终端不支持颜色或输出被重定向到文件/管道
  # 使用空字符串，避免输出 ANSI 转义序列
  GREEN=''
  BLUE=''
  YELLOW=''
  RED=''
  CYAN=''
  MAGENTA=''
  WHITE=''
  NC=''
fi

# 导出变量，供子进程使用
export GREEN BLUE YELLOW RED CYAN MAGENTA WHITE NC
