# ADDP - All Domain Data Platform

<div align="center">

**全域数据平台 - 企业级微服务数据平台**

[![Version](https://img.shields.io/badge/version-0.1.14-blue.svg)](https://github.com/addp/addp)
[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8.svg)](https://golang.org/)
[![Vue](https://img.shields.io/badge/Vue-3.x-4FC08D.svg)](https://vuejs.org/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED.svg)](https://www.docker.com/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](#-许可证)

[English](#english) | [中文文档](#中文文档)

</div>

---

## 中文文档

### 📖 项目简介

ADDP (All Domain Data Platform / 全域数据平台) 是一个企业级数据平台，采用微服务架构，提供数据管理、元数据服务、数据传输、工作流编排等完整的数据平台解决方案。

### ✨ 核心特性

- 🏗️ **微服务架构** - 模块化设计，独立部署，弹性扩展
- 🔐 **统一认证** - opaque Token、OAuth 2.0、AuthContext 与多租户隔离
- 📊 **数据管理** - 多数据源连接、数据预览、元数据扫描
- 🔄 **数据传输** - 数据导入/导出/同步，支持增量传输
- 🎯 **工作流编排** - 可视化编排，任务调度，监控告警
- 🗺️ **空间数据支持** - 完整的 GIS 数据处理能力
- 📱 **Console 控制台** - 一键登录，所有模块统一访问入口

### 🚀 快速开始

#### 前置要求

- **Go** 1.23+
- **Node.js** 16+
- **Docker** & **Docker Compose** (可选，用于容器化部署)
- **PostgreSQL** 15+ (开发模式需要)

#### 方式一：开发模式 (推荐开发调试)

```bash
# 1. 克隆项目
git clone https://github.com/addp/addp.git
cd addp

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env 修改数据库密码等配置

# 3. 一键启动完整开发环境（并行启动优化，约 25-39 秒）
bash scripts/dev/start.sh

# 4. 快速重启（不重新编译，约 15-25 秒）
bash scripts/dev/restart.sh

# 5. 选择性编译重启（只重新编译修改的模块）
bash scripts/dev/restart.sh -manager          # 只重新编译 Manager
bash scripts/dev/restart.sh -meta -transfer   # 重新编译 Meta 和 Transfer
bash scripts/dev/restart.sh -all              # 重新编译所有模块

# 6. 停止所有服务
bash scripts/dev/stop.sh

# 访问服务
# - Console: http://localhost:5170
# - Gateway: http://localhost:8000
# - System Backend: http://localhost:8180
```

**特点**：
- ✅ 直接运行 Go 和 npm 进程，无需 Docker
- ✅ **并行编译和启动** - 6 个后端 + 3 个 Workers 同时启动
- ✅ **快速重启** - 支持不重新编译或选择性编译
- ✅ **智能服务检测** - 自动跳过已运行的服务
- ✅ 自动启动基础设施容器 (PostgreSQL, Redis, MinIO)
- ✅ 健康检查和日志管理

**性能优化**：
- 完整启动时间：从 **47-78s** 降至 **25-39s**（提速 50%）
- 快速重启：**15-25s**（无需重新编译）
- 选择性重启：**10-20s**（只编译修改的模块）

#### 开发交付验证

```bash
# 默认：验证当前已跟踪和未跟踪改动影响的 T0-T3 门禁
make test-changed

# 验证一段已提交变更，或明确验证单个模块
make test-changed BASE_REF=<ref>
make test-module MODULE=<模块>
```

GitHub Actions 会在代码推送到 `main` 后使用独立环境复验，并每日定时检查环境和依赖漂移。本地启动、`scripts/dev/restart.sh` 和 `git commit` 不会触发 CI。新增模块、测试入口、构建单元或外部服务依赖时，必须在同一次变更中补齐根 `Makefile`、CI 路径或自动发现、平台一致性检查和文档；详细要求见 [开发原则](docs/spec/addp开发原则.md)。

#### 方式二：本地 Docker 部署 (推荐集成测试)

```bash
# 1. 编译和构建镜像
make build
make build-images

# 2. 启动完整平台
bash scripts/local/start.sh

# 访问服务
# - Console (推荐): http://localhost:80
# - Gateway: http://localhost:8000
```

**特点**：
- ✅ 完全容器化，与生产环境一致
- ✅ 自动镜像验证和健康检查
- ✅ 资源使用监控

#### 方式三：生产环境部署

```bash
# 1. 准备部署包
make build BUILD_ARGS="--arch both"
IMAGE_TAG=v1.0.0 make build-images IMAGE_BUILD_ARGS=--multi-arch
bash scripts/build/package.sh --mode registry

# 2. 部署到生产服务器
bash scripts/prod/start.sh

# 3. 健康检查
bash scripts/prod/health-check.sh

# 访问服务
# - ✨ 统一入口: http://localhost （Nginx）
# - Console: http://localhost:5170
# - Gateway: http://localhost:8000
```

**特点**：
- ✅ 分步启动，依赖关系明确
- ✅ 支持 Docker Swarm 高可用部署
- ✅ 健康监控和自动重启

### 📁 项目结构

```
addp/
├── common/              # 共享后端库 (client, models, config)
├── common-frontend/     # 共享前端组件 (Vue 3)
│   ├── basic/          # 基础 UI 组件
│   └── map/            # 地图相关组件
├── console/             # 控制台入口
├── system/             # 核心系统模块 (认证、日志、资源管理)
├── gateway/            # API 网关
├── manager/            # 数据管理模块 (数据源、预览)
├── meta/               # 元数据服务 (扫描、血缘)
├── transfer/           # 数据传输模块 (导入/导出)
├── orchestrator/       # 工作流编排模块
├── develop/            # 数据开发模块
├── business/           # 业务库 (独立部署)
├── scripts/            # 自动化脚本 (⭐ 核心工具)
│   ├── infra/         # 基础设施管理
│   ├── dev/           # 开发模式
│   ├── build/         # 编译和构建
│   ├── local/         # 本地部署
│   └── prod/          # 生产部署
├── docs/              # 详细文档
├── nginx/             # Nginx 配置
├── Makefile           # Make 命令封装
├── .env.example       # 环境变量模板
└── docker-compose*.yml # Docker Compose 配置
```

### 🛠️ Scripts 脚本工具

ADDP 提供完整的自动化脚本工具链，覆盖开发、构建、部署全流程：

| 目录 | 用途 | 核心脚本 | 使用场景 |
|------|------|---------|---------|
| **scripts/infra/** | 基础设施管理 | `up.sh`, `down.sh`, `status.sh` | 启动/停止 PostgreSQL、Redis、MinIO |
| **scripts/dev/** | 开发模式 | `start.sh`, `stop.sh`, `restart.sh` | 日常开发调试 |
| **scripts/build/** | 编译构建 | `compile.sh`, `build-images.sh`, `package.sh` | 构建发布版本 |
| **scripts/local/** | 本地部署 | `start.sh`, `stop.sh`, `status.sh` | 本地容器化测试 |
| **scripts/prod/** | 生产部署 | `start.sh`, `health-check.sh`, `swarm/` | 生产环境部署 |

📚 **详细文档**: 查看 [scripts/README.md](scripts/README.md) 了解完整使用指南

### 🏗️ 技术栈

#### 后端

- **语言**: Go 1.23+
- **Web 框架**: Gin
- **ORM**: GORM
- **数据库**: PostgreSQL 15 (schema 隔离)
- **缓存/事件**: Redis 7
- **对象存储**: MinIO (S3 兼容)
- **有界执行领取**: PostgreSQL claim + execution lease
- **全文搜索**: Meilisearch

#### 前端

- **框架**: Vue 3 + Composition API
- **构建工具**: Vite
- **UI 库**: Element Plus
- **状态管理**: Pinia
- **路由**: Vue Router
- **地图**: OpenLayers + 高德地图

#### 基础设施

- **容器化**: Docker + Docker Compose
- **反向代理**: Nginx
- **高可用**: Docker Swarm (可选)

### 🔑 IAM 首次初始化

ADDP 不创建默认全权管理员、默认租户或共享弱密码账号。首次平台系统管理员、安全管理员和审计管理员只能通过一次性离线 Bootstrap 建立；Bootstrap 完成后永久关闭。

### 📊 服务端口

| 服务 | 开发端口 | 生产端口 | 说明 |
|------|---------|---------|------|
| **Nginx Gateway** | - | **80** | **统一入口 (推荐)** |
| **Console** | **5170** | **5170** | Console 前端 |
| Gateway | 8000 | 8000 | API 网关 |
| System Backend | 8180 | 8180 | 认证、用户管理 |
| Manager Backend | 8081 | 8081 | 数据管理 |
| Meta Backend | 8082 | 8082 | 元数据服务 |
| Transfer Backend | 8083 | 8083 | 数据传输 |
| Orchestrator Backend | 8084 | 8084 | 工作流编排 |
| PostgreSQL | 5432 | 5432 | 系统数据库 |
| Redis | 6379 | 6379 | 缓存、事件和分布式锁 |
| MinIO API | 9000 | 9000 | 对象存储 |
| MinIO Console | 9001 | 9001 | MinIO 管理界面 |

### 📚 文档

- **[CLAUDE.md](CLAUDE.md)** - 完整项目架构和开发指南 (English)
- **[AGENTS.md](AGENTS.md)** - 项目工作原则、模块导航和开发约定
- **[scripts/README.md](scripts/README.md)** - Scripts 脚本使用指南
- **[docs/](docs/)** - 详细技术文档
  - [addp部署和开发步骤.md](docs/guide/addp部署和开发步骤.md) - 部署与开发启动指南
  - [addp配置介绍.md](docs/spec/addp配置介绍.md) - 配置分层、管理能力与环境变量规范

### 🤝 贡献

欢迎贡献！请遵循以下步骤：

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 创建 Pull Request

### 📝 许可证

除特别标明的目录外，本项目采用 MIT 许可证。`manager/frontend` 因链接 GPL-3.0 的 LibreDWG 浏览器转换器，其发行物采用 GPL-3.0-only；其他模块仍采用 MIT 许可证。详见仓库根目录和 `manager/frontend` 下的许可证说明。

### 🙏 致谢

感谢所有为 ADDP 做出贡献的开发者！

---

## English

### 📖 About

ADDP (All Domain Data Platform) is an enterprise-level data platform built on microservices architecture, providing comprehensive data management, metadata services, data transfer, and workflow orchestration solutions.

### ✨ Key Features

- 🏗️ **Microservices Architecture** - Modular design, independent deployment, elastic scaling
- 🔐 **Unified Authentication** - Opaque tokens, OAuth 2.0, AuthContext, and tenant isolation
- 📊 **Data Management** - Multi-source connections, data preview, metadata scanning
- 🔄 **Data Transfer** - Import/export/sync with incremental transfer support
- 🎯 **Workflow Orchestration** - Visual orchestration, task scheduling, monitoring
- 🗺️ **Spatial Data Support** - Complete GIS data processing capabilities
- 📱 **Console** (Control Panel) - Single sign-on, unified access to all modules

### 🚀 Quick Start

#### Option 1: Development Mode (Recommended for Development)

```bash
# 1. Clone the repository
git clone https://github.com/addp/addp.git
cd addp

# 2. Configure environment
cp .env.example .env
# Edit .env to update passwords

# 3. Start complete dev environment
bash scripts/dev/start.sh

# Access services
# - Console: http://localhost:5170
# - Gateway: http://localhost:8000
```

#### Option 2: Local Docker Deployment (Recommended for Testing)

```bash
# 1. Build images
make build
make build-images

# 2. Start platform
bash scripts/local/start.sh

# Access services
# - Console (Recommended): http://localhost:80
```

#### Option 3: Production Deployment

```bash
# 1. Build and package
make build BUILD_ARGS="--arch both"
IMAGE_TAG=v1.0.0 make build-images IMAGE_BUILD_ARGS=--multi-arch
bash scripts/build/package.sh --mode registry

# 2. Deploy to production
bash scripts/prod/start.sh

# 3. Health check
bash scripts/prod/health-check.sh
```

### 🛠️ Scripts Toolchain

Complete automation scripts for development, build, and deployment:

| Directory | Purpose | Core Scripts | Use Case |
|-----------|---------|--------------|----------|
| **scripts/infra/** | Infrastructure | `up.sh`, `down.sh`, `status.sh` | Manage PostgreSQL, Redis, MinIO |
| **scripts/dev/** | Development | `start.sh`, `stop.sh`, `restart.sh` | Daily development |
| **scripts/build/** | Build & Compile | `compile.sh`, `build-images.sh`, `package.sh` | Build releases |
| **scripts/local/** | Local Deploy | `start.sh`, `stop.sh`, `status.sh` | Local testing |
| **scripts/prod/** | Production | `start.sh`, `health-check.sh`, `swarm/` | Production deployment |

📚 **Documentation**: See [scripts/README.md](scripts/README.md) for complete guide

### 🏗️ Tech Stack

**Backend**: Go 1.23+, Gin, GORM, PostgreSQL 15, Redis 7 (cache/events), MinIO

**Frontend**: Vue 3, Vite, Element Plus, Pinia, OpenLayers

**Infrastructure**: Docker, Docker Compose, Nginx, Docker Swarm (optional)

### 🔑 IAM Bootstrap

ADDP does not create default administrator or tenant accounts. The initial system, security, and audit administrators are established only through the one-time offline IAM bootstrap process.

### 📚 Documentation

- **[CLAUDE.md](CLAUDE.md)** - Complete architecture and development guide (English)
- **[AGENTS.md](AGENTS.md)** - Project working principles, module navigation, and development conventions
- **[scripts/README.md](scripts/README.md)** - Scripts usage guide
- **[docs/](docs/)** - Detailed technical documentation

### 📝 License

Except where otherwise noted, this project is licensed under the MIT License. The distributed `manager/frontend` application is licensed under GPL-3.0-only because it links the GPL-3.0 LibreDWG browser converter; other modules remain MIT-licensed. See the repository root and `manager/frontend` license notices.

---

<div align="center">

**Made with ❤️ by ADDP Team**

[Documentation](docs/) · [Report Bug](https://github.com/addp/addp/issues) · [Request Feature](https://github.com/addp/addp/issues)

</div>
