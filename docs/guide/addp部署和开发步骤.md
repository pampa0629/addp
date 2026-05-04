## 快速开始
需要时，请阅读 docs/guide/addp部署和开发步骤.md。

### 前提条件

**需要 Docker 环境**: ADDP 平台需要安装 Docker 和 Docker Compose。

- 安装 Docker Desktop: https://www.docker.com/products/docker-desktop
- 验证安装: `docker --version` 和 `docker-compose --version`

### 第一步: 启动基础设施 (必须首先执行)

**重要**: 必须先启动基础设施服务 (PostgreSQL、Redis、MinIO、Meilisearch)。

```bash
# 从项目根目录
bash scripts/infra/up.sh
```

此脚本自动完成:
- 启动 PostgreSQL (addp-postgres)、Redis (addp-redis)、MinIO (addp-minio)、Meilisearch (addp-meilisearch) 容器
- 初始化所有模块的 PostgreSQL schemas
- 初始化 MinIO buckets 和 Redis 配置
- 配置 Meilisearch 索引

检查基础设施状态:
```bash
bash scripts/infra/status.sh
```

### 第二步: 开发模式 (推荐用于开发)

**按正确顺序启动所有服务**,使用自动化开发脚本:

```bash
# 从项目根目录
bash scripts/dev/start.sh
```

自动启动以下内容:
1. 基础设施 (如未运行)
2. 所有后端服务 (System、Manager、Meta、Transfer、Orchestrator、Develop、Python Workflow Engine)
3. Gateway 服务
4. 所有前端服务 (可选,提示用户)

停止所有服务:
```bash
bash scripts/dev/stop.sh
```

代码修改后重启:
```bash
bash scripts/dev/restart.sh
```

### 第三步: 构建模式 (用于 Docker 镜像构建)

```bash
# 编译 Go 二进制文件
bash scripts/build/compile.sh

# 构建 Docker 镜像
bash scripts/build/build-images.sh

# 打包并推送镜像 (如需要)
bash scripts/build/package.sh
```

### 第四步: 本地 Docker Compose 模式

```bash
# 通过 Docker Compose 启动完整平台
bash scripts/local/start.sh

# 检查状态
bash scripts/local/status.sh

# 停止所有服务
bash scripts/local/stop.sh
```

### 第五步: 生产模式

**一键生产部署**:

```bash
# 从项目根目录
bash scripts/prod/start.sh
```

**部署流程** (自动执行):

1. **启动基础设施** (PostgreSQL、Redis、MinIO、Meilisearch)
2. **启动 System Backend** (其他服务依赖它)
3. **启动业务后端** (Manager、Meta、Transfer、Orchestrator、Develop、Python Workflow Engine、Gateway)
4. **启动前端服务** (所有模块前端 + Console + Nginx)
5. **健康检查** (验证所有服务就绪)

**访问地址** (部署完成后):

- **✨ Console 控制台 (推荐)**: http://localhost:80
  - 统一登录,一键访问所有模块
  - 通过 Nginx 反向代理,提供最佳用户体验
- **Console 独立访问** (开发调试): http://localhost:5170
- **API Gateway**: http://localhost:8000
- **独立模块访问** (如需单独访问):
  - System: http://localhost:8090
  - Manager: http://localhost:8091
  - Meta: http://localhost:8092
  - Transfer: http://localhost:8093
  - Orchestrator: http://localhost:8094
  - Develop: http://localhost:8095

  ### 构建和部署
  - [`Makefile`](Makefile) - 项目范围的编排命令
  - [`scripts/`](scripts/) - 所有用于开发、构建和部署的自动化脚本
    - [`scripts/infra/`](scripts/infra/) - 基础设施管理 (PostgreSQL, Redis, MinIO, Meilisearch)
      - `up.sh` - 启动基础设施 + 自动初始化
      - `down.sh` - 停止基础设施
      - `status.sh` - 检查服务健康状态
      - `init-postgresql.sh` - 初始化数据库 schemas
      - `init-redis.sh` - 初始化 Redis 配置
      - `init-minio.sh` - 初始化 MinIO buckets
      - `init-meilisearch.sh` - 初始化 Meilisearch 索引
    - [`scripts/dev/`](scripts/dev/) - 开发模式脚本 (直接 Go/npm 进程)
      - `start.sh` - 启动完整开发环境 (基础设施 + 后端 + 前端)
      - `stop.sh` - 停止所有开发服务
      - `restart.sh` - 重启所有服务
      - `modtidy.sh` - 清理 Go 模块依赖
    - [`scripts/build/`](scripts/build/) - 编译和 Docker 镜像构建
      - `compile.sh` - 编译 Go 二进制文件 (go build)
      - `build-images.sh` - 构建 Docker 镜像 (docker build)
      - `package.sh` - 打包部署工件 (docker save/push)
      - `push-images.sh` - 推送镜像到仓库
    - [`scripts/local/`](scripts/local/) - 本地 Docker Compose 部署
      - `start.sh` - 通过 Docker Compose 启动完整平台
      - `stop.sh` - 停止 Docker 服务
      - `status.sh` - 查看容器状态和资源使用
    - [`scripts/prod/`](scripts/prod/) - 生产部署脚本
      - `start.sh` - 启动生产环境 (分步执行)
      - `stop.sh` - 停止生产服务
      - `health-check.sh` - 健康监控
      - `swarm/` - Docker Swarm 高可用部署
    - 完整脚本文档参阅 [`scripts/README.md`](scripts/README.md)
