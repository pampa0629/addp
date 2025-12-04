#!/bin/bash

# 快速部署脚本：高性能 Spatialite → PostgreSQL 导入

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# 1. 检查环境
log "Step 1/5: 检查环境..."

if ! command -v docker &> /dev/null; then
    error "Docker 未安装，请先安装 Docker"
fi

if ! command -v curl &> /dev/null; then
    error "curl 未安装"
fi

log "✓ 环境检查通过"

# 2. 启动业务基础设施
log "Step 2/5: 启动 PostgreSQL/PostGIS (business infrastructure)..."

cd /Users/pampa/code/addp/business

if [ ! -f .env ]; then
    warn ".env 文件不存在，从模板复制"
    cp .env.example .env 2>/dev/null || true
fi

docker-compose up -d

log "等待 PostgreSQL 启动..."
sleep 10

# 检查 PostgreSQL 健康状态
for i in {1..30}; do
    if docker-compose exec -T postgres pg_isready -U business &> /dev/null; then
        log "✓ PostgreSQL 已就绪"
        break
    fi
    if [ $i -eq 30 ]; then
        error "PostgreSQL 启动超时"
    fi
    sleep 2
done

# 3. 优化 PostgreSQL 配置（导入前）
log "Step 3/5: 优化 PostgreSQL 配置..."

cat > /tmp/pg_optimize.sql <<'EOF'
-- 临时优化配置（仅导入时使用）
ALTER SYSTEM SET synchronous_commit = 'off';
ALTER SYSTEM SET fsync = 'off';
ALTER SYSTEM SET full_page_writes = 'off';
ALTER SYSTEM SET checkpoint_timeout = '30min';
ALTER SYSTEM SET max_wal_size = '10GB';
ALTER SYSTEM SET shared_buffers = '2GB';
ALTER SYSTEM SET work_mem = '256MB';
ALTER SYSTEM SET maintenance_work_mem = '1GB';
SELECT pg_reload_conf();

-- 创建空间数据库 schema
CREATE SCHEMA IF NOT EXISTS spatial;
EOF

docker-compose exec -T postgres psql -U business -d business < /tmp/pg_optimize.sql

log "✓ PostgreSQL 配置已优化"

# 4. 启动 Transfer 模块
log "Step 4/5: 启动 Transfer 模块..."

cd /Users/pampa/code/addp

# 启动 Transfer backend
if ! pgrep -f "transfer/backend" > /dev/null; then
    log "启动 Transfer backend..."
    cd transfer/backend
    go run cmd/server/main.go > /tmp/transfer-backend.log 2>&1 &
    TRANSFER_PID=$!
    echo $TRANSFER_PID > /tmp/transfer-backend.pid
    cd ../..

    log "等待 Transfer 启动..."
    sleep 5

    # 检查健康状态
    for i in {1..20}; do
        if curl -s -f http://localhost:8083/health > /dev/null 2>&1; then
            log "✓ Transfer backend 已就绪"
            break
        fi
        if [ $i -eq 20 ]; then
            error "Transfer backend 启动失败，查看日志: tail -f /tmp/transfer-backend.log"
        fi
        sleep 2
    done
else
    log "✓ Transfer backend 已运行"
fi

# 5. 显示使用说明
log "Step 5/5: 部署完成！"

cat <<'EOF'

========================================
🚀 高性能数据导入环境已就绪
========================================

📍 服务地址:
  - Transfer API: http://localhost:8083
  - PostgreSQL: localhost:5433 (user: business, db: business)
  - MinIO Console: http://localhost:9001

📖 快速使用指南:

1️⃣ 创建导入任务（以 1000 万条 POI 数据为例）:

cat > task.json <<'JSON'
{
  "name": "Import 10M POI Data",
  "source_config": {
    "type": "spatialite_parallel",
    "config": {
      "file_path": "/path/to/your/data.sqlite",
      "table": "poi",
      "num_workers": 8,
      "partition_size": 200000,
      "batch_size": 5000,
      "geometry_fields": ["geom"]
    },
    "batch_size": 5000
  },
  "target_config": {
    "type": "postgres_copy",
    "config": {
      "host": "localhost",
      "port": 5433,
      "database": "business",
      "username": "business",
      "password": "business_password",
      "table": "spatial.poi",
      "batch_size": 10000,
      "max_connections": 8,
      "create_table": true,
      "srid": 4326,
      "geometry_columns": ["geom"]
    },
    "batch_size": 10000
  },
  "execution_mode": "parallel"
}
JSON

# 提交任务
TASK_ID=$(curl -s -X POST http://localhost:8083/api/tasks \
  -H "Content-Type: application/json" \
  -d @task.json | jq -r '.id')

echo "✓ 任务已创建，ID: ${TASK_ID}"

2️⃣ 启动执行:

EXEC_ID=$(curl -s -X POST http://localhost:8083/api/tasks/${TASK_ID}/execute | jq -r '.id')
echo "✓ 执行已启动，ID: ${EXEC_ID}"

3️⃣ 监控进度:

watch -n 2 "curl -s http://localhost:8083/api/executions/${EXEC_ID} | jq '{status, progress, records_read, records_written, throughput}'"

4️⃣ 导入完成后恢复 PostgreSQL 配置:

docker-compose -f business/docker-compose.yml exec postgres psql -U business -d business <<'SQL'
ALTER SYSTEM RESET ALL;
SELECT pg_reload_conf();
VACUUM ANALYZE spatial.poi;
CREATE INDEX idx_poi_geom ON spatial.poi USING GIST(geom);
SQL

📊 性能预期（8核并行 + COPY）:
  - 1000万条: ~1.5分钟
  - 5000万条: ~8分钟
  - 吞吐量: 80,000-120,000 条/秒

📚 更多文档:
  - 详细指南: transfer/backend/docs/HIGH_PERFORMANCE_IMPORT.md
  - 配置模板: transfer/backend/docs/config_templates.json
  - 架构图: transfer/backend/docs/ARCHITECTURE_DIAGRAMS.md

🧪 性能测试:
  cd transfer/backend/scripts
  ./benchmark.sh /path/to/test.sqlite localhost

⚠️  导入完成后，记得恢复 PostgreSQL 配置（fsync、autovacuum 等）

========================================

EOF

log "部署完成！开始你的高性能数据导入之旅 🚀"
