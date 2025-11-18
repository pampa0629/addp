# 空间算子集成到 DolphinScheduler 的完整方案

## 目标
让空间算子直接在 DolphinScheduler Web UI 中可见，用户通过拖拽编排工作流。

## 集成方式

### 方式 1: Python 任务类型（最简单，推荐） ⭐

#### 步骤 1: 在 DolphinScheduler 中创建资源文件

将空间算子相关代码上传到 DolphinScheduler 的**资源中心**:

```
资源中心/
├── spatial/
│   ├── operator_registry.py
│   ├── operator_executor.py
│   ├── operators.py
│   └── __init__.py
```

上传方式：
1. 登录 DolphinScheduler Web UI
2. 进入 **资源中心 → 文件管理**
3. 创建 `spatial` 文件夹
4. 上传所有 Python 文件

#### 步骤 2: 创建任务模板（每个算子一个模板）

**示例: 缓冲区算子任务模板**

在 DolphinScheduler 中创建 **Python 任务**:

```python
# 任务名称: SPATIAL_BUFFER
# 任务描述: 空间缓冲区分析算子
# 任务类型: Python

import subprocess
import json
import sys

# 任务参数（用户在 UI 中填写）
input_geom = ${input_geom}         # GeoJSON 字符串
distance = ${distance}              # 缓冲距离
segments = ${segments}              # 圆弧段数（默认 8）

# 构造算子配置
task_config = {
    "operator": "buffer",
    "params": {
        "input_geom": json.loads(input_geom),
        "distance": float(distance),
        "segments": int(segments) if segments else 8
    }
}

# 执行算子
result = subprocess.run(
    [
        'python3',
        '/path/to/resources/spatial/operator_executor.py',
        json.dumps(task_config)
    ],
    capture_output=True,
    text=True
)

# 输出结果
print(result.stdout)

# 将结果保存到共享变量（供下游任务使用）
output = json.loads(result.stdout)
print(f"##[set-output name=result]{json.dumps(output['result'])}")

if result.returncode != 0:
    raise Exception(f"算子执行失败: {result.stderr}")
```

**参数定义**（在任务配置中添加）:
```json
{
  "localParams": [
    {
      "prop": "input_geom",
      "direct": "IN",
      "type": "VARCHAR",
      "value": ""
    },
    {
      "prop": "distance",
      "direct": "IN",
      "type": "DOUBLE",
      "value": "100.0"
    },
    {
      "prop": "segments",
      "direct": "IN",
      "type": "INTEGER",
      "value": "8"
    }
  ]
}
```

#### 步骤 3: 用户使用流程

1. **创建工作流**: 进入 **项目管理 → 工作流定义 → 创建工作流**
2. **拖拽任务**: 从左侧任务列表拖拽 `SPATIAL_BUFFER` 到画布
3. **配置参数**: 填写 `input_geom`, `distance`, `segments`
4. **连接任务**: 拖拽连线定义依赖关系
5. **运行工作流**: 点击 **运行** 按钮

**示例工作流（缓冲区 → 交集）**:

```
[SPATIAL_BUFFER_1]  ─┐
  input_geom: {...}  │
  distance: 100      │
                     ├─→ [SPATIAL_INTERSECTION]
[SPATIAL_BUFFER_2]  │    geom_a: ${SPATIAL_BUFFER_1.result}
  input_geom: {...}  │    geom_b: ${SPATIAL_BUFFER_2.result}
  distance: 50       ┘
```

---

### 方式 2: 自定义任务插件（更灵活，需要 Java 开发）

#### 优势
- 💎 完全自定义 UI 表单（参数输入更友好）
- 💎 可以添加地图组件选择几何对象
- 💎 自动参数验证

#### 开发步骤

1. **创建 DolphinScheduler 插件项目**

