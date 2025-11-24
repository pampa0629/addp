#!/bin/bash
# Docker 容器内存监控脚本

echo "==================================="
echo "  Docker 容器内存使用情况"
echo "==================================="
echo ""

# 显示所有容器的内存使用（按内存降序排列）
docker stats --no-stream --format "table {{.Name}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.CPUPerc}}" | \
  (read -r; printf "%s\n" "$REPLY"; sort -t $'\t' -k3 -rn)

echo ""
echo "==================================="
echo "  总计统计"
echo "==================================="

# 计算总内存使用
TOTAL_MEM=$(docker stats --no-stream --format "{{.MemUsage}}" | \
  awk -F' / ' '{print $1}' | \
  awk '{
    if ($2 == "GiB") sum += $1 * 1024
    else if ($2 == "MiB") sum += $1
    else if ($2 == "KiB") sum += $1 / 1024
  } END {printf "%.2f MiB (%.2f GiB)", sum, sum/1024}')

echo "所有容器总内存使用: $TOTAL_MEM"
echo ""
echo "提示: 按 Ctrl+C 停止"
echo "持续监控模式: watch -n 2 ./scripts/monitor-memory.sh"