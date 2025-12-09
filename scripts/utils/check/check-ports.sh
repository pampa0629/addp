#!/bin/bash
# 检查端口占用情况

echo "==========================================="
echo "  检查 ADDP 所需端口占用情况"
echo "==========================================="
echo ""

# ADDP 需要的端口列表
PORTS=(
  "5432:PostgreSQL"
  "6379:Redis"
  "8000:Gateway"
  "8080:System Backend"
  "8081:Manager Backend"
  "8082:Meta Backend"
  "9000:MinIO API"
  "9001:MinIO Console"
  "5170:Portal"
)

echo "检查端口占用:"
echo ""

CONFLICTS=()

for port_info in "${PORTS[@]}"; do
  PORT=$(echo $port_info | cut -d: -f1)
  SERVICE=$(echo $port_info | cut -d: -f2)

  # 检查端口是否被占用
  if lsof -i :$PORT > /dev/null 2>&1; then
    PROCESS=$(lsof -i :$PORT | tail -1 | awk '{print $1}')
    PID=$(lsof -i :$PORT | tail -1 | awk '{print $2}')
    echo "❌ 端口 $PORT ($SERVICE) 被占用"
    echo "   进程: $PROCESS (PID: $PID)"
    CONFLICTS+=("$PORT:$SERVICE:$PROCESS:$PID")
  else
    echo "✅ 端口 $PORT ($SERVICE) 可用"
  fi
done

echo ""

if [ ${#CONFLICTS[@]} -gt 0 ]; then
  echo "==========================================="
  echo "  发现端口冲突"
  echo "==========================================="
  echo ""
  echo "冲突端口列表:"
  for conflict in "${CONFLICTS[@]}"; do
    PORT=$(echo $conflict | cut -d: -f1)
    SERVICE=$(echo $conflict | cut -d: -f2)
    PROCESS=$(echo $conflict | cut -d: -f3)
    PID=$(echo $conflict | cut -d: -f4)
    echo "  - 端口 $PORT ($SERVICE) ← $PROCESS (PID: $PID)"
  done

  echo ""
  echo "解决方案:"
  echo ""
  echo "方案 A: 停止冲突的进程"
  for conflict in "${CONFLICTS[@]}"; do
    PORT=$(echo $conflict | cut -d: -f1)
    SERVICE=$(echo $conflict | cut -d: -f2)
    PROCESS=$(echo $conflict | cut -d: -f3)
    PID=$(echo $conflict | cut -d: -f4)
    echo "  sudo kill $PID  # 停止 $PROCESS (端口 $PORT)"
  done

  echo ""
  echo "方案 B: 修改 docker-compose.prod.yml 使用不同端口"
  echo "  例如: 9001:9001 改为 19001:9001"

  exit 1
else
  echo "==========================================="
  echo "  ✅ 所有端口都可用"
  echo "==========================================="
  exit 0
fi
