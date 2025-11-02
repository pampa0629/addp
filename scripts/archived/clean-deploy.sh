#!/bin/bash
# 清理旧部署并重新部署

set -e

REGISTRY="${REGISTRY:-}"

if [ -z "$REGISTRY" ]; then
  echo "❌ 错误: 请设置 REGISTRY 环境变量"
  echo "使用方法: REGISTRY=192.168.31.238:5001 $0"
  exit 1
fi

echo "==========================================="
echo "  ADDP 清理并重新部署"
echo "==========================================="
echo ""
echo "Registry: $REGISTRY"
echo "当前目录: $(pwd)"
echo ""

read -p "是否继续？这将停止并删除所有现有容器。(y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "已取消"
  exit 0
fi

echo ""
echo "==========================================="
echo "  步骤 1/5: 清理旧部署"
echo "==========================================="
echo ""

# 停止并删除所有 ADDP 相关容器
echo "==> 停止所有容器..."
docker-compose -f docker-compose.prod.yml --profile full down -v 2>/dev/null || true

# 删除可能存在的孤立容器
echo "==> 清理孤立容器..."
docker ps -a | grep -E "addp-|business-" | awk '{print $1}' | xargs -r docker rm -f 2>/dev/null || true

# 清理未使用的卷（可选，谨慎使用）
# docker volume prune -f

echo "✅ 旧部署已清理"
echo ""

echo "==========================================="
echo "  步骤 2/5: 验证配置文件"
echo "==========================================="
echo ""

if [ ! -f "docker-compose.prod.yml" ]; then
  echo "❌ 错误: docker-compose.prod.yml 不存在"
  exit 1
fi

echo "检查 docker-compose.prod.yml:"
echo ""

# 检查 PostgreSQL 配置
if grep -q "addp-infra-postgres-init:15-alpine" docker-compose.prod.yml; then
  echo "  ✅ PostgreSQL 镜像: postgres-init (含初始化脚本)"
else
  echo "  ❌ PostgreSQL 镜像配置错误"
  exit 1
fi

# 检查是否有错误的文件挂载
if grep -q "init-db.sql" docker-compose.prod.yml; then
  echo "  ❌ 发现 init-db.sql 文件挂载（需要删除）"
  exit 1
else
  echo "  ✅ 无 init-db.sql 文件挂载"
fi

echo ""

echo "==========================================="
echo "  步骤 3/5: 拉取最新镜像"
echo "==========================================="
echo ""

echo "==> 拉取镜像..."
if docker-compose -f docker-compose.prod.yml --profile full pull; then
  echo "✅ 镜像拉取完成"
else
  echo "❌ 镜像拉取失败"
  echo ""
  echo "请检查:"
  echo "  1. Registry 是否可访问: curl http://$REGISTRY/v2/"
  echo "  2. Docker 是否信任 Registry: docker info | grep $REGISTRY"
  exit 1
fi

echo ""

echo "==========================================="
echo "  步骤 4/5: 检查环境变量"
echo "==========================================="
echo ""

if [ ! -f ".env" ]; then
  echo "⚠️  .env 文件不存在"
  if [ -f ".env.example" ]; then
    echo "==> 从 .env.example 创建 .env"
    cp .env.example .env
    echo "✅ 已创建 .env"
    echo ""
    echo "⚠️  重要: 请编辑 .env 文件配置密钥和密码"
    echo "  - JWT_SECRET"
    echo "  - POSTGRES_PASSWORD"
    echo "  - REDIS_PASSWORD"
    echo "  - MINIO_SYSTEM_ROOT_PASSWORD"
    echo "  - ENCRYPTION_KEY"
    echo ""
    read -p "按任意键继续..." -n1 -s
    echo
  else
    echo "❌ 错误: .env.example 也不存在"
    exit 1
  fi
else
  echo "✅ .env 文件存在"
fi

echo ""

echo "==========================================="
echo "  步骤 5/5: 启动服务"
echo "==========================================="
echo ""

echo "==> 启动所有服务..."
echo ""

# 使用明确的 REGISTRY 变量启动
if REGISTRY=$REGISTRY docker-compose -f docker-compose.prod.yml --profile full up -d; then
  echo ""
  echo "✅ 服务启动成功"
else
  echo ""
  echo "❌ 服务启动失败"
  echo ""
  echo "查看详细错误:"
  echo "  docker-compose -f docker-compose.prod.yml logs"
  exit 1
fi

echo ""
echo "==> 等待服务就绪..."
sleep 10

echo ""
echo "==> 检查服务状态..."
docker-compose -f docker-compose.prod.yml ps

echo ""
echo "==========================================="
echo "  ✅ 部署完成！"
echo "==========================================="
echo ""

echo "服务访问地址:"
echo "  - Portal: http://localhost:5170"
echo "  - Gateway: http://localhost:8000"
echo "  - System Backend: http://localhost:8080"
echo "  - MinIO Console: http://localhost:9001"
echo ""

echo "常用命令:"
echo "  查看日志: docker-compose -f docker-compose.prod.yml logs -f"
echo "  查看状态: docker-compose -f docker-compose.prod.yml ps"
echo "  停止服务: docker-compose -f docker-compose.prod.yml down"
echo ""
