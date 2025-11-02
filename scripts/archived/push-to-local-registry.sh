#!/bin/bash
# 构建 ADDP 镜像并推送到本地私有 Registry
#
# 使用方法:
#   ./scripts/push-to-local-registry.sh                    # 使用默认 localhost:5000
#   ./scripts/push-to-local-registry.sh 5001               # 使用 localhost:5001
#   ./scripts/push-to-local-registry.sh localhost:5001     # 使用 localhost:5001
#   ./scripts/push-to-local-registry.sh 192.168.1.100:5001 # 使用局域网 IP:端口

set -e

# 智能解析参数
if [ -z "$1" ]; then
  # 无参数，尝试自动检测本机运行的 Registry
  REGISTRY="localhost:5000"

  # 检测是否有 Registry 容器在运行，并获取其端口
  if docker ps --format '{{.Names}}\t{{.Ports}}' | grep -q "addp-registry"; then
    DETECTED_PORT=$(docker ps --format '{{.Names}}\t{{.Ports}}' | grep "addp-registry" | grep -o '0.0.0.0:[0-9]*' | head -1 | cut -d: -f2)
    if [ -n "$DETECTED_PORT" ]; then
      REGISTRY="localhost:$DETECTED_PORT"
      echo "ℹ️  自动检测到 Registry 运行在端口 $DETECTED_PORT"
    fi
  fi
elif [[ "$1" =~ ^[0-9]+$ ]]; then
  # 参数是纯数字，视为端口号
  REGISTRY="localhost:$1"
elif [[ "$1" =~ : ]]; then
  # 参数包含冒号，视为完整的 host:port
  REGISTRY="$1"
else
  # 参数是主机名或 IP，使用默认端口 5000
  REGISTRY="$1:5000"
fi

# 定义所有需要构建的服务
SERVICES=(
  "system-backend"
  "system-frontend"
  "manager-backend"
  "manager-frontend"
  "meta-backend"
  "gateway"
  "portal"
)

echo "=========================================="
echo "  ADDP 镜像构建与推送脚本"
echo "=========================================="
echo ""
echo "目标 Registry: $REGISTRY"
echo "镜像数量: ${#SERVICES[@]}"
echo ""

# 提取端口号用于提示信息
REGISTRY_PORT=$(echo "$REGISTRY" | grep -o ':[0-9]*$' | tr -d ':')
if [ -z "$REGISTRY_PORT" ]; then
  REGISTRY_PORT="5000"
fi

# 检查 Registry 是否可访问
echo "==> 检查 Registry 连接..."
if ! curl -sf "http://$REGISTRY/v2/" > /dev/null; then
  echo "❌ 错误: 无法连接到 Registry: $REGISTRY"
  echo ""
  echo "请确保："
  echo "  1. Registry 服务已启动:"
  echo "     docker ps | grep addp-registry"
  echo ""
  echo "  2. 如果 Registry 未运行，请先启动:"
  echo "     ./scripts/setup-local-registry.sh $REGISTRY_PORT"
  echo ""
  echo "  3. 检查网络连接:"
  echo "     curl http://$REGISTRY/v2/"
  echo ""
  echo "  4. 如果使用局域网 IP，确保防火墙允许 $REGISTRY_PORT 端口"
  echo ""
  echo "提示："
  echo "  - 使用其他端口: ./scripts/push-to-local-registry.sh 5001"
  echo "  - 使用局域网 IP: ./scripts/push-to-local-registry.sh 192.168.1.100:5001"
  exit 1
fi
echo "✅ Registry 连接正常"
echo ""

# 构建所有镜像
echo "=========================================="
echo "  步骤 1/3: 构建镜像"
echo "=========================================="
echo ""

START_TIME=$(date +%s)

echo "==> 开始构建所有 ADDP 镜像..."
if ! make docker-build-all; then
  echo "❌ 镜像构建失败"
  exit 1
fi
echo "✅ 所有镜像构建完成"
echo ""

# 标记和推送镜像
echo "=========================================="
echo "  步骤 2/3: 标记并推送镜像"
echo "=========================================="
echo ""

PUSHED=0
FAILED=0

for service in "${SERVICES[@]}"; do
  LOCAL_IMAGE="addp-$service:latest"
  REMOTE_IMAGE="$REGISTRY/addp-$service:latest"

  echo "[$((PUSHED + FAILED + 1))/${#SERVICES[@]}] 处理: $service"

  # 检查本地镜像是否存在
  if ! docker images | grep -q "addp-$service.*latest"; then
    echo "  ⚠️  警告: 本地镜像 $LOCAL_IMAGE 不存在，跳过"
    ((FAILED++))
    continue
  fi

  # 标记镜像
  echo "  - 标记镜像: $REMOTE_IMAGE"
  if ! docker tag "$LOCAL_IMAGE" "$REMOTE_IMAGE"; then
    echo "  ❌ 标记失败"
    ((FAILED++))
    continue
  fi

  # 推送镜像
  echo "  - 推送镜像..."
  if docker push "$REMOTE_IMAGE" 2>&1 | grep -v "Waiting" | grep -v "Layer already exists"; then
    echo "  ✅ 推送成功"
    ((PUSHED++))
  else
    echo "  ❌ 推送失败"
    ((FAILED++))
  fi
  echo ""
done

# 清理本地标记的镜像（可选）
echo "=========================================="
echo "  步骤 3/3: 验证推送结果"
echo "=========================================="
echo ""

echo "==> 查询 Registry 中的镜像列表..."
CATALOG=$(curl -s "http://$REGISTRY/v2/_catalog" | grep -o '"repositories":\[.*\]')
echo "Registry 镜像目录:"
echo "$CATALOG" | sed 's/,/\n/g' | sed 's/.*"\(addp-[^"]*\)".*/  - \1/g'
echo ""

END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))

echo "=========================================="
echo "  ✅ 推送完成！"
echo "=========================================="
echo ""
echo "统计信息："
echo "  - 成功推送: $PUSHED 个镜像"
echo "  - 失败/跳过: $FAILED 个镜像"
echo "  - 总耗时: ${ELAPSED}s"
echo ""
echo "Registry 信息："
echo "  - URL: http://$REGISTRY"
echo "  - 查看镜像: curl http://$REGISTRY/v2/_catalog"
echo ""
echo "下一步："
echo "  1. 确保服务器可以访问本机 Registry ($REGISTRY)"
echo "  2. 在服务器上运行部署脚本:"
echo "     REGISTRY=$REGISTRY ./scripts/deploy-from-registry.sh"
echo ""
echo "提示："
echo "  - 本机需要保持开机，Registry 容器需要持续运行"
echo "  - 服务器拉取镜像期间，建议关闭本机防火墙或开放 $REGISTRY_PORT 端口"
echo ""
echo "快速启动 Registry (如果未运行):"
echo "  ./scripts/setup-local-registry.sh $REGISTRY_PORT"
echo "=========================================="
