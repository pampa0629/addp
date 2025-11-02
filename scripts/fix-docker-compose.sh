#!/bin/bash
# 快速修复服务器上的 docker-compose.prod.yml
# 移除 init-db.sql 挂载，使用新的 PostgreSQL 镜像

echo "==========================================="
echo "  修复 docker-compose.prod.yml"
echo "==========================================="
echo ""

COMPOSE_FILE="docker-compose.prod.yml"

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "❌ 错误: $COMPOSE_FILE 不存在"
  exit 1
fi

echo "当前配置检查："
echo ""

# 检查是否使用旧镜像
if grep -q "addp-infra-postgres:15-alpine" "$COMPOSE_FILE"; then
  echo "❌ 使用旧的 PostgreSQL 镜像（不含初始化脚本）"
  NEEDS_FIX=true
else
  echo "✅ 使用新的 PostgreSQL 镜像（含初始化脚本）"
  NEEDS_FIX=false
fi

# 检查是否挂载 init-db.sql
if grep -q "init-db.sql" "$COMPOSE_FILE"; then
  echo "❌ 仍然挂载 init-db.sql 文件"
  NEEDS_FIX=true
else
  echo "✅ 未挂载 init-db.sql"
fi

echo ""

if [ "$NEEDS_FIX" = false ]; then
  echo "✅ 配置已是最新，无需修复"
  exit 0
fi

echo "需要修复配置文件"
echo ""
read -p "是否继续修复？(y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "已取消"
  exit 0
fi

# 备份原文件
echo "==> 备份原文件..."
cp "$COMPOSE_FILE" "${COMPOSE_FILE}.backup.$(date +%Y%m%d_%H%M%S)"
echo "✅ 已备份到: ${COMPOSE_FILE}.backup.$(date +%Y%m%d_%H%M%S)"

# 修复 1: 更新 PostgreSQL 镜像
echo "==> 更新 PostgreSQL 镜像..."
sed -i.tmp1 's|addp-infra-postgres:15-alpine|addp-infra-postgres-init:15-alpine|g' "$COMPOSE_FILE"
echo "✅ PostgreSQL 镜像已更新"

# 修复 2: 移除 init-db.sql 挂载
echo "==> 移除 init-db.sql 挂载..."
sed -i.tmp2 '/init-db.sql:\/docker-entrypoint-initdb.d\/init-db.sql/d' "$COMPOSE_FILE"
echo "✅ init-db.sql 挂载已移除"

# 清理临时文件
rm -f "${COMPOSE_FILE}.tmp1" "${COMPOSE_FILE}.tmp2"

echo ""
echo "==========================================="
echo "  ✅ 修复完成！"
echo "==========================================="
echo ""

echo "修复后的 PostgreSQL 配置:"
grep -A5 "postgres:" "$COMPOSE_FILE" | head -10

echo ""
echo "下一步："
echo "  1. 重新运行部署: REGISTRY=<registry-address> ./scripts/deploy-from-registry.sh"
echo "  2. 或直接启动: docker-compose -f docker-compose.prod.yml up -d"
echo ""
