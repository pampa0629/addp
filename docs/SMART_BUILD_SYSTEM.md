# 智能构建系统 (Smart Build System)

## 概述

ADDP 平台实现了基于**源代码修改时间追踪**的智能构建系统，可以自动检测代码变更并只重新构建发生变化的服务，大幅提升构建速度。

## 核心特性

### 1. 自动变更检测
- 扫描每个服务的源代码目录，获取最新文件修改时间
- 与上次构建时间比较，自动判断是否需要重新构建
- 支持前端（Vue/JS/TS/CSS）和后端（Go）源文件检测

### 2. 智能缓存管理
- 未变更的服务使用 Docker 缓存层，快速完成构建（秒级）
- 变更的服务使用 `--no-cache` 强制重新构建，确保使用最新代码
- 构建成功后更新时间戳缓存文件（`.build-cache/`目录）

### 3. 多架构支持
- 同时支持 AMD64 和 ARM64 架构
- 每个架构独立跟踪缓存状态
- 自动创建 multi-arch manifest 列表

## 使用方法

### 自动模式（推荐）

```bash
# 自动检测变更并智能构建
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182
```

**行为**：
- Portal.vue 修改了 → Portal 重新构建（使用 --no-cache）
- System backend 没变 → 使用缓存（快速）
- Meta frontend 修改了 → Meta 重新构建（使用 --no-cache）

### 强制重新构建所有服务

```bash
# 忽略缓存，强制重建所有服务
./scripts/deploy/1-build-images-multiarch.sh --force
```

## 工作原理

### 1. 源代码监控

每个服务配置了需要监控的源代码目录：

```bash
# 前端服务示例（Portal）
portal:portal/frontend:portal/frontend/src,portal/frontend/public,portal/frontend/package.json

# 后端服务示例（System）
system-backend:system/backend:system/backend/cmd,system/backend/internal,system/backend/pkg
```

### 2. 时间戳比较流程

```
1. 扫描源代码目录
   ↓
2. 获取最新文件修改时间（stat -f "%m"）
   ↓
3. 读取上次构建时间（.build-cache/service-arch.timestamp）
   ↓
4. 比较时间戳
   ├─ 源代码更新 → 使用 --no-cache 重新构建
   └─ 源代码未变 → 使用 Docker 缓存快速构建
   ↓
5. 构建成功后更新缓存时间戳
```

### 3. 缓存文件结构

```
.build-cache/
├── portal-amd64.timestamp          # Portal AMD64 上次构建时间
├── portal-arm64.timestamp          # Portal ARM64 上次构建时间
├── system-backend-amd64.timestamp
├── system-backend-arm64.timestamp
├── meta-frontend-amd64.timestamp
└── ...
```

## 构建输出说明

### 智能构建输出示例

```bash
========================================
[7/12] portal
========================================
Building portal for amd64... (source changed)
✓ Built and pushed portal (amd64) - REBUILT
Building portal for arm64... (source changed)
✓ Built and pushed portal (arm64) - REBUILT

========================================
[1/12] system-backend
========================================
Building system-backend for amd64... (using cache - no source changes)
✓ Built and pushed system-backend (amd64) - CACHED
```

**输出解读**：
- `(source changed)` - 源代码有变更，正在重新构建
- `(using cache - no source changes)` - 源代码未变，使用缓存
- `REBUILT` - 实际重新构建了镜像
- `CACHED` - 使用了 Docker 缓存层

### 构建总结

```bash
========================================
Build Summary
========================================
Rebuilt (source changed):
  ↻ portal
  ↻ meta-frontend

Cached (no changes):
  ⚡ system-backend
  ⚡ manager-backend
  ⚡ transfer-backend
  ⚡ gateway
  ⚡ system-frontend
  ⚡ manager-frontend
  ⚡ transfer-frontend
  ⚡ nginx

✓ All services built successfully for both architectures

Cache efficiency: 9 cached, 2 rebuilt
```

## 性能对比

