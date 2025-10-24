#!/bin/bash

# 获取token
TOKEN=$(curl -s -X POST 'http://localhost:8080/api/auth/login' \
  -H 'Content-Type: application/json' \
  -d '{"username":"zuhu1","password":"xx123zzm"}' | \
  python3 -c "import sys, json; print(json.load(sys.stdin)['access_token'])")

echo "Token: ${TOKEN:0:20}..."

# 启动任务
echo "Starting task 11..."
RESPONSE=$(curl -s -X POST "http://localhost:8083/api/tasks/11/start" \
  -H "Authorization: Bearer ${TOKEN}")

echo "Response: $RESPONSE"

# 提取execution ID
EXEC_ID=$(echo $RESPONSE | python3 -c "import sys, json; print(json.load(sys.stdin).get('id', 'unknown'))")
echo "Execution ID: $EXEC_ID"

# 等待执行
echo "Waiting 10 seconds for execution..."
sleep 10

# 检查状态
echo "Checking execution status..."
PGPASSWORD=addp_password psql -h localhost -U addp -d addp -c \
  "SELECT id, status, records_written, error_msg FROM transfer.task_executions WHERE id = $EXEC_ID;"
