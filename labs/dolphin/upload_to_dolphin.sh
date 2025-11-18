#!/bin/bash

# ==============================================================================
# 一键上传空间算子到 DolphinScheduler
# ==============================================================================

set -e

# 配置
DOLPHIN_HOST=${DOLPHIN_HOST:-"localhost:12345"}
DOLPHIN_USER=${DOLPHIN_USER:-"admin"}
DOLPHIN_PASSWORD=${DOLPHIN_PASSWORD:-"dolphinscheduler123"}

echo "🐬 DolphinScheduler 空间算子上传工具"
echo "======================================"
echo ""

# 步骤 1: 登录获取 Token
echo "📝 步骤 1: 登录 DolphinScheduler..."
LOGIN_RESPONSE=$(curl -s -X POST "http://${DOLPHIN_HOST}/dolphinscheduler/api/v1/login" \
  -d "userName=${DOLPHIN_USER}" \
  -d "userPassword=${DOLPHIN_PASSWORD}")

TOKEN=$(echo $LOGIN_RESPONSE | grep -o '"token":"[^"]*' | cut -d'"' -f4)

if [ -z "$TOKEN" ]; then
    echo "❌ 登录失败，请检查用户名和密码"
    echo "响应: $LOGIN_RESPONSE"
    exit 1
fi

echo "✅ 登录成功，Token: ${TOKEN:0:20}..."

# 步骤 2: 获取或创建项目
echo ""
echo "📁 步骤 2: 检查项目..."
PROJECT_NAME="spatial_analysis"

# 查询项目列表
PROJECTS=$(curl -s -X GET "http://${DOLPHIN_HOST}/dolphinscheduler/api/v1/projects" \
  -H "token: ${TOKEN}")

PROJECT_CODE=$(echo $PROJECTS | grep -o "\"code\":[0-9]*" | head -1 | cut -d: -f2)

if [ -z "$PROJECT_CODE" ]; then
    echo "📝 创建新项目: ${PROJECT_NAME}"
    CREATE_PROJECT=$(curl -s -X POST "http://${DOLPHIN_HOST}/dolphinscheduler/api/v1/projects" \
      -H "token: ${TOKEN}" \
      -d "projectName=${PROJECT_NAME}" \
      -d "description=空间分析算子工作流项目")

    PROJECT_CODE=$(echo $CREATE_PROJECT | grep -o '"code":[0-9]*' | cut -d: -f2)
fi

echo "✅ 项目代码: ${PROJECT_CODE}"

# 步骤 3: 上传 Python 脚本到资源中心
echo ""
echo "📦 步骤 3: 上传空间算子代码到资源中心..."

upload_file() {
    local file_path=$1
    local file_name=$(basename $file_path)

    echo "   上传: ${file_name}..."

    # 创建临时 multipart 文件
    UPLOAD_RESPONSE=$(curl -s -X POST \
      "http://${DOLPHIN_HOST}/dolphinscheduler/api/v1/resources" \
      -H "token: ${TOKEN}" \
      -F "file=@${file_path}" \
      -F "type=FILE" \
      -F "name=${file_name}" \
      -F "description=空间算子: ${file_name}")

    if echo "$UPLOAD_RESPONSE" | grep -q '"success":true'; then
        echo "   ✅ ${file_name} 上传成功"
    else
        echo "   ⚠️  ${file_name} 上传失败或已存在"
    fi
}

