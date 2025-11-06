#!/bin/bash
# 测试 PostgresCOPYWriter 的 write_mode 功能
# 验证 replace 模式可以防止重复写入

set -e

echo "======================================"
echo "测试 PostgresCOPYWriter write_mode 功能"
echo "======================================"

# 配置
TRANSFER_API="http://localhost:8083/api"
POSTGRES_HOST="localhost"
POSTGRES_PORT="5432"
POSTGRES_USER="addp"
POSTGRES_DB="addp"
TEST_TABLE="test_write_mode_$(date +%s)"

echo ""
echo "1. 准备测试数据..."
echo "   创建测试表: $TEST_TABLE"

# 创建测试任务配置
TASK_CONFIG=$(cat <<EOF
{
  "name": "Test Write Mode - Replace",
  "description": "测试 replace 模式防止重复写入",
  "type": "import",
  "mode": "batch",
  "config": {
    "source": {
      "type": "jdbc",
      "driver": "postgresql",
      "host": "$POSTGRES_HOST",
      "port": $POSTGRES_PORT,
      "database": "$POSTGRES_DB",
      "username": "$POSTGRES_USER",
      "password": "addp_password",
      "query": "SELECT generate_series(1, 1000) AS id, 'test_' || generate_series(1, 1000) AS name"
    },
    "target": {
      "type": "postgres_copy",
      "host": "$POSTGRES_HOST",
      "port": $POSTGRES_PORT,
      "database": "$POSTGRES_DB",
      "username": "$POSTGRES_USER",
      "password": "addp_password",
      "table": "$TEST_TABLE",
      "write_mode": "replace",
      "create_table": true
    }
  }
}
EOF
)

echo ""
echo "2. 创建任务..."
TASK_ID=$(curl -s -X POST "$TRANSFER_API/tasks" \
  -H "Content-Type: application/json" \
  -d "$TASK_CONFIG" | jq -r '.id')

if [ -z "$TASK_ID" ] || [ "$TASK_ID" == "null" ]; then
  echo "❌ 创建任务失败"
  exit 1
fi

echo "✅ 任务创建成功，ID: $TASK_ID"

echo ""
echo "3. 第一次执行任务..."
curl -s -X POST "$TRANSFER_API/tasks/$TASK_ID/start" > /dev/null
sleep 5  # 等待任务完成

# 查询记录数
COUNT1=$(psql -h $POSTGRES_HOST -p $POSTGRES_PORT -U $POSTGRES_USER -d $POSTGRES_DB -tAc "SELECT COUNT(*) FROM $TEST_TABLE" 2>/dev/null || echo "0")
echo "   第一次执行后记录数: $COUNT1"

if [ "$COUNT1" != "1000" ]; then
  echo "❌ 第一次执行失败，预期 1000 条记录，实际 $COUNT1 条"
  exit 1
fi

echo "✅ 第一次执行成功"

echo ""
echo "4. 第二次执行任务（测试 replace 模式）..."
curl -s -X POST "$TRANSFER_API/tasks/$TASK_ID/start" > /dev/null
sleep 5  # 等待任务完成

# 查询记录数
COUNT2=$(psql -h $POSTGRES_HOST -p $POSTGRES_PORT -U $POSTGRES_USER -d $POSTGRES_DB -tAc "SELECT COUNT(*) FROM $TEST_TABLE" 2>/dev/null || echo "0")
echo "   第二次执行后记录数: $COUNT2"

if [ "$COUNT2" != "1000" ]; then
  echo "❌ replace 模式失败！数据重复了，预期 1000 条记录，实际 $COUNT2 条"
  exit 1
fi

echo "✅ 第二次执行成功，数据没有重复"

echo ""
echo "5. 清理测试数据..."
psql -h $POSTGRES_HOST -p $POSTGRES_PORT -U $POSTGRES_USER -d $POSTGRES_DB -c "DROP TABLE IF EXISTS $TEST_TABLE" > /dev/null 2>&1
curl -s -X DELETE "$TRANSFER_API/tasks/$TASK_ID" > /dev/null

echo "✅ 清理完成"

echo ""
echo "======================================"
echo "✅ 所有测试通过！"
echo "======================================"
echo ""
echo "测试结果："
echo "  - replace 模式正常工作"
echo "  - 多次执行不会重复写入数据"
echo "  - PostgresCOPYWriter write_mode 功能验证通过"