### 传统构建（无智能缓存）
```
12 个服务 × 2 架构 = 24 次构建
每次构建 30-90 秒
总耗时: 约 15-30 分钟
```

### 智能构建（仅修改 Portal）
```
Portal 重新构建: 2 次（amd64 + arm64）× 60秒 = 2 分钟
其他 11 个服务使用缓存: 22 次 × 3秒 = 1 分钟
总耗时: 约 3 分钟
```

**提升**: 从 15-30 分钟 → 3 分钟（**5-10 倍速度提升**）

## 常见场景

### 场景 1: 修复前端 Bug（Portal.vue）

```bash
# 1. 修改代码
vim portal/frontend/src/views/Portal.vue

# 2. 智能部署（自动检测到 Portal 变更）
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182

# 结果：只重新构建 Portal，其他 11 个服务使用缓存
```

### 场景 2: 更新后端 API（System Backend）

```bash
# 1. 修改代码
vim system/backend/internal/api/user.go

# 2. 重新编译 Go 二进制
./scripts/deploy/0-compile-binaries.sh --arch both

# 3. 智能部署
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182

# 结果：只重新构建 System backend，其他服务使用缓存
```

### 场景 3: 大版本发布（强制重建所有）

```bash
# 忽略缓存，重新构建所有服务
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182 --force

# 或直接调用构建脚本
./scripts/deploy/1-build-images-multiarch.sh --force
```

## 清除缓存

### 清除所有构建缓存

```bash
# 删除构建时间戳缓存
rm -rf .build-cache/

# 下次构建时会作为首次构建处理（使用 --no-cache）
```

### 清除特定服务缓存

```bash
# 只清除 Portal 的缓存
rm .build-cache/portal-*

# 下次构建时 Portal 会重新构建，其他服务仍使用缓存
```

## 故障排查

### 问题：修改了代码但构建仍使用缓存

**可能原因**：
1. 修改的文件不在监控目录内
2. 文件修改时间未更新（复制粘贴可能保留旧时间戳）
3. 缓存文件时间戳异常

**解决方法**：
```bash
# 方法 1: 强制重建该服务
rm .build-cache/portal-*

# 方法 2: 强制重建所有服务
./scripts/deploy/1-build-images-multiarch.sh --force

# 方法 3: 检查监控目录配置
grep "portal:" scripts/deploy/1-build-images-multiarch.sh
```

### 问题：构建脚本报错 "local: can only be used in a function"

**原因**: Bash 脚本 bug（已在最新版本修复）

**解决**: 更新到最新版本脚本

### 问题：想要查看完整构建日志

```bash
# 将完整日志保存到文件
./scripts/deploy/deploy-all.sh --server pampa@192.168.1.182 2>&1 | tee /tmp/deploy.log

# 查看日志
less /tmp/deploy.log
```

## 技术实现细节

### 文件修改时间获取（macOS）

```bash
# macOS 使用 stat -f
stat -f "%m" file.vue

# Linux 使用 stat -c
stat -c "%Y" file.vue
```

**注意**: 当前实现使用 macOS 格式，如需 Linux 支持需修改 `get_source_mtime()` 函数。

### 监控文件类型

| 服务类型 | 监控文件扩展名 |
|---------|---------------|
| 前端 | `.vue`, `.js`, `.ts`, `.json`, `.css`, `.html` |
| 后端 | `.go` |
| Nginx | `.conf`, `Dockerfile` |

### 缓存时间戳格式

```bash
# Unix timestamp (秒)
$ cat .build-cache/portal-amd64.timestamp
1730546400

# 转换为人类可读格式
$ date -r $(cat .build-cache/portal-amd64.timestamp)
Sat Nov  2 13:20:00 CST 2025
```

## 最佳实践

### ✅ 推荐做法

1. **日常开发**: 总是使用智能构建，让系统自动检测变更
2. **版本发布**: 使用 `--force` 确保所有服务都是最新构建
3. **定期清理**: 每月清理一次 `.build-cache/` 避免缓存失效问题

### ❌ 避免做法

