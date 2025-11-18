# DolphinScheduler 工作流配置指南

## 问题诊断结果

你的工作流失败原因：

1. ❌ **路径错误**: `/Users/pampa/...` 是宿主机路径，容器内不存在
2. ❌ **Python3 未安装**: 容器默认没有 Python3

## ✅ 解决方案

### 已完成的修复

1. **挂载代码目录到容器**
   ```yaml
   volumes:
     - ./backend:/opt/dolphin-scripts/backend
     - ./output:/opt/dolphin-scripts/output
   ```

2. **容器内安装 Python3**（正在进行中）

### 修正后的 Shell 脚本

在 DolphinScheduler Web UI 中，修改你的 Shell 任务脚本为：

#### 方案 1: 使用容器内路径（推荐）

```bash
# 切换到容器内的挂载路径
cd /opt/dolphin-scripts/backend/examples

# 执行脚本
python3 run_demo.py
```

#### 方案 2: 简单测试脚本（先验证环境）

```bash
# 测试基本命令
echo "=== 环境检查 ==="
echo "当前目录: $(pwd)"
echo "Python 版本: $(python3 --version)"
echo "挂载目录内容:"
ls -lh /opt/dolphin-scripts/backend/examples/

echo ""
echo "=== 执行 Demo ==="
cd /opt/dolphin-scripts/backend/examples
python3 run_demo.py
```

#### 方案 3: 最简单的 Hello World（确认工作流正常）

```bash
#!/bin/bash
echo "Hello from DolphinScheduler!"
date
hostname
uname -a
```

## 创建新工作流的步骤

### 步骤 1: 等待容器初始化完成（约 1-2 分钟）

```bash
# 检查容器状态
docker ps | grep dolphin

# 检查 Python 是否安装成功
docker exec dolphin-standalone python3 --version

# 检查挂载目录
docker exec dolphin-standalone ls -lh /opt/dolphin-scripts/backend/examples/
```

### 步骤 2: 在 Web UI 中创建新工作流

1. **访问**: http://localhost:12345/dolphinscheduler/ui
2. **登录**: admin / dolphinscheduler123
3. **进入项目**: 点击你的项目（如 "gis"）
4. **创建工作流**:
   - 点击 "工作流定义" → "创建工作流"
   - 工作流名称: `test-python-demo`
   - 拖拽一个 "Shell" 任务到画布

5. **配置 Shell 任务**:
   - 任务名称: `run_demo`
   - 脚本内容: 使用上面的"方案 2"
   - 点击 "确定"

6. **保存并上线**:
   - 点击 "保存"
   - 点击 "上线"

### 步骤 3: 运行工作流

1. 点击 "运行" 按钮
2. 选择运行模式（默认即可）
3. 点击 "确定"
4. 切换到 "工作流实例" 查看执行状态

## 验证环境是否就绪

运行以下命令检查：

```bash
# 1. 容器是否运行
docker ps | grep dolphin

# 2. Python3 是否安装
docker exec dolphin-standalone python3 --version

# 3. 挂载目录是否可访问
docker exec dolphin-standalone ls /opt/dolphin-scripts/backend/examples/

# 4. 脚本文件是否存在
docker exec dolphin-standalone ls /opt/dolphin-scripts/backend/examples/run_demo.py

# 5. 测试直接执行
docker exec dolphin-standalone bash -c "cd /opt/dolphin-scripts/backend/examples && python3 --version"
```

## 预期结果

成功运行后，你应该看到：

1. **工作流状态**: ✅ 成功
2. **执行日志**: 包含完整的演示输出
3. **生成文件**:
   - `output/poi_buffer_result.geojson`
   - `output/poi_buffer_lineage.json`
   - `output/lineage_graph.mmd`

## 如果还是失败

### 检查清单

- [ ] 容器是否正常运行？
- [ ] Python3 是否安装成功？
- [ ] 路径是否改为容器内路径？
- [ ] 挂载目录是否可访问？
- [ ] 脚本文件是否存在？

### 获取详细日志

```bash
# 容器日志
docker logs dolphin-standalone --tail 100

# 进入容器手动测试
docker exec -it dolphin-standalone bash
cd /opt/dolphin-scripts/backend/examples
python3 run_demo.py
```

### 安装 Python 依赖

如果需要 geopandas 等库（可选）：

```bash
docker exec dolphin-standalone bash -c "
  pip3 install --quiet geopandas shapely 2>/dev/null || \
  echo '提示: geopandas 安装失败，但演示脚本会自动处理'
"
```

## 下一步

1. ⏳ **等待 Python 安装完成**（约 1-2 分钟）
2. ✅ **验证环境**（运行上面的验证命令）
3. 🔄 **重新运行工作流**（使用修正后的脚本）
4. 📊 **查看结果**（工作流应该成功执行）
