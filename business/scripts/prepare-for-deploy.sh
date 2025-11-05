#!/bin/bash
# Business Infrastructure - 本地准备和验证脚本
# 用于在开发机上验证 Business 基础设施配置，并准备部署文件
#
# 使用方法:
#   ./business/scripts/prepare-for-deploy.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUSINESS_DIR="$(dirname "$SCRIPT_DIR")"
PROJECT_ROOT="$(dirname "$BUSINESS_DIR")"

echo "=========================================="
echo "  Business 基础设施 - 部署准备"
echo "=========================================="
echo ""
echo "目录信息："
echo "  - Business 目录: $BUSINESS_DIR"
echo "  - 项目根目录: $PROJECT_ROOT"
echo ""

# 步骤 1: 检查必要文件
echo "=========================================="
echo "  步骤 1/4: 检查必要文件"
echo "=========================================="
echo ""

REQUIRED_FILES=(
  "docker-compose.prod.yml"
  ".env.prod.example"
  "init-db.sql"
)

ALL_EXISTS=true
for file in "${REQUIRED_FILES[@]}"; do
  echo -n "检查 $file... "
  if [ -f "$BUSINESS_DIR/$file" ]; then
    echo "✅"
  else
    echo "❌ 缺失"
    ALL_EXISTS=false
  fi
done

if [ "$ALL_EXISTS" = false ]; then
  echo ""
  echo "❌ 错误: 缺少必要文件，请检查"
  exit 1
fi
echo ""

# 步骤 2: 验证配置文件语法
echo "=========================================="
echo "  步骤 2/4: 验证 Docker Compose 配置"
echo "=========================================="
echo ""

echo "==> 验证 docker-compose.prod.yml 语法..."
cd "$BUSINESS_DIR"
if docker-compose -f docker-compose.prod.yml config > /dev/null; then
  echo "✅ Docker Compose 配置语法正确"
else
  echo "❌ Docker Compose 配置语法错误"
  exit 1
fi
echo ""

# 步骤 3: 本地测试启动（可选）
echo "=========================================="
echo "  步骤 3/4: 本地测试（可选）"
echo "=========================================="
echo ""

read -p "是否在本地测试启动 Business 基础设施? (y/N): " test_local
if [[ "$test_local" =~ ^[Yy]$ ]]; then
  echo ""
  echo "==> 创建测试环境变量..."
  if [ ! -f "$BUSINESS_DIR/.env" ]; then
    cp "$BUSINESS_DIR/.env.prod.example" "$BUSINESS_DIR/.env"
    echo "✅ 已创建 .env 文件"
  else
    echo "⚠️  .env 文件已存在，跳过创建"
  fi

  echo ""
  echo "==> 启动 Business 基础设施（本地测试）..."
  docker-compose -f docker-compose.prod.yml up -d

  echo ""
  echo "==> 等待服务启动..."
  sleep 10

  echo ""
  echo "==> 检查服务状态..."
  docker-compose -f docker-compose.prod.yml ps

  echo ""
  echo "==> 健康检查..."

  # PostgreSQL 健康检查
  echo -n "PostgreSQL... "
  if docker exec business-postgres pg_isready -U business > /dev/null 2>&1; then
    echo "✅ 健康"
  else
    echo "❌ 不健康"
  fi

  # MinIO 健康检查
  echo -n "MinIO... "
  if curl -sf http://localhost:9002/minio/health/live > /dev/null; then
    echo "✅ 健康"
  else
    echo "❌ 不健康"
  fi

  # PostGIS 扩展检查
  echo -n "PostGIS扩展... "
  if docker exec business-postgres psql -U business -d business -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='postgis';" 2>/dev/null | grep -q "1"; then
    echo "✅ 已安装"
  else
    echo "⚠️  未安装"
  fi

  echo ""
  echo "本地测试服务访问地址："
  echo "  - PostgreSQL: localhost:5433 (含 PostGIS 扩展)"
  echo "  - MinIO API: http://localhost:9002"
  echo "  - MinIO Console: http://localhost:9003"
  echo ""

  read -p "测试完成，是否停止服务? (Y/n): " stop_services
  if [[ ! "$stop_services" =~ ^[Nn]$ ]]; then
    echo "==> 停止服务..."
    docker-compose -f docker-compose.prod.yml down
    echo "✅ 服务已停止"
  fi
else
  echo "跳过本地测试"
fi
echo ""

