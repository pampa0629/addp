#!/bin/bash
# 一键推送所有镜像（ADDP 应用镜像 + 基础设施镜像）到私有 Registry

set -e

REGISTRY="${1:-localhost:5001}"

echo "==========================================="
echo "  ADDP 完整镜像推送脚本"
echo "==========================================="
echo ""
echo "目标 Registry: $REGISTRY"
echo ""
echo "将推送:"
echo "  1. ADDP 应用镜像 (7个)"
echo "  2. 基础设施镜像 (4个)"
echo ""

# 确认继续
read -p "是否继续？(y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "已取消"
  exit 0
fi

START_TIME=$(date +%s)

# 步骤 1: 推送基础设施镜像
echo ""
echo "==========================================="
echo "  步骤 1/2: 推送基础设施镜像"
echo "==========================================="
echo ""

./scripts/push-infrastructure-images.sh "$REGISTRY" <<< "y"

if [ $? -ne 0 ]; then
  echo "❌ 基础设施镜像推送失败"
  exit 1
fi

# 步骤 2: 推送 ADDP 应用镜像
echo ""
echo "==========================================="
echo "  步骤 2/2: 推送 ADDP 应用镜像"
echo "==========================================="
echo ""

# 检查是否需要多架构构建
if [[ "$OSTYPE" == "darwin"* ]] && [[ $(uname -m) == "arm64" ]]; then
  echo "检测到 ARM Mac，将构建多架构镜像（AMD64 + ARM64）"
  echo ""
  read -p "是否构建多架构镜像？(Y/n) " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Nn]$ ]]; then
    # 使用多架构构建（如果网络正常）
    if ./scripts/push-to-local-registry-multiarch.sh "$REGISTRY"; then
      echo "✅ 多架构镜像构建成功"
    else
      echo "⚠️  多架构构建失败，回退到单架构"
      ./scripts/push-to-local-registry.sh "$REGISTRY"
    fi
  else
    # 只构建当前架构
    ./scripts/push-to-local-registry.sh "$REGISTRY"
  fi
else
  # Linux 或 Intel Mac，直接构建
  ./scripts/push-to-local-registry.sh "$REGISTRY"
fi

END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))

echo ""
echo "==========================================="
echo "  ✅ 所有镜像推送完成！"
echo "==========================================="
echo ""
echo "总耗时: ${ELAPSED}s"
echo ""
echo "Registry 信息："
echo "  - URL: http://$REGISTRY"
echo ""

# 列出所有镜像
echo "Registry 中的所有镜像:"
curl -s "http://$REGISTRY/v2/_catalog" | jq -r '.repositories[]' | sort | awk '{print "  - " $0}'

echo ""
echo "下一步："
echo "  1. 传输更新后的 docker-compose.prod.yml 到服务器"
echo "     rsync -avz docker-compose.prod.yml business/docker-compose.prod.yml server:/path/"
echo ""
echo "  2. 在服务器上运行部署:"
echo "     REGISTRY=$REGISTRY ./scripts/deploy-from-registry.sh"
echo ""
