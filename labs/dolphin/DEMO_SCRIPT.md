# 空间算子在 DolphinScheduler 中的演示流程

## 🎯 演示目标
展示如何在 DolphinScheduler 中通过拖拽编排空间算子工作流

---

## 📋 准备工作（5 分钟）

### 1. 启动 DolphinScheduler
```bash
cd /Users/pampa/code/addp/labs/dolphin
make start
make web
```

### 2. 上传空间算子代码
```bash
./upload_to_dolphin.sh
```

输出示例:
```
✅ 登录成功
✅ 项目代码: 123456
✅ operator_executor.py 上传成功
✅ operators.py 上传成功
```

---

## 🎬 演示脚本（10 分钟）

### 场景: 分析北京市中心区域的空间关系

**业务需求**:
1. 对天安门位置创建 1000 米缓冲区
2. 对故宫位置创建 800 米缓冲区
3. 计算两个缓冲区的交集面积

---

### 第 1 步: 创建工作流定义

1. 访问 http://localhost:12345/dolphinscheduler/ui
2. 登录（admin / dolphinscheduler123）
3. 进入 **项目管理** → **spatial_analysis**
4. 点击 **工作流定义** → **创建工作流**
5. 输入工作流名称: `beijing_buffer_analysis`

---

### 第 2 步: 添加第一个算子（天安门缓冲区）

1. 从左侧拖拽 **Python** 任务到画布
2. 双击任务打开配置面板
3. 填写任务信息:

**任务名称**: `buffer_tiananmen`

**Python 脚本**:
```python
import subprocess
import json

# 天安门坐标
input_geom = {
    "type": "Point",
    "coordinates": [116.39754, 39.90750]  # 天安门广场
}

distance = 1000.0  # 1000 米缓冲区
segments = 32      # 高精度圆弧

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
    ['python3', '/opt/dolphinscheduler/resources/operator_executor.py',
     json.dumps(task_config)],
    capture_output=True,
    text=True
)

print(result.stdout)

if result.returncode != 0:
    raise Exception(f"算子执行失败: {result.stderr}")

# 输出到变量池
output = json.loads(result.stdout)
print(f"##[set-output name=buffer_geom]{json.dumps(output['result'])}")
```

4. 保存任务

---

### 第 3 步: 添加第二个算子（故宫缓冲区）

1. 再次拖拽 **Python** 任务到画布
2. 配置任务:

**任务名称**: `buffer_gugong`

**Python 脚本**:
```python
import subprocess
import json

# 故宫坐标
input_geom = {
    "type": "Point",
    "coordinates": [116.39723, 39.91649]  # 故宫博物院
}

distance = 800.0  # 800 米缓冲区
segments = 32

task_config = {
    "operator": "buffer",
    "params": {
        "input_geom": input_geom,
        "distance": distance,
        "segments": segments
    }
}

result = subprocess.run(
    ['python3', '/opt/dolphinscheduler/resources/operator_executor.py',
     json.dumps(task_config)],
    capture_output=True,
    text=True
)

print(result.stdout)

if result.returncode != 0:
    raise Exception(result.stderr)

output = json.loads(result.stdout)
print(f"##[set-output name=buffer_geom]{json.dumps(output['result'])}")
```

---

### 第 4 步: 添加交集算子

1. 拖拽第三个 **Python** 任务到画布
2. 配置任务:

**任务名称**: `intersection_analysis`

**Python 脚本**:
```python
import subprocess
import json

# 引用上游任务的输出
geom_a = ${buffer_tiananmen.buffer_geom}
geom_b = ${buffer_gugong.buffer_geom}

task_config = {
    "operator": "intersection",
    "params": {
        "geom_a": json.loads(geom_a),
        "geom_b": json.loads(geom_b)
    }
}

result = subprocess.run(
    ['python3', '/opt/dolphinscheduler/resources/operator_executor.py',
     json.dumps(task_config)],
    capture_output=True,
    text=True
)

print(result.stdout)

if result.returncode != 0:
    raise Exception(result.stderr)

# 计算交集面积
output = json.loads(result.stdout)
from shapely.geometry import shape
intersection_geom = shape(output['result'])
area = intersection_geom.area

print(f"🎉 交集面积: {area:.2f} 平方度")
print(f"   约等于: {area * 111000 * 111000:.2f} 平方米")
```

---

### 第 5 步: 连接任务形成 DAG

1. 拖拽连线:
   ```
   buffer_tiananmen  ───┐
                        ├──→ intersection_analysis
   buffer_gugong     ───┘
   ```

