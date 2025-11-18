# DolphinScheduler 工作流失败排查指南

## 查看失败日志的 3 种方法

### 方法 1: Web UI 查看（最直观）

1. **打开工作流实例列表**
   - 访问: http://localhost:12345/dolphinscheduler/ui
   - 左侧菜单: 项目管理 → 选择你的项目 → 工作流实例

2. **点击失败的工作流实例**
   - 在列表中找到状态为 ❌ 失败的实例
   - 点击实例名称（如 `gis-1-20251118143647308`）

3. **查看 DAG 图**
   - 会显示工作流的 DAG 图
   - 失败的任务节点会标红

4. **查看任务日志**
   - 点击失败的任务节点
   - 右侧会弹出任务详情
   - 点击 "查看日志" 或 "View Log" 按钮
   - 可以看到完整的执行输出和错误信息

### 方法 2: 容器内查看日志文件

```bash
# 进入容器
docker exec -it dolphin-standalone bash

# 查看 Master 日志
tail -f /opt/dolphinscheduler/logs/dolphinscheduler-master.log

# 查看 Worker 日志
tail -f /opt/dolphinscheduler/logs/dolphinscheduler-worker.log

# 查看 API 日志
tail -f /opt/dolphinscheduler/logs/dolphinscheduler-api.log

# 查看任务执行日志（按日期）
ls -lh /opt/dolphinscheduler/logs/
tail -f /opt/dolphinscheduler/logs/<日期>/

# 搜索错误信息
grep -r "ERROR" /opt/dolphinscheduler/logs/ | tail -20
grep -r "Exception" /opt/dolphinscheduler/logs/ | tail -20
```

### 方法 3: 从宿主机查看容器日志

```bash
# 查看最近 100 行日志
docker logs dolphin-standalone --tail 100

# 实时查看日志
docker logs -f dolphin-standalone

# 搜索错误
docker logs dolphin-standalone 2>&1 | grep -i error | tail -20
docker logs dolphin-standalone 2>&1 | grep -i exception | tail -20

# 查看特定时间段的日志
docker logs dolphin-standalone --since 10m

# 保存日志到文件
docker logs dolphin-standalone > dolphin.log 2>&1
```

## 常见失败原因和解决方案

### 1. CPU 过载（你当前遇到的）

**症状**:
```
Master OverLoad: the TotalCpuUsedPercentage: 1.0 is over then the MaxCpuUsagePercentageThresholds 0.9
```

**原因**:
- 系统 CPU 使用率超过 90%
- DolphinScheduler 拒绝执行新任务以保护系统

**解决方案**:

#### 方案 A: 临时方案 - 释放 CPU 资源
```bash
# 关闭其他占用 CPU 的程序
# 等待 CPU 使用率下降
# 然后在 Web UI 中重新运行工作流
```

#### 方案 B: 调整 CPU 阈值配置
```bash
# 进入容器
docker exec -it dolphin-standalone bash

# 编辑配置文件
vi /opt/dolphinscheduler/conf/common.properties

# 找到并修改以下配置（放宽限制）
# master.max.cpu.load.avg=-1                    # 禁用 CPU 负载检查
# master.reserved.memory=0.3                     # 预留 30% 内存

# 重启容器
exit
docker restart dolphin-standalone
```

#### 方案 C: 增加 Docker 资源限制
```bash
# 在 docker-compose.yml 中添加:
services:
  dolphinscheduler:
    # ...
    deploy:
      resources:
        limits:
          cpus: '2.0'      # 增加 CPU 限制
          memory: 4G       # 增加内存限制
```

### 2. Shell 脚本执行失败

**症状**: 任务状态为失败，但 DolphinScheduler 本身正常

**可能原因**:
- 脚本路径错误
- 权限不足
- 依赖缺失（如 Python、库等）
- 脚本语法错误

**解决方案**:
```bash
# 1. 确认脚本路径正确
# 2. 确认脚本有执行权限
chmod +x /path/to/script.sh

# 3. 在容器内手动测试脚本
docker exec -it dolphin-standalone bash
cd /path/to/script
./script.sh  # 看是否能正常执行

# 4. 检查依赖是否安装
python3 --version
which python3
pip3 list
```

### 3. 资源不足

**症状**:
```
Worker OverLoad: Memory/CPU insufficient
```

**解决方案**:
- 减少并发任务数
- 增加 Docker 容器资源
- 使用外部 Worker 节点

### 4. 数据库连接失败

**症状**:
```
Unable to connect to database
Connection refused
```

**解决方案**:
```bash
# 检查数据库是否启动
docker exec -it dolphin-standalone bash
# 如果使用 H2 数据库（默认）
ls -lh /opt/dolphinscheduler/standalone-server/data/

# 如果使用 PostgreSQL
psql -h localhost -U dolphinscheduler -d dolphinscheduler
```

### 5. 端口冲突

**症状**:
```
Port 12345 already in use
```

**解决方案**:
```bash
# 检查端口占用
lsof -i :12345
netstat -an | grep 12345

# 修改 docker-compose.yml 中的端口映射
ports:
  - "12346:12345"  # 改为 12346
```

## 针对你的工作流的排查步骤

### 步骤 1: 确认 CPU 使用率

```bash
# 在 macOS 上查看 CPU
top -l 1 | grep "CPU usage"

# 查看 Docker 容器 CPU 使用情况
docker stats dolphin-standalone --no-stream
```

### 步骤 2: 在 Web UI 中查看详细日志

1. 访问 http://localhost:12345/dolphinscheduler/ui
2. 导航到工作流实例列表
3. 点击失败的实例 `gis-1-20251118143647308`
4. 查看具体哪个任务失败
5. 查看该任务的执行日志

### 步骤 3: 重新运行工作流

**如果是 CPU 过载导致的失败**:
1. 等待 1-2 分钟让 CPU 使用率下降
2. 在 Web UI 中点击 "重新运行" 按钮
3. 观察是否成功执行

**如果是脚本问题**:
1. 修改工作流定义
2. 修正脚本路径或内容
3. 保存并重新运行

## 快速检查清单

- [ ] DolphinScheduler 容器是否正常运行？
  ```bash
  docker ps | grep dolphin
  ```

- [ ] CPU 使用率是否过高？
  ```bash
  docker stats dolphin-standalone --no-stream
  ```

- [ ] Web UI 是否可以访问？
  ```bash
  curl -I http://localhost:12345/dolphinscheduler/ui
  ```

- [ ] 工作流定义是否正确？
  - 在 Web UI 中查看工作流定义
  - 确认任务配置正确

- [ ] 任务脚本是否可执行？
  ```bash
  docker exec -it dolphin-standalone bash
  # 手动执行脚本测试
  ```

## 下一步建议

1. **立即操作**: 在 Web UI 中点击失败的工作流实例，查看详细日志

2. **如果是 CPU 过载**: 等待几分钟，然后点击 "重新运行"

3. **如果是脚本问题**:
   - 记录错误信息
   - 修改工作流定义
   - 测试脚本后重新运行

4. **如果需要帮助**:
   - 复制任务执行日志
   - 告诉我具体的错误信息
   - 我会帮你分析和解决
