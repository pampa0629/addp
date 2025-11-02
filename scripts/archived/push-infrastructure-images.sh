#!/bin/bash
# 推送基础设施镜像到私有 Registry
# 用于离线部署时，将第三方依赖镜像推送到本地 Registry

set -e

# 解析 Registry 参数
REGISTRY_INPUT="${1:-localhost:5001}"

# 智能判断：如果只是端口号，自动添加 localhost
if [[ "$REGISTRY_INPUT" =~ ^[0-9]+$ ]]; then
  REGISTRY="localhost:$REGISTRY_INPUT"
elif [[ "$REGISTRY_INPUT" =~ : ]]; then
  REGISTRY="$REGISTRY_INPUT"
else
  # 假设是 IP 地址，添加默认端口
  REGISTRY="$REGISTRY_INPUT:5000"
fi

echo "==========================================="
echo "  推送基础设施镜像到私有 Registry"
echo "==========================================="
echo ""
echo "目标 Registry: $REGISTRY"
echo ""

# 基础设施镜像列表
INFRASTRUCTURE_IMAGES=(
  "redis:7-alpine"
  "minio/minio:latest"
  "docker.elastic.co/elasticsearch/elasticsearch:8.11.0"
)

# PostgreSQL 使用自定义镜像（包含初始化脚本）
CUSTOM_POSTGRES=true

echo "将推送以下基础设施镜像:"
echo "  - postgres:15-alpine (自定义镜像，含初始化脚本)"
printf '  - %s\n' "${INFRASTRUCTURE_IMAGES[@]}"
echo ""

# 确认继续
read -p "是否继续？(y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "已取消"
  exit 0
fi

START_TIME=$(date +%s)
PUSHED=0
FAILED=0

echo "==========================================="
echo "  开始处理镜像"
echo "==========================================="
echo ""

# 先构建并推送 PostgreSQL 自定义镜像
echo "[1/$((${#INFRASTRUCTURE_IMAGES[@]} + 1))] 处理: postgres (自定义镜像)"
echo "  - Dockerfile: scripts/postgres/Dockerfile"
echo "  - 目标: $REGISTRY/addp-infra-postgres-init:15-alpine"
echo "  - 构建镜像..."

if docker build -t "$REGISTRY/addp-infra-postgres-init:15-alpine" \
  -f scripts/postgres/Dockerfile scripts/postgres/ > /dev/null 2>&1; then
  echo "    ✅ 构建成功"

  echo "  - 推送到 Registry..."
  if docker push "$REGISTRY/addp-infra-postgres-init:15-alpine" > /dev/null 2>&1; then
    echo "    ✅ 推送成功"
    ((PUSHED++))
  else
    echo "    ❌ 推送失败"
    ((FAILED++))
  fi
else
  echo "    ❌ 构建失败"
  ((FAILED++))
fi
echo ""

# 处理其他基础设施镜像
INDEX=2
for image in "${INFRASTRUCTURE_IMAGES[@]}"; do
  echo "[$INDEX/$((${#INFRASTRUCTURE_IMAGES[@]} + 1))] 处理: $image"
  ((INDEX++))

  # 从原镜像名生成目标镜像名
  # postgres:15-alpine -> addp-postgres:15-alpine
  # minio/minio:latest -> addp-minio:latest
  # docker.elastic.co/elasticsearch/elasticsearch:8.11.0 -> addp-elasticsearch:8.11.0

  BASE_NAME=$(echo "$image" | awk -F'/' '{print $NF}' | cut -d: -f1)
  TAG=$(echo "$image" | cut -d: -f2)
  TARGET_IMAGE="$REGISTRY/addp-infra-$BASE_NAME:$TAG"

  echo "  原镜像: $image"
  echo "  目标: $TARGET_IMAGE"

  # 拉取原镜像
  echo "  - 拉取镜像..."
  if docker pull "$image" > /dev/null 2>&1; then
    echo "    ✅ 拉取成功"
  else
    echo "    ❌ 拉取失败"
    ((FAILED++))
    echo ""
    continue
  fi

  # 打标签
  echo "  - 打标签..."
  if docker tag "$image" "$TARGET_IMAGE"; then
    echo "    ✅ 标签成功"
  else
    echo "    ❌ 标签失败"
    ((FAILED++))
    echo ""
    continue
  fi

  # 推送到私有 Registry
  echo "  - 推送到 Registry..."
  if docker push "$TARGET_IMAGE" > /dev/null 2>&1; then
    echo "    ✅ 推送成功"
    ((PUSHED++))
  else
    echo "    ❌ 推送失败"
    ((FAILED++))
  fi

  echo ""
done

END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))

echo "==========================================="
echo "  验证推送结果"
echo "==========================================="
echo ""

# 验证镜像
echo "Registry 中的基础设施镜像:"

# 检查 PostgreSQL
if curl -s "http://$REGISTRY/v2/addp-infra-postgres-init/tags/list" | grep -q "15-alpine" 2>/dev/null; then
  echo "  ✅ addp-infra-postgres-init:15-alpine"
else
  echo "  ❌ addp-infra-postgres-init:15-alpine (未找到)"
fi

# 检查其他镜像
for image in "${INFRASTRUCTURE_IMAGES[@]}"; do
  BASE_NAME=$(echo "$image" | awk -F'/' '{print $NF}' | cut -d: -f1)
  TAG=$(echo "$image" | cut -d: -f2)
  REMOTE_IMAGE="addp-infra-$BASE_NAME:$TAG"

  if curl -s "http://$REGISTRY/v2/addp-infra-$BASE_NAME/tags/list" | grep -q "$TAG" 2>/dev/null; then
    echo "  ✅ $REMOTE_IMAGE"
  else
    echo "  ❌ $REMOTE_IMAGE (未找到)"
  fi
done

echo ""
echo "==========================================="
echo "  ✅ 推送完成！"
echo "==========================================="
echo ""
echo "统计信息："
echo "  - 成功推送: $PUSHED 个镜像"
echo "  - 失败: $FAILED 个镜像"
echo "  - 总耗时: ${ELAPSED}s"
echo ""
echo "Registry 信息："
echo "  - URL: http://$REGISTRY"
echo ""
echo "下一步："
echo "  1. 更新 docker-compose.prod.yml 使用私有 Registry 的基础设施镜像"
echo "  2. 在服务器上运行部署脚本"
echo ""
