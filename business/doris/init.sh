#!/bin/bash
set -e

echo "=== Doris 集群自动初始化 ==="

# 等待 FE 完全启动
echo "等待 Doris FE 就绪..."
MAX_RETRIES=30
RETRY_COUNT=0

until mysql -h business-doris-fe -P 9030 -uroot --connect-timeout=5 -e "SHOW FRONTENDS;" 2>/dev/null; do
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
EXISTING_BE=$(mysql -h business-doris-fe -P 9030 -uroot -e "SHOW BACKENDS;" 2>/dev/null | grep -c "business-doris-be" || echo "0")

if [ "$EXISTING_BE" -gt 0 ]; then
  echo "✅ BE 节点已存在于集群中，跳过添加"
else
  echo "添加 BE 节点到集群..."
  mysql -h business-doris-fe -P 9030 -uroot <<EOF
ALTER SYSTEM ADD BACKEND "business-doris-be:9050";
EOF
  echo "✅ BE 节点添加成功"
fi

# 验证 BE 状态
echo "验证 BE 状态 (可能需要几秒才会显示为 Alive)..."
mysql -h business-doris-fe -P 9030 -uroot -e "SHOW BACKENDS\G"

# 创建测试数据库
echo "创建测试数据库..."
mysql -h business-doris-fe -P 9030 -uroot <<EOF
CREATE DATABASE IF NOT EXISTS test_db
COMMENT '测试数据库（用于验证 ADDP 集成）';
EOF

echo "✅ Doris 集群初始化完成！"
echo ""
echo "连接信息:"
echo "  Host: localhost"
echo "  Port: 9030"
echo "  User: root"
echo "  Password: (空)"
echo ""
echo "验证集群状态:"
echo "  docker exec business-doris-fe mysql -h127.0.0.1 -P9030 -uroot -e 'SHOW BACKENDS;'"
