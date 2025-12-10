# Debug 脚本说明

本目录包含 ADDP 系统的故障诊断和快速修复脚本，用于排查和解决常见问题。

## 📋 脚本清单

### 扫描问题诊断

#### `scan/debug.sh`
**用途**: 调试 Meta 模块扫描任务

**适用场景**:
- 元数据扫描卡住不动
- 扫描任务失败无错误信息
- 数据源扫描不完整

**检查项**:
1. ✅ 检查扫描任务状态
2. ✅ 检查 Asynq 队列
3. ✅ 检查 Worker 日志
4. ✅ 检查数据库连接
5. ✅ 检查 Redis 队列

**命令**:
```bash
./scripts/debug/scan/debug.sh [resource_id]

# 示例：调试资源 ID 为 1 的扫描
./scripts/debug/scan/debug.sh 1
```

**输出示例**:
```
[检查] 扫描任务状态: pending
[检查] Asynq 队列: asynq:meta:default
[检查] 队列中任务数: 0
[诊断] 任务已创建但未入队
[建议] 检查 Meta Backend 日志: logs/meta-backend.log
```

**注意**: 此脚本当前硬编码了资源 ID = 2,计划改进为接受命令行参数。

---

#### `scan/fix.sh`
**用途**: 修复扫描任务常见问题

**适用场景**:
- 扫描任务长时间pending
- Worker 未消费队列
- 扫描结果不更新

**自动修复**:
1. ✅ 重启 Meta Worker
2. ✅ 清理失败的任务
3. ✅ 重新入队待处理任务
4. ✅ 验证数据库连接

**命令**:
```bash
./scripts/debug/scan/fix.sh [resource_id]

# 示例：修复资源 ID 为 1 的扫描
./scripts/debug/scan/fix.sh 1
```

**输出示例**:
```
[修复] 重启 Meta Worker...
[修复] 清理失败任务...
[修复] 重新入队任务: task_id=12345
[✓] 扫描任务修复完成
[提示] 查看进度: curl http://localhost:8082/api/scan/status/1
```

**注意**: 此脚本当前硬编码了资源 ID = 2,计划改进为接受命令行参数。

---

#### `scan/view-log.sh`
**用途**: 查看 Meta 后端扫描日志

**功能**:
- 过滤扫描相关日志
- 高亮错误和警告信息
- 显示最近的扫描活动

**命令**:
```bash
./scripts/debug/scan/view-log.sh
```

---

## 🔄 故障排查流程

### 通用排查步骤

```bash
# 1. 检查服务状态
./scripts/infra/status.sh
docker compose ps

# 2. 查看服务日志
./scripts/dev/status.sh  # 查看所有服务状态
tail -f logs/system-backend.log
tail -f logs/manager-backend.log

# 3. 使用健康检查脚本
./scripts/prod/health-check.sh  # 生产环境健康检查

# 4. 针对扫描问题诊断
./scripts/debug/scan/debug.sh 1    # 1 是资源 ID
./scripts/debug/scan/fix.sh 1      # 修复
```

### 按症状排查

#### 症状：元数据扫描不工作
```bash
./scripts/debug/scan/debug.sh 1    # 1 是资源 ID
./scripts/debug/scan/fix.sh 1      # 修复
```

#### 症状：服务启动失败
```bash
./scripts/infra/status.sh          # 检查基础设施
./scripts/prod/health-check.sh     # 健康检查
./scripts/infra/up.sh              # 启动基础设施（如需要）
```

#### 症状：前端访问异常
```bash
# 检查所有服务状态
./scripts/prod/health-check.sh

# 检查 Nginx 和 Portal
docker compose ps portal nginx

# 查看日志
docker compose logs -f portal
```

---

## 📊 日志文件位置

所有服务的日志文件存储在 `logs/` 目录：

```
logs/
├── system-backend.log      # System 后端日志
├── manager-backend.log     # Manager 后端日志
├── meta-backend.log        # Meta 后端日志
├── transfer-backend.log    # Transfer 后端日志
├── orchestrator-backend.log # Orchestrator 后端日志
├── gateway.log             # Gateway 日志
├── meta-worker.log         # Meta Worker 日志
└── transfer-worker.log     # Transfer Worker 日志
```

**查看日志**:
```bash
# 实时查看
tail -f logs/meta-backend.log

# 搜索错误
grep -i error logs/*.log

# 查看最近50行
tail -n 50 logs/system-backend.log
```

---

## ⚠️ 常见问题与解决方案

### 问题 1: 端口被占用

**症状**:
```
Error: bind: address already in use
```

