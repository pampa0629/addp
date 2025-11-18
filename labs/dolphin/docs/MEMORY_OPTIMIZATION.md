# DolphinScheduler 内存优化指南

## 问题现象

DolphinScheduler Standalone 默认配置会占用约 **3.12 GB** 内存，即使没有任何任务运行。

## 原因分析

### 默认 JVM 配置
```bash
-Xms4g    # 初始堆内存 4GB（启动时立即分配）
-Xmx4g    # 最大堆内存 4GB
-Xmn2g    # 新生代内存 2GB
```

### 为什么这么高？
Standalone 模式在一个 JVM 进程中运行了 5 个组件：
1. **Master Server** - 工作流调度核心
2. **Worker Server** - 任务执行器
3. **API Server** - REST API 服务
4. **Alert Server** - 告警服务
5. **Python Gateway** - Python API 支持

## 优化方案

### 学习环境配置（1GB）

**适用场景**：本地学习、小规模测试（< 10 并发任务）

```yaml
environment:
  JAVA_OPTS: "-server -Xms1g -Xmx1g -Xmn512m -XX:+UseG1GC -XX:MaxGCPauseMillis=200"
```

**预期内存占用**：约 **1GB - 1.2GB**

### 中等规模配置（2GB）

**适用场景**：开发环境、中等并发（10-50 任务）

```yaml
environment:
  JAVA_OPTS: "-server -Xms2g -Xmx2g -Xmn1g -XX:+UseG1GC -XX:MaxGCPauseMillis=200"
```

**预期内存占用**：约 **2GB - 2.5GB**

### 生产环境配置（4GB+）

**适用场景**：生产环境、高并发（50+ 任务）

```yaml
environment:
  JAVA_OPTS: "-server -Xms4g -Xmx4g -Xmn2g -XX:+UseG1GC -XX:MaxGCPauseMillis=200"
```

**预期内存占用**：约 **4GB - 5GB**

## 应用优化配置

### 方法 1：重启容器（推荐）

```bash
# 1. 停止当前容器
make stop

# 2. 移除旧容器（保留数据卷）
docker-compose down

# 3. 启动新配置
make start

# 4. 验证内存占用
docker stats dolphin-standalone --no-stream
```

### 方法 2：手动设置环境变量

如果不想修改 `docker-compose.yml`，可以在启动时覆盖：

```bash
JAVA_OPTS="-server -Xms1g -Xmx1g -Xmn512m" docker-compose up -d
```

## 性能影响分析

### 降低到 1GB 的影响

**优点**：
✅ 内存占用降低 70%（3.1GB → 1GB）
✅ 启动速度更快
✅ 适合笔记本电脑本地开发

**缺点**：
⚠️ 高并发时可能出现 GC 频繁（> 20 并发任务）
⚠️ 大型 DAG（> 100 节点）可能内存不足

### 建议配置表

| 使用场景 | 并发任务数 | DAG 规模 | 推荐内存 |
|---------|-----------|---------|---------|
| 学习测试 | < 5 | < 20 节点 | 1GB |
| 本地开发 | 5-10 | 20-50 节点 | 1-2GB |
| 小团队 | 10-30 | 50-100 节点 | 2-3GB |
| 生产环境 | 30+ | 100+ 节点 | 4GB+ |

## 监控内存使用

### 实时监控
```bash
# 查看容器内存占用
docker stats dolphin-standalone

# 查看 Java 堆内存使用
docker exec dolphin-standalone sh -c "ps aux | grep java"
```

### 查看 GC 日志
```bash
# 进入容器
docker exec -it dolphin-standalone bash

# 查看 GC 日志
tail -f gc.log
```

## 故障排查

### 内存不足错误

如果出现 `OutOfMemoryError`，说明内存配置太低：

```bash
# 查看日志
docker logs dolphin-standalone | grep -i "OutOfMemory"

# 解决方案：增加堆内存
# 修改 docker-compose.yml 中的 JAVA_OPTS，例如从 1g 改为 2g
```

### GC 频繁导致卡顿

如果任务执行缓慢，检查是否 GC 过于频繁：

```bash
# 查看 GC 统计
docker exec dolphin-standalone jstat -gcutil 8 1000 10
```

**解决方案**：
1. 增加堆内存（`-Xmx`）
2. 调整新生代大小（`-Xmn`）
3. 切换 GC 算法（已使用 G1GC，适合大堆）

## 其他优化建议

### 1. 限制 Docker 容器内存
```yaml
services:
  dolphinscheduler:
    deploy:
      resources:
        limits:
          memory: 2G  # 硬限制，防止内存泄漏
        reservations:
          memory: 1G  # 预留内存
```

### 2. 禁用不需要的组件
如果不需要 Python Gateway：
```yaml
ports:
  - "12345:12345"  # 只保留 API Server
  # - "25333:25333"  # 注释掉 Python Gateway
```

### 3. 使用外部数据库
H2 内存数据库会占用额外内存，考虑切换到 PostgreSQL：
```yaml
environment:
  DATABASE: postgresql
  SPRING_DATASOURCE_URL: jdbc:postgresql://postgres:5432/dolphinscheduler
```

## 总结

**推荐配置**：
- 🎓 **学习环境**：1GB（已在 docker-compose.yml 中配置）
- 💻 **开发环境**：2GB
- 🏭 **生产环境**：4GB+

**应用方法**：
```bash
make stop && docker-compose down && make start
```

**验证效果**：
```bash
docker stats dolphin-standalone --no-stream
# 预期显示：MEM USAGE ~1GB（而不是 ~3GB）
```