```java
// SpatialBufferTask.java
package org.apache.dolphinscheduler.plugin.task.spatial;

@TaskType(value = "SPATIAL_BUFFER")
public class SpatialBufferTask extends AbstractTask {

    private SpatialBufferParameters parameters;

    @Override
    public void handle(TaskExecutionContext context) throws TaskException {
        // 1. 获取参数
        String inputGeom = parameters.getInputGeom();
        double distance = parameters.getDistance();

        // 2. 调用 Python 算子执行器
        ProcessBuilder pb = new ProcessBuilder(
            "python3",
            "/path/to/operator_executor.py",
            buildTaskConfig(inputGeom, distance)
        );

        // 3. 执行并捕获输出
        Process process = pb.start();
        String output = readOutput(process);

        // 4. 设置输出变量
        context.setVarPool(buildVarPool(output));
    }
}
```

2. **创建参数类**

```java
// SpatialBufferParameters.java
public class SpatialBufferParameters extends AbstractParameters {

    @FormField(label = "输入几何对象", type = FormFieldType.TEXT_AREA)
    private String inputGeom;

    @FormField(label = "缓冲距离（米）", type = FormFieldType.NUMBER)
    private double distance;

    @FormField(label = "圆弧段数", type = FormFieldType.NUMBER, defaultValue = "8")
    private int segments;

    // Getters and setters...
}
```

3. **创建前端表单组件**

```vue
<!-- SpatialBufferTaskForm.vue -->
<template>
  <el-form :model="form">
    <el-form-item label="输入几何对象">
      <el-input
        type="textarea"
        v-model="form.inputGeom"
        placeholder='{"type": "Point", "coordinates": [116.404, 39.915]}'
      />
      <!-- 可选: 添加地图选择器 -->
      <el-button @click="openMapPicker">在地图上选择</el-button>
    </el-form-item>

    <el-form-item label="缓冲距离（米）">
      <el-input-number v-model="form.distance" :min="0" />
    </el-form-item>

    <el-form-item label="圆弧段数">
      <el-input-number v-model="form.segments" :min="4" :max="32" />
    </el-form-item>
  </el-form>
</template>
```

4. **编译插件并安装**

```bash
# 编译插件
mvn clean package

# 复制到 DolphinScheduler 插件目录
cp target/dolphinscheduler-task-spatial-*.jar \
   $DOLPHINSCHEDULER_HOME/libs/

# 重启 DolphinScheduler
sh $DOLPHINSCHEDULER_HOME/bin/dolphinscheduler-daemon.sh restart api-server
```

5. **用户界面效果**

用户在创建任务时:
1. 选择任务类型 → **空间分析** → **缓冲区分析**
2. 看到定制化的参数表单（带地图选择器）
3. 填写参数后保存
4. 拖拽连线定义工作流

---

## 方案选择建议

### 快速原型阶段（1-2 周内）
✅ **使用方式 1（Python 任务类型）**
- 无需 Java 开发
- 快速验证算子逻辑
- 用户可以立即使用

### 生产环境（长期维护）
✅ **升级到方式 2（自定义插件）**
- 更好的用户体验
- 参数验证和错误提示
- 可集成地图可视化

---

## 完整示例: 在 DolphinScheduler 中创建 8 个空间算子任务

### 自动化注册脚本

创建一个脚本自动在 DolphinScheduler 中注册所有算子:

```python
# scripts/register_operators_to_dolphin.py
import requests
import json
from spatial.operator_registry import registry

DOLPHIN_API_BASE = "http://localhost:12345/dolphinscheduler"
TOKEN = "your-api-token"

def create_task_definition(operator):
    """为每个算子创建 DolphinScheduler 任务定义"""

    # 生成 Python 脚本
    script = f"""
import subprocess
import json

# 参数（由 DolphinScheduler 传入）
params = {{
    {', '.join([f'"{p.name}": ${{{p.name}}}' for p in operator.input_params])}
}}

task_config = {{
    "operator": "{operator.code}",
    "params": params
}}

# 执行算子
result = subprocess.run(
    ['python3', '/path/to/operator_executor.py', json.dumps(task_config)],
    capture_output=True,
    text=True
)

print(result.stdout)
if result.returncode != 0:
    raise Exception(result.stderr)
"""

    # 生成参数定义
    local_params = [
        {
            "prop": param.name,
            "direct": "IN",
            "type": param_type_to_dolphin(param.type),
            "value": param.default or ""
        }
        for param in operator.input_params
    ]

    # 创建任务定义
    task_def = {
        "name": f"SPATIAL_{operator.code.upper()}",
        "description": f"{operator.name} - {operator.description}",
        "taskType": "PYTHON",
        "taskParams": {
            "rawScript": script,
            "localParams": local_params
        }
    }

    # 提交到 DolphinScheduler
    response = requests.post(
        f"{DOLPHIN_API_BASE}/projects/1/task-definition",
        headers={"token": TOKEN},
        json=task_def
    )

    print(f"✅ 注册算子 {operator.name}: {response.status_code}")


def param_type_to_dolphin(param_type):
    """参数类型映射"""
    mapping = {
        "float": "DOUBLE",
        "int": "INTEGER",
        "string": "VARCHAR",
        "geojson": "VARCHAR",
        "wkt": "VARCHAR"
    }
    return mapping.get(param_type.value, "VARCHAR")


if __name__ == "__main__":
    print("🚀 开始注册空间算子到 DolphinScheduler...")

    for op in registry.list_all():
        create_task_definition(op)

    print("✅ 注册完成！")
```

