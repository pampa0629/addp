# 构建产物大小分析

## 非容器化部署

### 后端 (Go Binary)
- **文件**: `bin/server`
- **大小**: **14 MB**
- **说明**: 单个可执行文件，包含所有依赖

### 前端 (Static Files)
- **目录**: `frontend/dist`
- **大小**: **1.5 MB**
- **内容**:
  - HTML: 4 KB
  - CSS: ~343 KB (压缩后 ~47 KB gzip)
  - JavaScript: ~1,172 KB (压缩后 ~378 KB gzip)
  - 其他资源文件

### 数据库
- **文件**: `data/system.db` (SQLite)
- **大小**: 动态增长，初始几乎为 0
- **说明**: 随着数据增加而增长

### 总计（非容器化）
```
后端:    14 MB
前端:   1.5 MB
数据:     0 MB (初始)
-----------------------
总计:  ~15.5 MB
```

## 容器化部署（估算）

### 后端镜像
**基础镜像**: `golang:1.21-alpine` (构建) + `alpine:latest` (运行)
```
Alpine Linux 基础镜像:  ~7 MB
Go 编译的二进制文件:    14 MB
SQLite 运行时库:        ~1 MB
-----------------------
预计后端镜像大小:      ~25-30 MB
```

### 前端镜像
**基础镜像**: `node:18-alpine` (构建) + `nginx:alpine` (运行)
```
Nginx Alpine 镜像:     ~40 MB
前端静态文件:          1.5 MB
Nginx 配置:            <1 KB
-----------------------
预计前端镜像大小:      ~45-50 MB
```

### 数据卷
```
SQLite 数据库:  动态增长
-----------------------
预计初始大小:    <1 MB
```

### 总计（容器化）
```
后端镜像:    25-30 MB
前端镜像:    45-50 MB
数据卷:      <1 MB
-----------------------
总计:       ~70-80 MB
```

## 对比分析

| 项目 | 非容器化 | 容器化 | 差异 |
|-----|---------|--------|------|
| 后端 | 14 MB | 25-30 MB | +11-16 MB (基础镜像) |
| 前端 | 1.5 MB | 45-50 MB | +43.5-48.5 MB (Nginx 镜像) |
| 数据 | 动态 | 动态 | 相同 |
| **总计** | **~15.5 MB** | **~70-80 MB** | **+54.5-64.5 MB** |

## 优化建议

### 非容器化优化
1. ✅ Go 已使用默认编译，可考虑 `go build -ldflags="-s -w"` 进一步压缩（减少 30%）
2. ✅ 前端已压缩和 tree-shaking，可考虑代码分割进一步优化
3. ✅ 使用 SQLite，无需额外数据库服务

### 容器化优化
1. **多阶段构建** - 已在 Dockerfile 中使用，减少最终镜像大小
2. **Alpine 基础镜像** - 已使用最小化 Linux 发行版
3. **压缩层** - Docker 镜像层已自动压缩
4. 可考虑使用 `scratch` 或 `distroless` 进一步减小后端镜像

## 实际构建命令

### 非容器化构建
```bash
# 后端
cd backend
go build -o ../bin/server ./cmd/server/main.go

# 前端
cd frontend
npm run build
```

### 容器化构建
```bash
# 确保 Docker 守护进程运行
docker compose build

# 或单独构建
docker build -t addp-system-backend ./backend
docker build -t addp-system-frontend ./frontend
```

### 查看实际 Docker 镜像大小
```bash
docker images | grep addp-system
```

## 部署建议

### 小规模部署 (< 100 用户)
- **推荐**: 非容器化
- **原因**: 占用空间小，部署简单，资源占用少
- **方式**: 直接运行二进制文件 + Nginx 托管静态文件

### 中大规模部署 (> 100 用户)
- **推荐**: 容器化
- **原因**: 便于扩展、版本管理、持续部署
- **方式**: Docker Compose 或 Kubernetes

### 资源受限环境
- **推荐**: 非容器化
- **原因**: 磁盘和内存占用最小化
- **额外优化**:
  ```bash
  # Go 编译时压缩
  go build -ldflags="-s -w" -o server ./cmd/server/main.go
  # 预计减少至 ~10 MB
  ```

## 网络传输考虑

### 下载时间估算 (100 Mbps 网络)
```
非容器化 (~15.5 MB):  约 1.2 秒
容器化 (~75 MB):      约 6 秒
```

### CI/CD 构建时间
```
非容器化:  Go 编译 ~30 秒 + 前端构建 ~20 秒 = 50 秒
容器化:    Docker 构建 ~3-5 分钟 (首次)
           Docker 构建 ~1-2 分钟 (增量)
```

## 生产环境实际测试

若需要获取 Docker 镜像的准确大小，请执行：
```bash
# 启动 Docker Desktop (macOS)
# 或启动 Docker 服务 (Linux)
open -a Docker

# 等待 Docker 启动后执行
docker compose build
docker images | grep addp-system
```