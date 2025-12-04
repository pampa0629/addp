# ADDP 基础设施脚本

本目录包含所有与 ADDP 基础设施（PostgreSQL、Redis、MinIO）相关的初始化和管理脚本。

## 目录结构

```
scripts/infra/
├── README.md                    # 本文件
├── up.sh                        # 启动基础设施服务
├── down.sh                      # 停止基础设施服务
├── restart.sh                   # 重启基础设施服务
├── status.sh                    # 查看服务状态
├── init-db.sh                   # 初始化数据库（执行所有 SQL 文件）
├── init-db.sql                  # 主数据库 schema 初始化
├── init-orchestrator.sql        # Orchestrator 模块 schema
├── init-minio.sh                # 初始化 MinIO buckets
├── init-redis.sh                # 初始化 Redis 配置
├── init-postgis.sh              # 初始化 PostGIS 扩展
└── init-pgvector.sh             # 初始化 pgvector 扩展
```

## 使用方法

### 通过 Makefile（推荐）

```bash
# 启动基础设施
make infra-up

# 查看状态
make infra-status

# 停止基础设施
make infra-down

# 重启基础设施
make infra-restart

# 初始化数据库
make db-migrate

# 初始化 MinIO
make init-minio

# 初始化 Redis
make init-redis
```

### 直接调用脚本

```bash
# 启动基础设施（自动进行端口检查和健康检查）
./scripts/infra/up.sh

# 初始化数据库（按顺序执行所有 SQL 文件）
./scripts/infra/init-db.sh

# 初始化 MinIO buckets（包括 mvt-tiles 等）
./scripts/infra/init-minio.sh

# 初始化 Redis（配置任务队列和缓存）
./scripts/infra/init-redis.sh

# 查看所有服务状态
./scripts/infra/status.sh

# 停止所有基础设施服务
./scripts/infra/down.sh

# 完整重启（清理旧镜像、重建、重启）
./scripts/infra/restart.sh
```

## SQL 初始化文件

SQL 文件按顺序执行（在 `init-db.sh` 中定义）：

1. **init-db.sql** - 主数据库 schema
   - System schema（用户、租户、审计日志、资源）
   - Manager schema（数据源、目录、权限、快显）
   - Metadata schema（元数据节点、元数据项、字典、变更日志）
   - Transfer schema（任务、执行记录、数据映射、检查点）
   - Develop schema（SQL 脚本管理）

2. **init-orchestrator.sql** - Orchestrator 模块
   - Orchestrator schema（编排定义、执行实例）

### 添加新的 SQL 初始化文件

如果需要添加新模块的 SQL 初始化：

1. 创建新的 SQL 文件，例如 `init-newmodule.sql`
2. 在 `init-db.sh` 中的 `SQL_FILES` 数组添加该文件路径：
   ```bash
   SQL_FILES=(
     "${SCRIPT_DIR}/init-db.sql"
     "${SCRIPT_DIR}/init-orchestrator.sql"
     "${SCRIPT_DIR}/init-newmodule.sql"  # 新增
   )
   ```
3. 重新运行 `./scripts/infra/init-db.sh` 即可

## 脚本说明

### up.sh
- 启动 PostgreSQL、Redis、MinIO 容器
- 自动进行端口占用检查
- 等待服务健康检查通过
- 自动初始化 PostGIS 和 pgvector 扩展

### down.sh
- 停止所有基础设施容器
- 支持 `--rm` 参数移除容器（不删除数据卷）

### restart.sh
- 完整的重启流程
- 清理旧 Docker 镜像
- 重新构建必要的服务
- 等待服务健康后自动初始化扩展

### status.sh
- 显示所有容器状态
- 检查健康状态
- 显示访问 URL 和端口信息

### init-db.sh
- 按顺序执行所有 SQL 初始化文件
- 支持多个 SQL 文件的模块化管理
- 自动检查 PostgreSQL 容器是否运行
- 使用事务确保数据一致性

### init-minio.sh
- 创建必要的 MinIO buckets
- 配置 bucket 策略
- 初始化 MVT 瓦片缓存目录

### init-redis.sh
- 配置 Redis 认证
- 初始化 Asynq 任务队列
- 设置缓存命名空间
- 配置持久化策略

## 注意事项

1. **执行顺序很重要**：先启动基础设施（`up.sh`），再初始化数据库（`init-db.sh`）
2. **数据持久化**：所有数据存储在 Docker volumes 中，停止容器不会丢失数据
3. **端口冲突**：启动前会自动检查端口是否被占用
4. **健康检查**：脚本会等待服务完全启动后才继续执行
5. **幂等性**：所有 SQL 脚本使用 `IF NOT EXISTS`，可以重复执行

## 相关文档

- [CLAUDE.md](../../CLAUDE.md) - 项目整体架构说明
- [docs/INFRA_ARCHITECTURE_DETECTION.md](../../docs/INFRA_ARCHITECTURE_DETECTION.md) - 基础设施架构检测
- [docker-compose.yml](../../docker-compose.yml) - Docker Compose 配置
