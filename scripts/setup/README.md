# Setup 脚本说明

本目录包含 ADDP 系统的初始化和配置脚本，用于项目首次部署和环境准备。

## 📋 脚本清单

### Registry 管理（本地镜像仓库）

#### `local-registry.sh`
**用途**: 初始化本地 Docker Registry

**功能**:
- 检查并释放端口（默认 5000）
- 创建 Registry 数据目录和配置文件
- 启动 Registry 容器（registry:2）
- 生成详细的使用说明

**使用场景**: 首次设置本地镜像仓库

**命令**:
```bash
./scripts/setup/local-registry.sh [port]
# 示例：使用默认端口 5000
./scripts/setup/local-registry.sh

# 示例：使用自定义端口 5001
./scripts/setup/local-registry.sh 5001
```

#### `start-registry.sh`
**用途**: 启动或重启已存在的 Registry

**功能**:
- 检查容器运行状态
- 智能去重（已运行则跳过）
- 健康检查（10次重试）
- 快速启动无需重新配置

**使用场景**: 日常启动 Registry

**命令**:
```bash
./scripts/setup/start-registry.sh
```

#### `configure-docker-registry.sh`
**用途**: 配置 Docker daemon 信任本地 Registry

**功能**:
- 检测操作系统（macOS / Linux）
- macOS: 提供手动配置步骤（需要 Docker Desktop）
- Linux: 自动修改 /etc/docker/daemon.json
- 测试 Registry 连接

**使用场景**: 首次设置或更换 Registry 地址

**命令**:
```bash
# 配置信任本地 Registry
./scripts/setup/configure-docker-registry.sh localhost:5001

# 配置信任局域网 Registry
./scripts/setup/configure-docker-registry.sh 192.168.1.100:5001
```

#### `check-registry.sh`
**用途**: 检查 Registry 状态和镜像列表

**功能**:
- 验证容器是否运行
- 健康检查
- 列出已存储的镜像
- 彩色输出状态信息

**使用场景**: 诊断 Registry 问题或查看镜像

**命令**:
```bash
./scripts/setup/check-registry.sh
```

---

### 基础镜像管理

#### `pull-base-images.sh`
**用途**: 预拉取所有基础 Docker 镜像

**功能**:
- 拉取 PostgreSQL、Redis、MinIO 等基础设施镜像
- 拉取 Golang、Node.js 等构建镜像
- 支持推送到本地 Registry
- 加速后续构建和部署

**使用场景**: 离线部署准备或网络优化

**命令**:
```bash
# 仅拉取镜像
./scripts/setup/pull-base-images.sh

# 拉取并推送到本地 Registry
./scripts/setup/pull-base-images.sh --push-to-registry
```

---

### 前端标准化

#### `standardize-frontend-docker.sh`
**用途**: 统一所有前端模块的 Dockerfile 和 nginx.conf

**功能**:
- 检查所有前端 Dockerfile 的一致性
- 验证 nginx.conf 配置规范
- 自动修复不符合标准的配置
- 确保所有前端使用相同的构建模式

**使用场景**: 前端 Docker 配置审查和修复

**命令**:
```bash
./scripts/setup/standardize-frontend-docker.sh
```

---

### MinIO 初始化

#### `init-minio-mvt.sh`
**用途**: 初始化 MinIO MVT 瓦片存储

**功能**:
- 创建 MVT 瓦片专用 bucket
- 配置访问权限
- 设置生命周期策略

**使用场景**: Meta 模块 MVT 瓦片功能初始化

**命令**:
```bash
./scripts/setup/init-minio-mvt.sh
```

---

## 🔄 典型工作流

### 首次部署流程

```bash
# 1. 设置本地 Registry（可选，用于离线部署）
./scripts/setup/local-registry.sh
./scripts/setup/configure-docker-registry.sh localhost:5001

# 2. 拉取基础镜像（可选，加速部署）
./scripts/setup/pull-base-images.sh

# 3. 初始化 MinIO MVT 存储（如需使用 Meta 模块）
./scripts/setup/init-minio-mvt.sh

# 4. 前端标准化检查（开发时使用）
./scripts/setup/standardize-frontend-docker.sh
```

### 日常开发流程

```bash
# 启动本地 Registry
./scripts/setup/start-registry.sh

# 检查 Registry 状态
./scripts/setup/check-registry.sh
```

---

## ⚠️ 注意事项

1. **Registry 脚本**
   - `local-registry.sh` 用于首次初始化
   - `start-registry.sh` 用于日常启动
   - 不要重复运行初始化脚本

2. **环境配置**
   - 使用 `.env.example` 作为模板创建 `.env` 文件
   - `.env` 文件包含敏感信息，不要提交到 Git
   - 修改配置后需要重启相关服务

3. **权限要求**
   - Linux 系统某些脚本需要 sudo 权限
   - macOS 系统配置 Docker daemon 需要手动操作

4. **幂等性**
   - 所有脚本支持重复执行
   - 已存在的配置会被保留或提示确认覆盖

---

## 🔗 相关文档

- [基础设施管理](../infra/README.md) - 启动 PostgreSQL、Redis、MinIO
- [开发环境](../dev/README.md) - 本地开发启动流程
- [构建打包](../build/README.md) - 编译和镜像构建
- [生产部署](../prod/README.md) - 生产环境部署流程

---

## 📞 故障排查

如遇到问题，请参考：
- [调试脚本](../debug/README.md) - 常见问题诊断
- [测试脚本](../test/README.md) - 验证配置正确性

---

## 🗑️ 已删除的设置脚本

以下脚本已被删除：

- ~~`generate-env.sh`~~ - 未集成到主流程，请直接复制 `.env.example` 为 `.env` 并手动配置
- ~~`setup-high-performance-import.sh`~~ - 包含硬编码路径，功能应整合到 Transfer 模块中