2. 最终 DAG 结构:
   ```
   ┌─────────────────┐
   │ buffer_tiananmen│
   │  (天安门1000m)  │
   └────────┬────────┘
            │
            ├──────┐
            │      ▼
            │  ┌──────────────────┐
            │  │ intersection     │
            │  │ _analysis        │
            │  │ (计算交集)       │
            │  └──────────────────┘
            │      ▲
   ┌────────▼──────┘
   │ buffer_gugong  │
   │  (故宫800m)    │
   └────────────────┘
   ```

---

### 第 6 步: 保存并运行工作流

1. 点击工具栏 **保存** 按钮
2. 点击 **上线** 按钮（使工作流可执行）
3. 点击 **运行** 按钮
4. 选择运行模式: **并行执行**（buffer_tiananmen 和 buffer_gugong 并行）
5. 点击 **确定**

---

### 第 7 步: 监控执行过程

1. 进入 **工作流实例** 页面
2. 观察任务执行状态:
   ```
   buffer_tiananmen   [运行中]  ─┐
                                  ├─→ intersection_analysis [等待中]
   buffer_gugong      [运行中]  ─┘
   ```

3. 等待任务完成:
   ```
   buffer_tiananmen   [成功] ✅
   buffer_gugong      [成功] ✅
   intersection_analysis [成功] ✅
   ```

---

### 第 8 步: 查看结果

1. 点击 `intersection_analysis` 任务节点
2. 点击 **查看日志**
3. 查看输出:

```json
{
  "status": "success",
  "operator": "intersection",
  "result": {
    "type": "Polygon",
    "coordinates": [...]
  }
}

🎉 交集面积: 0.05 平方度
   约等于: 617025.00 平方米
```

---

## 🎉 演示亮点

### ✅ 用户看到的价值

1. **无需编程**: 拖拽即可完成空间分析
2. **可视化编排**: DAG 图清晰展示数据流
3. **并行执行**: 两个缓冲区任务自动并行（性能提升）
4. **参数传递**: 自动将上游结果传递给下游
5. **日志追踪**: 实时查看每个算子的执行日志
6. **失败重试**: 自动重试机制（DolphinScheduler 内置）

### ✅ 技术亮点

- **模块化设计**: 每个算子独立封装
- **标准化接口**: 统一的 JSON 输入输出
- **扩展性强**: 新增算子只需注册，无需修改工作流
- **云原生**: 容器化部署，易于扩展

---

## 📊 复杂场景演示（可选）

### 场景 2: POI 密度分析

**业务需求**: 分析商圈周围 500 米内的餐厅密度

工作流结构:
```
[加载商圈数据]
      ↓
[创建500m缓冲区]
      ↓
[空间连接餐厅POI表] ──→ [统计聚合] ──→ [输出密度热力图]
```

---

## 🔧 调试技巧

### 问题 1: 任务执行失败

**排查步骤**:
1. 查看任务日志（点击任务节点 → 查看日志）
2. 检查 Python 环境是否安装 `shapely`
3. 确认资源文件路径正确

### 问题 2: 参数传递失败

**解决方案**:
```python
# 检查上游任务是否正确输出
print(f"##[set-output name=result]{json.dumps(result)}")

# 下游任务中打印接收到的值
print(f"接收到的参数: {geom_a}")
```

---

## 📈 性能优化建议

### 1. 大数据集处理
- 使用分块处理（将大表拆分为多个任务）
- 利用 DolphinScheduler 的动态子工作流功能

### 2. 缓存机制
```python
import redis
r = redis.Redis(host='localhost')

# 缓存中间结果
cache_key = f"buffer_{lat}_{lng}_{distance}"
if r.exists(cache_key):
    result = json.loads(r.get(cache_key))
else:
    result = calculate_buffer(...)
    r.set(cache_key, json.dumps(result), ex=3600)
```

---

## 🎓 用户培训要点

### 核心概念

1. **算子 = 积木块**: 每个算子是独立的功能单元
2. **工作流 = 搭积木**: 通过连接算子构建复杂流程
3. **参数传递 = 数据流**: 上游结果自动传给下游
4. **DAG = 执行计划**: 箭头方向决定执行顺序

### 常见误区

❌ **误区 1**: 认为必须串行执行
✅ **正确**: DolphinScheduler 自动识别并行机会

❌ **误区 2**: 手动管理中间结果
✅ **正确**: 使用变量池自动传递

---

## 📝 总结

通过这个演示，用户可以:
- ✅ 在 DolphinScheduler Web UI 中直接操作空间算子
- ✅ 通过拖拽快速构建空间分析工作流
- ✅ 无需关心底层 Python 实现细节
- ✅ 享受分布式调度的强大能力

**下一步**: 扩展更多空间算子（如热力图生成、路径规划等）