# 步骤 4: 生成部署包
echo "=========================================="
echo "  步骤 4/4: 生成部署包"
echo "=========================================="
echo ""

DEPLOY_PACKAGE_DIR="$BUSINESS_DIR/deploy-package"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
PACKAGE_NAME="business-deploy-$TIMESTAMP.tar.gz"

echo "==> 创建部署包目录..."
rm -rf "$DEPLOY_PACKAGE_DIR"
mkdir -p "$DEPLOY_PACKAGE_DIR"

echo "==> 复制部署文件..."
cp "$BUSINESS_DIR/docker-compose.prod.yml" "$DEPLOY_PACKAGE_DIR/"
cp "$BUSINESS_DIR/.env.prod.example" "$DEPLOY_PACKAGE_DIR/.env.example"
cp "$BUSINESS_DIR/init-db.sql" "$DEPLOY_PACKAGE_DIR/" 2>/dev/null || echo "⚠️  init-db.sql 不存在，跳过"

# 复制部署脚本
if [ -d "$BUSINESS_DIR/scripts" ]; then
  cp -r "$BUSINESS_DIR/scripts" "$DEPLOY_PACKAGE_DIR/"
fi

echo "==> 生成部署说明文档..."
cat > "$DEPLOY_PACKAGE_DIR/DEPLOY.md" <<'EOF'
# Business 基础设施部署说明

## 快速部署

```bash
# 1. 解压部署包
tar -xzf business-deploy-*.tar.gz
cd business-deploy-*/

# 2. 配置环境变量
cp .env.example .env
vim .env
# 修改所有密码为生产环境强密码

# 3. 启动服务
docker-compose -f docker-compose.prod.yml up -d

# 4. 查看状态
docker-compose -f docker-compose.prod.yml ps
docker-compose -f docker-compose.prod.yml logs -f
```

## 服务访问

- PostgreSQL: `localhost:5433` (含 PostGIS 空间扩展)
- MinIO API: `http://localhost:9002`
- MinIO Console: `http://localhost:9003`

## 空间数据支持

Business PostgreSQL 已安装 PostGIS 扩展，支持：
- Shapefile 数据导入
- GeoJSON、WKT/WKB 格式
- 空间类型：POINT、LINESTRING、POLYGON、MULTIPOINT、MULTILINESTRING、MULTIPOLYGON

## 管理命令

```bash
# 查看日志
docker-compose -f docker-compose.prod.yml logs -f

# 重启服务
docker-compose -f docker-compose.prod.yml restart

# 停止服务
docker-compose -f docker-compose.prod.yml down

# 备份数据
docker exec business-postgres pg_dumpall -U business > backup.sql
```

## 注意事项

1. **必须先启动 Business 基础设施**，然后再启动 ADDP 系统
2. **生产环境务必修改所有密码**（.env 文件中）
3. **定期备份数据**（PostgreSQL 数据库和 MinIO 对象存储）
4. **端口配置**避免与 ADDP 系统冲突
EOF

echo "==> 打包部署文件..."
cd "$BUSINESS_DIR"
tar -czf "$PACKAGE_NAME" -C deploy-package .
mv "$PACKAGE_NAME" "$PROJECT_ROOT/"

echo "✅ 部署包已创建: $PROJECT_ROOT/$PACKAGE_NAME"
echo ""

# 清理临时目录
rm -rf "$DEPLOY_PACKAGE_DIR"

echo "=========================================="
echo "  ✅ 准备完成！"
echo "=========================================="
echo ""
echo "部署包信息："
echo "  - 文件: $PROJECT_ROOT/$PACKAGE_NAME"
echo "  - 大小: $(du -h "$PROJECT_ROOT/$PACKAGE_NAME" | cut -f1)"
echo ""
echo "下一步操作："
echo "  1. 传输部署包到服务器:"
echo "     scp $PACKAGE_NAME user@server-ip:/opt/"
echo ""
echo "  2. 在服务器上解压并部署:"
echo "     ssh user@server-ip"
echo "     cd /opt"
echo "     tar -xzf $PACKAGE_NAME"
echo "     cd business-deploy-*/"
echo "     cp .env.example .env"
echo "     vim .env  # 修改密码"
echo "     docker-compose -f docker-compose.prod.yml up -d"
echo ""
echo "  3. 或者使用自动化部署脚本:"
echo "     ./scripts/deploy-business.sh"
echo ""
echo "=========================================="
