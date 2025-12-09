# ADDP 本地 Docker 部署脚本

本目录包含用于本地 Docker 部署测试的脚本。

## 目录结构

```
scripts/local/
├── README.md        # 本文件
├── start.sh         # 启动本地 Docker 环境（基础设施 + 应用服务）
├── stop.sh          # 停止本地 Docker 环境
└── restart.sh       # 重启本地 Docker 环境
```

## 核心原则

遵循以下7个原则(与 scripts/infra/ 和 scripts/dev/ 一致):

1. **单一职责**: 同样功能只在一处实现
2. **适应性**: 自动适配不同环境
3. **清晰明了**: 一看就懂的结构和命名
4. **可重复执行**: 幂等性,多次执行不会破坏系统
5. **易用性**: 用户无需了解技术细节
6. **分散和集中**: 模块配置分散,脚本集中在 scripts/
7. **敢于删除**: 删除重复或无用内容

## 使用方法

### 快速启动

```bash
# 一键启动完整本地 Docker 环境
bash scripts/local/start.sh

# 访问服务
# - Portal: http://localhost:8000
# - System Backend: http://localhost:8080
# - Manager Backend: http://localhost:8081
# - PostgreSQL: localhost:5433
```

### 脚本说明

#### start.sh

**功能**: 启动完整本地 Docker 环境

**执行步骤**:
1. 检查 Docker 是否运行
2. 启动基础设施 (docker-compose.infra.yml)
3. 启动应用服务 (docker-compose.app.yml)
4. 等待所有服务就绪
5. 显示访问地址

**特性**:
- ✅ 自动检查端口占用
- ✅ 健康检查等待
- ✅ 友好的错误提示
- ✅ 幂等性(可重复执行)

#### stop.sh

**功能**: 停止所有本地 Docker 服务

**执行步骤**:
1. 停止应用服务 (docker-compose.app.yml)
2. 停止基础设施 (docker-compose.infra.yml)
3. 清理孤立容器

**特性**:
- ✅ 优雅停止(保留数据卷)
- ✅ 可选参数 `--rm` 删除数据卷

#### restart.sh

**功能**: 重启所有服务

**实现**: stop.sh + start.sh

## 与其他部署模式对比

| 部署模式 | 脚本目录 | 用途 | 运行方式 |
|---------|---------|------|---------|
| **开发模式** | scripts/dev/ | 本地开发调试 | 直接运行 Go/npm |
| **本地 Docker** | scripts/local/ | 本地容器化测试 | Docker Compose (本地镜像) |
| **生产环境** | scripts/prod/ | 服务器部署 | Docker Compose (Registry 镜像) |

## 依赖关系

```
scripts/build/compile.sh    # 编译二进制文件
        ↓
docker-compose.app.yml      # 使用 Dockerfile.prebuilt 构建镜像
        ↓
scripts/local/start.sh      # 启动本地 Docker 环境
```

## 故障排查

### Docker 未运行

```bash
# 检查 Docker 状态
docker info

# macOS: 启动 Docker Desktop
open -a Docker

# Linux: 启动 Docker 服务
sudo systemctl start docker
```

### 端口被占用

```bash
# 查看端口占用
lsof -i :5433  # PostgreSQL
lsof -i :8080  # System Backend

# 停止占用进程或修改 .env 端口配置
```

### 容器启动失败

```bash
# 查看容器日志
docker compose -f docker-compose.app.yml logs system-backend

# 查看所有容器状态
docker compose -f docker-compose.app.yml ps
docker compose -f docker-compose.infra.yml ps
```

## 注意事项

1. **数据持久化**: 使用 Docker volumes,数据不会丢失
2. **镜像更新**: 修改代码后需要重新编译和构建镜像
3. **资源消耗**: Docker 容器会占用更多内存,建议至少 8GB RAM
4. **网络隔离**: 所有服务使用 addp-network 网络

## 相关文档

- [scripts/build/README.md](../build/README.md) - 编译和打包脚本
- [scripts/infra/README.md](../infra/README.md) - 基础设施脚本
- [scripts/dev/README.md](../dev/README.md) - 开发模式脚本
- [CLAUDE.md](../../CLAUDE.md) - 项目整体架构