**诊断**:
```bash
lsof -i :8080  # 检查端口 8080
```

**解决**:
```bash
# 方案 1: 杀死占用进程
kill -9 <PID>

# 方案 2: 修改配置使用其他端口
# 编辑 .env 文件修改端口号
```

---

### 问题 2: 数据库连接失败

**症状**:
```
Error: dial tcp 127.0.0.1:5432: connect: connection refused
```

**诊断**:
```bash
./scripts/infra/status.sh
docker compose -f docker-compose.infra.yml ps postgres
```

**解决**:
```bash
# 启动 PostgreSQL
./scripts/infra/up.sh

# 如果仍然失败，检查配置
cat .env | grep POSTGRES
```

---

### 问题 3: Redis 连接失败

**症状**:
```
Error: dial tcp 127.0.0.1:6379: connect: connection refused
```

**诊断**:
```bash
docker compose -f docker-compose.infra.yml ps redis
```

**解决**:
```bash
# 启动 Redis
./scripts/infra/up.sh

# 测试连接
docker exec addp-redis redis-cli ping
```

---

### 问题 4: MinIO 不可访问

**症状**:
```
Error: The specified bucket does not exist
```

**诊断**:
```bash
curl http://localhost:9000/minio/health/live
```

**解决**:
```bash
# 启动 MinIO
./scripts/infra/up.sh

# 初始化 buckets
./scripts/infra/init-minio.sh
```

---

### 问题 5: Go 模块依赖问题

**症状**:
```
Error: cannot find package
```

**解决**:
```bash
# 清理并重新下载依赖
./scripts/utils/go-mod-tidy-all.sh

# 或手动操作
cd system/backend && go mod tidy
cd manager/backend && go mod tidy
```

---

### 问题 6: 前端构建失败

**症状**:
```
Error: Cannot find module '@common-ui'
```

**解决**:
```bash
# 重新安装依赖
cd portal/frontend
rm -rf node_modules package-lock.json
npm install

# 重新构建
npm run build
```

---

## 🔧 高级调试

### 使用 Docker Compose 日志

```bash
# 查看所有容器日志
docker compose -f docker-compose.infra.yml logs -f

# 查看特定服务日志
docker compose -f docker-compose.infra.yml logs -f postgres

# 查看最近 100 行
docker compose -f docker-compose.infra.yml logs --tail=100
```

### 进入容器调试

```bash
# 进入 PostgreSQL 容器
docker exec -it postgres psql -U addp -d addp

# 进入 Redis 容器
docker exec -it redis redis-cli

# 进入 MinIO 容器
docker exec -it minio sh
```

### 网络调试

```bash
# 检查 Docker 网络
docker network ls
docker network inspect addp-infra_default

# 测试服务间连接
docker exec system-backend ping postgres
docker exec manager-backend curl http://system-backend:8080/health
```

---

## 🔗 相关文档

- [开发环境](../dev/README.md) - 启动和管理开发服务
- [测试脚本](../test/README.md) - 验证功能正确性
- [基础设施](../infra/README.md) - 管理 PostgreSQL、Redis、MinIO
- [生产部署](../prod/README.md) - 生产环境健康检查

---

## 📞 获取帮助

如果以上方法无法解决问题，请：

1. **查看完整日志**: `tail -f logs/*.log`
2. **检查系统资源**: `docker stats`
3. **检查磁盘空间**: `df -h`
4. **提交 Issue**: 附上错误日志和诊断脚本输出

---

## 💡 预防性维护

**定期执行**:
```bash
# 每周清理 Docker 资源
docker system prune -f

# 每月清理日志文件
find logs/ -name "*.log" -mtime +30 -delete

# 定期备份数据库
./scripts/prod/backup.sh
```

**监控建议**:
- 监控磁盘空间使用
- 监控服务健康检查端点
- 定期查看错误日志

---

## 🗑️ 已删除的调试脚本

以下脚本已被删除，因为已过时或被更好的工具替代：

- ~~`diagnose-502.sh`~~ - 已被 `scripts/prod/health-check.sh` 替代
- ~~`diagnose-startup.sh`~~ - 已被 `scripts/infra/status.sh` 和 `scripts/prod/health-check.sh` 替代
- ~~`fix-portal-blank.sh`~~ - 针对已解决的历史问题，现已不再需要
- ~~`scan/quick-fix.sh`~~ - 硬编码路径，无法通用使用

使用更通用的诊断工具:
- `scripts/infra/status.sh` - 检查基础设施服务
- `scripts/prod/health-check.sh` - 健康检查所有服务
- `scripts/dev/status.sh` - 开发环境状态检查
