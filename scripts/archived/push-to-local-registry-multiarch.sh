#!/bin/bash
# 构建多平台 ADDP 镜像并推送到本地私有 Registry
# 支持 ARM64 (Apple Silicon) 和 AMD64 (Intel) 架构
#
# 使用方法:
#   ./scripts/push-to-local-registry-multiarch.sh                    # 自动检测
#   ./scripts/push-to-local-registry-multiarch.sh 5001               # 指定端口
#   ./scripts/push-to-local-registry-multiarch.sh 192.168.1.100:5001 # 局域网

set -e

# 智能解析参数（与原脚本相同）
if [ -z "$1" ]; then
  REGISTRY="localhost:5000"
  if docker ps --format '{{.Names}}\t{{.Ports}}' | grep -q "addp-registry"; then
    DETECTED_PORT=$(docker ps --format '{{.Names}}\t{{.Ports}}' | grep "addp-registry" | grep -o '0.0.0.0:[0-9]*' | head -1 | cut -d: -f2)
    if [ -n "$DETECTED_PORT" ]; then
      REGISTRY="localhost:$DETECTED_PORT"
      echo "ℹ️  自动检测到 Registry 运行在端口 $DETECTED_PORT"
    fi
  fi
elif [[ "$1" =~ ^[0-9]+$ ]]; then
  REGISTRY="localhost:$1"
elif [[ "$1" =~ : ]]; then
  REGISTRY="$1"
else
  REGISTRY="$1:5000"
fi

# 定义要构建的服务
SERVICES=(
  "system-backend"
  "system-frontend"
  "manager-backend"
  "manager-frontend"
  "meta-backend"
  "gateway"
  "portal"
)

# 定义目标平台
# 默认同时构建 x86 和 ARM 架构，可通过环境变量覆盖
TARGET_PLATFORM="${TARGET_PLATFORM:-linux/amd64,linux/arm64}"

echo "=========================================="
echo "  ADDP 多平台镜像构建与推送脚本"
echo "=========================================="
echo ""
echo "目标 Registry: $REGISTRY"
echo "目标平台: $TARGET_PLATFORM"
echo "镜像数量: ${#SERVICES[@]}"
echo ""

# 提取端口号
REGISTRY_PORT=$(echo "$REGISTRY" | grep -o ':[0-9]*$' | tr -d ':')
if [ -z "$REGISTRY_PORT" ]; then
  REGISTRY_PORT="5000"
fi

# 检查 Docker Buildx
echo "==> 检查 Docker Buildx..."
if ! docker buildx version > /dev/null 2>&1; then
  echo "❌ 错误: Docker Buildx 未安装"
  echo ""
  echo "Docker Buildx 是 Docker 的多平台构建工具。"
  echo ""
  echo "安装方法:"
  echo "  1. 确保 Docker Desktop 版本 >= 19.03"
  echo "  2. 启用实验性功能:"
  echo "     Docker Desktop -> Preferences -> Docker Engine"
  echo "     添加: {\"experimental\": true}"
  echo ""
  exit 1
fi
echo "✅ Docker Buildx 已安装"

# 创建或使用 multiarch builder
echo "==> 配置 Buildx Builder..."
BUILDER_NAME="addp-multiarch-builder"

if ! docker buildx ls | grep -q "$BUILDER_NAME"; then
  echo "创建新的 builder: $BUILDER_NAME"
  docker buildx create --name "$BUILDER_NAME" --driver docker-container --bootstrap
else
  echo "使用现有 builder: $BUILDER_NAME"
fi

docker buildx use "$BUILDER_NAME"
echo "✅ Builder 配置完成"
echo ""

# 检查 Registry 连接
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
  exit 1
fi
echo "✅ Registry 连接正常"
echo ""

# 配置 insecure registry (本地 Registry 需要)
echo "==> 配置 buildx 使用 insecure registry..."
cat > /tmp/buildkitd.toml <<EOF
[registry."$REGISTRY"]
  http = true
  insecure = true
EOF

