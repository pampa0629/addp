#!/bin/bash
# 查看 DolphinScheduler 工作流失败日志的脚本

echo "=== DolphinScheduler 日志查询工具 ==="
echo ""

# 1. 登录获取 token
echo "[1/3] 正在登录..."
TOKEN_RESPONSE=$(curl -s -X POST 'http://localhost:12345/dolphinscheduler/login' \
  -d 'userName=admin&userPassword=dolphinscheduler123')

TOKEN=$(echo $TOKEN_RESPONSE | grep -o '"data":"[^"]*"' | cut -d'"' -f4)

if [ -z "$TOKEN" ]; then
    echo "❌ 登录失败！"
    echo "响应: $TOKEN_RESPONSE"
    exit 1
fi

echo "✓ 登录成功，token: ${TOKEN:0:20}..."

# 2. 获取项目列表
echo ""
echo "[2/3] 获取项目列表..."
PROJECTS=$(curl -s -X GET 'http://localhost:12345/dolphinscheduler/projects/list' \
  -H "token: $TOKEN")

echo "项目列表:"
echo $PROJECTS | jq -r '.data[] | "  - \(.name) (code: \(.code))"' 2>/dev/null || echo "$PROJECTS"

PROJECT_CODE=$(echo $PROJECTS | jq -r '.data[0].code' 2>/dev/null)
echo ""
echo "使用项目 code: $PROJECT_CODE"

# 3. 获取工作流实例列表
echo ""
echo "[3/3] 获取工作流实例..."
INSTANCES=$(curl -s -X GET "http://localhost:12345/dolphinscheduler/projects/${PROJECT_CODE}/process-instances?pageNo=1&pageSize=10" \
  -H "token: $TOKEN")

echo "工作流实例列表:"
echo "$INSTANCES" | jq -r '.data.totalList[] | "  - ID: \(.id), Name: \(.name), State: \(.state)"' 2>/dev/null || echo "$INSTANCES"

# 获取失败的工作流实例 ID
FAILED_INSTANCE_ID=$(echo "$INSTANCES" | jq -r '.data.totalList[] | select(.state == "FAILURE") | .id' | head -1)

if [ -n "$FAILED_INSTANCE_ID" ]; then
    echo ""
    echo "=== 失败工作流详情 ==="
    echo "实例 ID: $FAILED_INSTANCE_ID"

    # 获取任务实例
    echo ""
    echo "任务列表:"
    TASKS=$(curl -s -X GET "http://localhost:12345/dolphinscheduler/projects/${PROJECT_CODE}/process-instances/${FAILED_INSTANCE_ID}/tasks" \
      -H "token: $TOKEN")

    echo "$TASKS" | jq -r '.data[] | "  - Task ID: \(.id), Name: \(.name), State: \(.state)"' 2>/dev/null || echo "$TASKS"

    # 获取第一个失败任务的 ID
    TASK_ID=$(echo "$TASKS" | jq -r '.data[] | select(.state == "FAILURE") | .id' | head -1)

    if [ -n "$TASK_ID" ]; then
        echo ""
        echo "=== 任务执行日志 (Task ID: $TASK_ID) ==="
        LOG=$(curl -s -X GET "http://localhost:12345/dolphinscheduler/log/detail?taskInstanceId=${TASK_ID}" \
          -H "token: $TOKEN")

        echo "$LOG" | jq -r '.data.message' 2>/dev/null || echo "$LOG"
    fi
else
    echo ""
    echo "⚠️  没有找到失败的工作流实例"
fi

echo ""
echo "=== 完成 ==="
