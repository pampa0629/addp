#!/bin/bash
# 快速诊断和修复服务器上的部署问题

set -e

echo "==========================================="
echo "  ADDP 部署问题诊断和修复"
echo "==========================================="
echo ""

COMPOSE_FILE="${1:-docker-compose.prod.yml}"

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "❌ 错误: $COMPOSE_FILE 不存在"
  exit 1
fi

echo "检查配置文件: $COMPOSE_FILE"
echo ""

# 问题 1: 检查 PostgreSQL 镜像
echo "1. 检查 PostgreSQL 镜像配置..."
if grep -q "addp-infra-postgres-init:15-alpine" "$COMPOSE_FILE"; then
  echo "   ✅ 使用正确的 PostgreSQL 镜像 (postgres-init)"
  PG_OK=true
else
  if grep -q "addp-infra-postgres:15-alpine" "$COMPOSE_FILE"; then
    echo "   ❌ 使用旧的 PostgreSQL 镜像（不含初始化脚本）"
    PG_OK=false
  else
    echo "   ❌ PostgreSQL 镜像配置异常"
    PG_OK=false
  fi
fi

# 问题 2: 检查 init-db.sql 挂载
echo ""
echo "2. 检查 init-db.sql 文件挂载..."
if grep -q "init-db.sql" "$COMPOSE_FILE"; then
  echo "   ❌ 发现 init-db.sql 挂载（这会导致错误）"
  INITDB_OK=false
else
  echo "   ✅ 无 init-db.sql 挂载"
  INITDB_OK=true
fi

# 问题 3: 检查端口配置
echo ""
echo "3. 检查端口配置..."
echo "   MinIO Console 端口:"
MINIO_PORT=$(grep -A10 "minio-system:" "$COMPOSE_FILE" | grep "9001:9001" || echo "")
if [ -n "$MINIO_PORT" ]; then
  echo "   ✅ 使用默认端口 9001"
else
  echo "   ℹ️  使用自定义端口"
fi

echo ""
echo "==========================================="
echo "  诊断结果"
echo "==========================================="
echo ""

NEEDS_FIX=false

if [ "$PG_OK" = false ] || [ "$INITDB_OK" = false ]; then
  NEEDS_FIX=true
  echo "❌ 发现配置问题，需要修复"
  echo ""

  if [ "$PG_OK" = false ]; then
    echo "问题 1: PostgreSQL 镜像不正确"
    echo "  当前: addp-infra-postgres:15-alpine"
    echo "  应为: addp-infra-postgres-init:15-alpine"
    echo ""
  fi

  if [ "$INITDB_OK" = false ]; then
    echo "问题 2: 仍然挂载 init-db.sql 文件"
    echo "  这会导致错误: 'path is not shared from the host'"
    echo ""
  fi

  echo "是否自动修复这些问题？"
  read -p "继续修复? (y/N): " -n 1 -r
  echo

  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "已取消修复"
    echo ""
    echo "手动修复步骤："
    echo "  1. 编辑 $COMPOSE_FILE"
    echo "  2. 修改 PostgreSQL 镜像为: addp-infra-postgres-init:15-alpine"
    echo "  3. 删除包含 'init-db.sql' 的行"
    exit 1
  fi

  echo ""
  echo "==========================================="
  echo "  开始修复"
  echo "==========================================="
  echo ""

  # 备份
  BACKUP="${COMPOSE_FILE}.backup.$(date +%Y%m%d_%H%M%S)"
  cp "$COMPOSE_FILE" "$BACKUP"
  echo "✅ 已备份到: $BACKUP"

  # 修复 PostgreSQL 镜像
  if [ "$PG_OK" = false ]; then
    echo "==> 修复 PostgreSQL 镜像..."
    sed -i.tmp1 's|addp-infra-postgres:15-alpine|addp-infra-postgres-init:15-alpine|g' "$COMPOSE_FILE"
    echo "✅ PostgreSQL 镜像已更新"
  fi

  # 移除 init-db.sql 挂载
  if [ "$INITDB_OK" = false ]; then
    echo "==> 移除 init-db.sql 挂载..."
    sed -i.tmp2 '/init-db.sql:\/docker-entrypoint-initdb.d\/init-db.sql/d' "$COMPOSE_FILE"
    echo "✅ init-db.sql 挂载已移除"
  fi

  # 清理临时文件
  rm -f "${COMPOSE_FILE}.tmp1" "${COMPOSE_FILE}.tmp2"

  echo ""
  echo "==========================================="
  echo "  ✅ 修复完成"
  echo "==========================================="
  echo ""

  # 显示修复后的配置
  echo "修复后的 PostgreSQL 配置:"
  awk '/postgres:/,/redis:/' "$COMPOSE_FILE" | head -20

else
  echo "✅ 配置正确，无需修复"
fi

echo ""
echo "==========================================="
echo "  下一步"
echo "==========================================="
echo ""

if [ "$NEEDS_FIX" = true ]; then
  echo "配置已修复，现在可以部署:"
  echo ""
fi

echo "运行以下命令启动服务:"
echo "  REGISTRY=192.168.31.238:5001 docker-compose -f $COMPOSE_FILE up -d"
echo ""
echo "或使用部署脚本:"
echo "  REGISTRY=192.168.31.238:5001 ./scripts/deploy-from-registry.sh"
echo ""