运行脚本:
```bash
python3 scripts/register_operators_to_dolphin.py
```

---

## 参数传递机制（关键！）

### 问题: 如何将上游任务输出传递给下游任务？

DolphinScheduler 支持两种方式:

#### 方式 A: 使用变量池（推荐）

**上游任务（SPATIAL_BUFFER_1）**:
```python
# 在输出中添加特殊标记
output = json.loads(result.stdout)
print(f"##[set-output name=result_geom]{json.dumps(output['result'])}")
```

**下游任务（SPATIAL_INTERSECTION）**:
```python
# 引用上游变量
geom_a = ${SPATIAL_BUFFER_1.result_geom}
geom_b = ${SPATIAL_BUFFER_2.result_geom}
```

#### 方式 B: 使用共享存储（适合大数据）

```python
# 上游任务: 保存到 Redis
import redis
r = redis.Redis(host='localhost')
r.set(f'task_output_{task_id}', json.dumps(result))

# 下游任务: 从 Redis 读取
upstream_result = json.loads(r.get(f'task_output_{upstream_task_id}'))
```

---

## 最终效果演示

用户在 DolphinScheduler Web UI 中:

1. **看到算子列表**:
   ```
   Python 任务
   ├── SPATIAL_BUFFER (缓冲区分析)
   ├── SPATIAL_INTERSECTION (几何相交)
   ├── SPATIAL_UNION (几何合并)
   ├── SPATIAL_CENTROID (计算质心)
   ├── SPATIAL_CONTAINS (包含关系判断)
   ├── SPATIAL_INTERSECTS (相交关系判断)
   ├── SPATIAL_DISTANCE (距离计算)
   └── SPATIAL_JOIN (空间连接)
   ```

2. **拖拽编排工作流**:
   - 从左侧拖拽 `SPATIAL_BUFFER` 到画布
   - 填写参数（input_geom, distance）
   - 连接到下一个算子
   - 保存并运行

3. **查看执行结果**:
   - 实时日志显示算子执行过程
   - 输出结果可视化（如果集成地图组件）
   - 失败自动重试

---

## 下一步行动计划

### 立即可做（1-2 小时）
1. ✅ 上传 Python 文件到 DolphinScheduler 资源中心
2. ✅ 手动创建 2-3 个算子任务模板
3. ✅ 测试参数传递机制

### 短期优化（1-2 天）
1. ⬜ 编写自动注册脚本
2. ⬜ 完善参数验证和错误处理
3. ⬜ 添加数据源集成（连接 PostgreSQL/MinIO）

### 长期规划（1-2 周）
1. ⬜ 开发 DolphinScheduler 自定义插件
2. ⬜ 集成地图组件（参数选择更直观）
3. ⬜ 开发结果可视化面板

---

## 总结

**✅ 能直接嵌入 DolphinScheduler** - 使用 Python 任务类型即可
**✅ 用户可以拖拽编排** - 在 DolphinScheduler Web UI 中操作
**✅ 开发成本低** - 无需修改 DolphinScheduler 源码

**推荐路径**: 先用方式 1 快速验证 → 用户反馈良好后升级到方式 2 提升体验