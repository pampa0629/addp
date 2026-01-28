#!/bin/bash
set -e

echo "=== Doris 集群自动初始化 ==="

# 容器名和连接参数，支持环境变量自定义
DORIS_FE_HOST=${DORIS_FE_HOST:-business-doris-fe}
DORIS_FE_PORT=${DORIS_FE_PORT:-9030}
DORIS_BE_HOST=${DORIS_BE_HOST:-business-doris-be}
DORIS_BE_PORT=${DORIS_BE_PORT:-9050}

# 等待 FE 完全启动
echo "等待 Doris FE 就绪 (主机: $DORIS_FE_HOST:$DORIS_FE_PORT)..."
MAX_RETRIES=30
RETRY_COUNT=0

until mysql -h "$DORIS_FE_HOST" -P "$DORIS_FE_PORT" -uroot --connect-timeout=5 -e "SHOW FRONTENDS;" 2>/dev/null; do
  RETRY_COUNT=$((RETRY_COUNT + 1))
  if [ $RETRY_COUNT -ge $MAX_RETRIES ]; then
    echo "❌ FE 启动超时，请检查日志"
    exit 1
  fi
  echo "FE 尚未就绪 ($RETRY_COUNT/$MAX_RETRIES)，等待 5 秒..."
  sleep 5
done

echo "✅ Doris FE 已就绪"

# 等待 BE 容器启动 (给 BE 一些启动时间)
echo "等待 BE 容器启动..."
sleep 10

# 检查 BE 是否已经添加到集群
EXISTING_BE=$(mysql -h "$DORIS_FE_HOST" -P "$DORIS_FE_PORT" -uroot -e "SHOW BACKENDS;" 2>/dev/null | grep -c "$DORIS_BE_HOST" || echo "0")

if [ "$EXISTING_BE" -gt 0 ]; then
  echo "✅ BE 节点已存在于集群中，跳过添加"
else
  echo "添加 BE 节点到集群..."
  mysql -h "$DORIS_FE_HOST" -P "$DORIS_FE_PORT" -uroot <<EOF
ALTER SYSTEM ADD BACKEND "$DORIS_BE_HOST:$DORIS_BE_PORT";
EOF
  echo "✅ BE 节点添加成功"
fi

# 验证 BE 状态
echo "验证 BE 状态 (可能需要几秒才会显示为 Alive)..."
mysql -h "$DORIS_FE_HOST" -P "$DORIS_FE_PORT" -uroot -e "SHOW BACKENDS\G"

# 创建测试数据库
echo "创建测试数据库..."
mysql -h "$DORIS_FE_HOST" -P "$DORIS_FE_PORT" -uroot <<EOF
CREATE DATABASE IF NOT EXISTS test_db
COMMENT '测试数据库（用于验证 ADDP 集成）';
EOF

echo "✅ Doris 集群初始化完成！"
echo ""
echo "连接信息:"
echo "  Host: $DORIS_FE_HOST"
echo "  Port: $DORIS_FE_PORT"
echo "  User: root"
echo "  Password: (空)"
echo ""
echo "验证集群状态:"
echo "  docker exec business-doris-fe mysql -h$DORIS_FE_HOST -P$DORIS_FE_PORT -uroot -e 'SHOW BACKENDS;'"