1. **不要手动修改** `.build-cache/` 中的时间戳文件
2. **不要提交** `.build-cache/` 到 Git（已添加到 .gitignore）
3. **不要依赖缓存** 进行关键发布（使用 `--force` 更安全）

## 未来改进方向

### 计划中的功能

1. **Linux 支持**: 自动检测操作系统并使用正确的 stat 命令
2. **依赖追踪**: 检测 package.json 和 go.mod 变更
3. **并行构建**: 同时构建 AMD64 和 ARM64 镜像（需要 Docker Buildx）
4. **增量构建报告**: 生成详细的构建时间对比报告
5. **CI/CD 集成**: 与 GitHub Actions 集成，自动化部署流程

### 性能优化方向

1. **更细粒度的缓存**: 基于文件哈希而非时间戳
2. **共享基础镜像**: 提取公共依赖到单独的基础镜像
3. **远程缓存**: 支持从 Registry 拉取缓存层

## 相关文档

- [部署脚本说明](./DEPLOY_SUMMARY.md)
- [多架构镜像构建](./MULTI_ARCH_IMAGES.md)
- [Docker Registry 使用](./REGISTRY_QUICK_REFERENCE.md)
- [环境变量管理](./FIX_ENV_PLACEHOLDERS.md)

## 总结

智能构建系统通过**自动检测源代码变更**和**智能缓存管理**，将日常开发中的构建时间从 15-30 分钟缩短到 3-5 分钟，**大幅提升开发效率**。

**关键优势**：
- ✅ **自动化**: 无需手动指定哪些服务需要重建
- ✅ **高效**: 只重建变更的服务，其他使用缓存
- ✅ **可靠**: 提供 `--force` 选项确保完整重建
- ✅ **透明**: 清晰的构建输出显示哪些服务被重建/缓存

**使用建议**：
- 日常开发 → 使用智能构建（自动检测）
- 版本发布 → 使用 `--force` 完整重建
- 遇到问题 → 清除缓存重试

## 自动清理机制（Auto-Cleanup）

### 清理内容

智能构建系统在每次运行时**自动**执行以下清理操作：

#### 1. 旧部署包清理
- **策略**: 保留最新 3 个部署包，删除更早的包
- **文件模式**: `addp-deploy-*.tar.gz`
- **好处**: 防止磁盘空间被大量旧包占用（每个包约 100-200MB）

#### 2. Docker 悬空镜像清理
- **目标**: 清理标记为 `<none>` 的悬空镜像
- **限制**: 每次最多清理 20 个（避免长时间阻塞）
- **触发**: 镜像构建过程中产生的中间层和被覆盖的镜像

### 清理输出示例

```bash
Checking for old deployment packages...
Found 5 old package(s), removing...
✓ Cleaned old packages

Cleaning 3 dangling Docker image(s)...
✓ Cleaned dangling images

========================================
ADDP Smart Multi-Arch Builder
========================================
```

### 为什么需要自动清理？

1. **避免磁盘空间浪费**
   - 每次部署生成新的 tar.gz 包（~150MB）
   - Docker 构建产生大量悬空镜像（可达数GB）
   - 长期积累会占用大量磁盘空间

2. **简化操作流程**
   - 无需手动清理
   - 无需额外的命令选项
   - 自动维护健康的构建环境

3. **支持重复部署**
   - 可以多次运行构建脚本而不担心磁盘空间
   - 适合频繁开发和测试场景

### 手动清理（如需深度清理）

虽然自动清理已覆盖大部分场景，但你仍可手动执行深度清理：

```bash
# 清除所有构建缓存（强制所有服务重新构建）
rm -rf .build-cache/

# 清除所有 Go 二进制文件
find . -name "server-*" -o -name "worker-*" | xargs rm -f

# 清除所有部署包
rm -f addp-deploy-*.tar.gz

# 深度清理 Docker（谨慎使用）
docker builder prune -f      # 清理构建缓存
docker image prune -a -f     # 清理所有未使用的镜像
```

**注意**: 深度清理后，下次构建需要重新下载依赖和构建所有镜像，耗时较长。
