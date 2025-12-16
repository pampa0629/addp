
## 常用 Make 命令 (项目根目录)

```bash
# 初始化
make init                # 创建配置文件和目录
make install-deps        # 安装 Go 和 npm 依赖

# 开发
make dev-start           # 按正确顺序启动所有服务 (推荐)
make dev-stop            # 停止所有开发服务
make dev-health          # 检查所有服务健康状态
make dev-system          # 在开发模式下运行 System
make dev-manager         # 运行 Manager 后端
make dev-gateway         # 运行 Gateway 服务

# Docker 操作
make up                  # 仅启动 System 模块
make up-full             # 启动所有服务 (完整平台)
make up-infra            # 仅启动 PostgreSQL, Redis, MinIO
make down                # 停止所有服务
make restart             # 重启 System 模块
make restart-full        # 重启所有服务

# 构建
make build               # 构建所有工件到 dist/
make docker-build        # 构建 System Docker 镜像
make docker-build-all    # 构建所有服务 Docker 镜像

# 监控
make status              # 显示所有服务状态和 URL
make logs                # 查看所有服务日志
make logs-system         # 仅查看 System 日志
make logs-manager        # 查看 Manager 日志
make health              # 检查所有服务健康状态

# 数据库
make db-shell            # 连接到 PostgreSQL
make db-migrate          # 运行数据库迁移 (init-db.sql)
make redis-cli           # 连接到 Redis
make minio-setup         # 初始化 MinIO buckets
make backup              # 备份 PostgreSQL 数据库
make restore FILE=...    # 从备份恢复数据库

# 测试和质量
make test                # 运行所有测试
make test-system         # 运行 System 模块测试
make lint                # 运行代码检查器
make fmt                 # 格式化 Go 代码

# 清理
make clean               # 删除构建工件
make clean-all           # 删除所有数据和卷 (破坏性)
```