# 重新创建 builder 使用新配置
docker buildx rm "$BUILDER_NAME" 2>/dev/null || true
docker buildx create --name "$BUILDER_NAME" \
  --driver docker-container \
  --config /tmp/buildkitd.toml \
  --bootstrap
docker buildx use "$BUILDER_NAME"
echo "✅ Buildx 配置完成"
echo ""

# 构建并推送镜像
echo "=========================================="
echo "  构建并推送多平台镜像"
echo "=========================================="
echo ""

START_TIME=$(date +%s)
PUSHED=0
FAILED=0

for service in "${SERVICES[@]}"; do
  echo "[$((PUSHED + FAILED + 1))/${#SERVICES[@]}] 处理: $service"

  # 确定 Dockerfile 路径
  case "$service" in
    system-backend)
      DOCKERFILE="system/backend/Dockerfile"
      CONTEXT="."
      ;;
    system-frontend)
      DOCKERFILE="system/frontend/Dockerfile"
      CONTEXT="system/frontend"
      ;;
    manager-backend)
      DOCKERFILE="manager/backend/Dockerfile"
      CONTEXT="."
      ;;
    manager-frontend)
      DOCKERFILE="manager/frontend/Dockerfile"
      CONTEXT="manager/frontend"
      ;;
    meta-backend)
      DOCKERFILE="meta/backend/Dockerfile"
      CONTEXT="."
      ;;
    gateway)
      DOCKERFILE="gateway/Dockerfile"
      CONTEXT="."
      ;;
    portal)
      DOCKERFILE="portal/frontend/Dockerfile"
      CONTEXT="portal/frontend"
      ;;
    *)
      echo "  ⚠️  未知服务，跳过"
      ((FAILED++))
      continue
      ;;
  esac

  # 检查 Dockerfile 是否存在
  if [ ! -f "$DOCKERFILE" ]; then
    echo "  ⚠️  Dockerfile 不存在: $DOCKERFILE，跳过"
    ((FAILED++))
    continue
  fi

  REMOTE_IMAGE="$REGISTRY/addp-$service:latest"

  echo "  - Dockerfile: $DOCKERFILE"
  echo "  - 目标平台: $TARGET_PLATFORM"
  echo "  - 目标镜像: $REMOTE_IMAGE"
  echo "  - 构建并推送..."

  # 使用 buildx 构建多平台镜像并直接推送
  if docker buildx build \
    --platform "$TARGET_PLATFORM" \
    --tag "$REMOTE_IMAGE" \
    --file "$DOCKERFILE" \
    --push \
    --progress plain \
    "$CONTEXT" 2>&1 | grep -v "^#" | grep -v "sha256:" | head -20; then
    echo "  ✅ 构建并推送成功"
    ((PUSHED++))
  else
    echo "  ❌ 构建或推送失败"
    ((FAILED++))
  fi
  echo ""
done

# 验证推送结果
echo "=========================================="
echo "  验证推送结果"
echo "=========================================="
echo ""

echo "==> 查询 Registry 中的镜像列表..."
CATALOG=$(curl -s "http://$REGISTRY/v2/_catalog" | grep -o '"repositories":\[.*\]')
echo "Registry 镜像目录:"
echo "$CATALOG" | sed 's/,/\n/g' | sed 's/.*"\(addp-[^"]*\)".*/  - \1/g'
echo ""

# 验证镜像架构
echo "==> 验证镜像架构..."
for service in "${SERVICES[@]}"; do
  echo -n "检查 addp-$service... "
  MANIFEST=$(curl -s "http://$REGISTRY/v2/addp-$service/manifests/latest" -H "Accept: application/vnd.docker.distribution.manifest.list.v2+json" 2>/dev/null)

  if echo "$MANIFEST" | grep -q "amd64"; then
    echo "✅ (包含 amd64)"
  elif echo "$MANIFEST" | grep -q "platform"; then
    echo "✅ (已推送)"
  else
    echo "⚠️  (可能未包含 amd64)"
  fi
done
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
echo "  - 目标平台: $TARGET_PLATFORM"
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
echo "=========================================="
