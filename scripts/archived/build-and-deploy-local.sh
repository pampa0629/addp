#!/bin/bash
# 服务器端构建和部署脚本
# 在服务器上直接构建 AMD64 镜像并部署

set -e

echo "==========================================="
echo "  ADDP 服务器端构建与部署脚本"
echo "==========================================="
echo ""

# 检查是否在项目根目录
if [ ! -f "docker-compose.prod.yml" ]; then
  echo "❌ 错误: 请在项目根目录运行此脚本"
  exit 1
fi

# 服务列表
SERVICES=(
  "system-backend"
  "system-frontend"
  "manager-backend"
  "manager-frontend"
  "meta-backend"
  "gateway"
  "portal"
)

echo "将构建以下服务:"
printf '  - %s\n' "${SERVICES[@]}"
echo ""

# 确认继续
read -p "是否继续构建？(y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "已取消"
  exit 0
fi

START_TIME=$(date +%s)
BUILT=0
FAILED=0

echo "==========================================="
echo "  开始构建镜像"
echo "==========================================="
echo ""

# 构建每个服务
for service in "${SERVICES[@]}"; do
  echo "[$((BUILT + FAILED + 1))/${#SERVICES[@]}] 构建: $service"

  # 确定 Dockerfile 和构建上下文
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

  # 检查 Dockerfile
  if [ ! -f "$DOCKERFILE" ]; then
    echo "  ⚠️  Dockerfile 不存在: $DOCKERFILE"
    ((FAILED++))
    continue
  fi

  IMAGE_NAME="addp-$service:latest"

  echo "  - Dockerfile: $DOCKERFILE"
  echo "  - Context: $CONTEXT"
  echo "  - 构建中..."

  # 构建镜像
  if docker build \
    -t "$IMAGE_NAME" \
    -f "$DOCKERFILE" \
    "$CONTEXT" > /tmp/build-$service.log 2>&1; then
    echo "  ✅ 构建成功"
    ((BUILT++))
  else
    echo "  ❌ 构建失败，查看日志: /tmp/build-$service.log"
    ((FAILED++))
    tail -20 /tmp/build-$service.log
  fi
  echo ""
done

END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))

echo "==========================================="
echo "  构建完成"
echo "==========================================="
echo ""
echo "统计信息："
echo "  - 成功构建: $BUILT 个镜像"
echo "  - 失败: $FAILED 个镜像"
echo "  - 总耗时: ${ELAPSED}s"
echo ""

# 列出构建的镜像
echo "本地镜像："
docker images | grep "^addp-" | awk '{printf "  - %s:%s (%s)\n", $1, $2, $7}'
echo ""

if [ $FAILED -gt 0 ]; then
  echo "⚠️  部分镜像构建失败，请检查日志"
  exit 1
fi

echo "==========================================="
echo "  开始部署"
echo "==========================================="
echo ""

# 检查 .env 文件
if [ ! -f ".env" ]; then
  echo "⚠️  未找到 .env 文件"
  if [ -f ".env.example" ]; then
    echo "正在复制 .env.example 到 .env"
    cp .env.example .env
    echo "⚠️  请编辑 .env 文件配置密钥和密码"
    read -p "按任意键继续..." -n1 -s
  else
    echo "❌ 错误: 未找到 .env.example"
    exit 1
  fi
fi

# 停止现有服务
echo "停止现有服务..."
docker-compose -f docker-compose.prod.yml down 2>/dev/null || true

# 启动服务
echo "启动服务..."
REGISTRY="" docker-compose -f docker-compose.prod.yml up -d

echo ""
echo "==========================================="
echo "  ✅ 部署完成！"
echo "==========================================="
echo ""
echo "服务状态："
docker-compose -f docker-compose.prod.yml ps
echo ""
echo "访问地址："
echo "  - Portal: http://localhost:5170"
echo "  - Gateway: http://localhost:8000"
echo "  - System Backend: http://localhost:8080"
echo ""
echo "查看日志:"
echo "  docker-compose -f docker-compose.prod.yml logs -f"
echo ""