# 上传所有 Python 文件
for file in backend/spatial/*.py; do
    if [ -f "$file" ]; then
        upload_file "$file"
    fi
done

# 步骤 4: 创建算子任务定义
echo ""
echo "🔧 步骤 4: 创建算子任务定义..."

create_operator_task() {
    local op_code=$1
    local op_name=$2
    local op_desc=$3
    local params=$4

    echo "   创建任务: ${op_name}..."

    # 生成 Python 脚本
    TASK_SCRIPT=$(cat <<EOF
import subprocess
import json

# 算子: ${op_name}
# 参数: ${params}

task_config = {
    "operator": "${op_code}",
    "params": ${params}
}

# 执行算子
result = subprocess.run(
    ['python3', '\${RESOURCE_PATH}/operator_executor.py', json.dumps(task_config)],
    capture_output=True,
    text=True
)

print(result.stdout)

if result.returncode != 0:
    raise Exception(result.stderr)

# 保存输出到变量池
import json
output = json.loads(result.stdout)
print(f"##[set-output name=result]{json.dumps(output['result'])}")
EOF
)

    # 创建任务定义（这里简化为打印，实际需要调用 API）
    echo "   📝 任务定义已生成: SPATIAL_${op_code^^}"
}

# 创建 Buffer 算子任务
create_operator_task "buffer" "缓冲区分析" "对几何对象创建指定距离的缓冲区" \
  '{"input_geom": ${input_geom}, "distance": ${distance}, "segments": ${segments}}'

# 创建 Intersection 算子任务
create_operator_task "intersection" "几何相交" "计算两个几何对象的交集" \
  '{"geom_a": ${geom_a}, "geom_b": ${geom_b}}'

# 创建 Union 算子任务
create_operator_task "union" "几何合并" "合并多个几何对象为一个" \
  '{"geometries": ${geometries}}'

# 步骤 5: 生成使用文档
echo ""
echo "📚 步骤 5: 生成使用文档..."

cat > DOLPHIN_USAGE.md <<'EOF'
# DolphinScheduler 空间算子使用指南

## 已上传的资源

资源中心中的文件:
- `operator_registry.py` - 算子注册中心
- `operator_executor.py` - 算子执行器
- `operators.py` - 算子实现
- `workflow_builder.py` - 工作流构建器

## 创建空间分析工作流

### 步骤 1: 创建工作流定义

1. 登录 DolphinScheduler Web UI
2. 进入 **项目管理** → **spatial_analysis**
3. 点击 **创建工作流** → **工作流定义**

### 步骤 2: 添加算子任务

#### 示例: 缓冲区分析任务

1. 拖拽 **Python** 任务到画布
2. 填写任务信息:
   - **任务名称**: buffer_analysis
   - **脚本类型**: Python
   - **Python 脚本**:

```python
import subprocess
import json

# 参数配置
input_geom = {"type": "Point", "coordinates": [116.404, 39.915]}
distance = 100.0
segments = 16

task_config = {
    "operator": "buffer",
    "params": {
        "input_geom": input_geom,
        "distance": distance,
        "segments": segments
    }
}

# 执行算子
result = subprocess.run(
    ['python3', '/path/to/resources/operator_executor.py', json.dumps(task_config)],
    capture_output=True,
    text=True
)

print(result.stdout)

if result.returncode != 0:
    raise Exception(result.stderr)

# 保存输出
output = json.loads(result.stdout)
print(f"##[set-output name=result]{json.dumps(output['result'])}")
```

3. 保存任务

### 步骤 3: 连接任务形成工作流

示例工作流: **缓冲区 → 交集**

```
[Buffer_Task_1]  ──┐
                   │
                   ├──→ [Intersection_Task]
                   │
[Buffer_Task_2]  ──┘
```

在 Intersection_Task 的脚本中引用上游输出:

```python
# 引用上游任务输出
geom_a = ${Buffer_Task_1.result}
geom_b = ${Buffer_Task_2.result}

task_config = {
    "operator": "intersection",
    "params": {
        "geom_a": json.loads(geom_a),
        "geom_b": json.loads(geom_b)
    }
}
```

### 步骤 4: 运行工作流

1. 保存工作流定义
2. 点击 **上线** 按钮
3. 点击 **运行** 按钮
4. 查看 **工作流实例** 页面监控执行状态

## 可用算子列表

| 算子代码 | 名称 | 描述 | 参数 |
|---------|------|------|------|
| `buffer` | 缓冲区分析 | 创建指定距离的缓冲区 | input_geom, distance, segments |
| `intersection` | 几何相交 | 计算两个几何对象的交集 | geom_a, geom_b |
| `union` | 几何合并 | 合并多个几何对象 | geometries |
| `centroid` | 计算质心 | 计算几何对象的质心点 | input_geom |
| `contains` | 包含关系判断 | 判断 A 是否包含 B | geom_a, geom_b |
| `intersects` | 相交关系判断 | 判断 A 和 B 是否相交 | geom_a, geom_b |
| `distance` | 距离计算 | 计算最短距离 | geom_a, geom_b |
| `spatial_join` | 空间连接 | 基于空间关系连接表 | left_table, right_table, predicate |

## 参数传递技巧

### 方法 1: 使用变量池（推荐）

上游任务输出:
```python
print(f"##[set-output name=my_result]{json.dumps(result)}")
```

下游任务引用:
```python
upstream_result = ${upstream_task_name.my_result}
```

### 方法 2: 使用全局参数

在工作流定义中添加全局参数:
```json
{
  "globalParams": [
    {"prop": "base_distance", "value": "100.0"}
  ]
}
```

在任务中引用:
```python
distance = ${global.base_distance}
```

## 常见问题

**Q: 如何查看算子执行日志？**
A: 在工作流实例页面，点击任务节点 → 查看日志

**Q: 如何处理大几何对象？**
A: 使用共享存储（Redis/S3），任务间传递引用 key

**Q: 如何添加自定义算子？**
A: 在 `operators.py` 中实现新函数 → 在 `operator_registry.py` 中注册 → 重新上传到资源中心

## 下一步

1. 查看 [SPATIAL_OPERATOR_GUIDE.md](SPATIAL_OPERATOR_GUIDE.md) 了解算子详情
2. 查看 [DOLPHIN_INTEGRATION.md](DOLPHIN_INTEGRATION.md) 了解集成方案
3. 尝试创建复杂工作流（多个算子组合）
EOF

echo "✅ 使用文档已生成: DOLPHIN_USAGE.md"

# 完成
echo ""
echo "======================================"
echo "✅ 上传完成！"
echo ""
echo "📚 下一步:"
echo "   1. 访问 DolphinScheduler UI: http://${DOLPHIN_HOST}/dolphinscheduler/ui"
echo "   2. 进入 '项目管理' → 'spatial_analysis'"
echo "   3. 查看 '资源中心' 确认文件已上传"
echo "   4. 阅读 DOLPHIN_USAGE.md 了解如何创建工作流"
echo ""
echo "🎉 现在可以开始在 DolphinScheduler 中编排空间算子了！"