# 多架构镜像存储指南

## 📦 多架构镜像原理

### 什么是多架构镜像？

一个镜像标签（如 `latest`）可以同时包含多个 CPU 架构的镜像版本。Docker 会根据运行环境自动选择合适的架构。

### 存储结构

```
Registry: localhost:5001
│
└─ addp-system-backend:latest
    ├─ Manifest List (清单索引)
    │   ├─ linux/amd64  → sha256:abc123...
    │   └─ linux/arm64  → sha256:def456...
    │
    ├─ 镜像数据 (linux/amd64)
    │   ├─ Layer 1
    │   ├─ Layer 2
    │   └─ Layer 3
    │
    └─ 镜像数据 (linux/arm64)
        ├─ Layer 1
        ├─ Layer 2
        └─ Layer 3
```

---

## ✅ 推送多架构镜像

### 方法一：一次性推送多架构（推荐）

```bash
# 同时构建并推送 x86 和 ARM 镜像
./scripts/push-to-local-registry-multiarch.sh 5001

# 脚本默认配置：
# TARGET_PLATFORM="linux/amd64,linux/arm64"
```

**优点：**
- ✅ 一次推送，支持所有平台
- ✅ ARM Mac 和 Intel 服务器都能直接拉取使用
- ✅ 镜像标签统一，管理简单

**存储空间：**
- 大约是单架构的 **1.8-2 倍**（不同架构的二进制文件大小不同）
- 基础层可能共享，实际占用小于 2 倍

---

### 方法二：分别推送不同架构

```bash
# 仅推送 x86 镜像（用于 Intel 服务器）
TARGET_PLATFORM=linux/amd64 ./scripts/push-to-local-registry-multiarch.sh 5001

# 稍后再推送 ARM 镜像（会合并到同一标签）
TARGET_PLATFORM=linux/arm64 ./scripts/push-to-local-registry-multiarch.sh 5001
```

**Docker Buildx 会自动合并**：
- 第一次推送：创建 Manifest List，包含 `linux/amd64`
- 第二次推送：更新 Manifest List，添加 `linux/arm64`
- 结果：同一标签包含两种架构

---

## 🔍 验证多架构镜像

### 查看镜像支持的架构

```bash
# 使用 docker buildx imagetools
docker buildx imagetools inspect localhost:5001/addp-system-backend:latest

# 输出示例
Name:      localhost:5001/addp-system-backend:latest
MediaType: application/vnd.docker.distribution.manifest.list.v2+json
Digest:    sha256:1234567890abcdef...

Manifests:
  Name:      localhost:5001/addp-system-backend:latest@sha256:abc123...
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/amd64  ← 支持 x86

  Name:      localhost:5001/addp-system-backend:latest@sha256:def456...
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/arm64  ← 支持 ARM
```

### 使用 curl 查看（底层 API）

```bash
# 查看 Manifest List
curl -s -H "Accept: application/vnd.docker.distribution.manifest.list.v2+json" \
  http://localhost:5001/v2/addp-system-backend/manifests/latest | jq

# 输出
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.docker.distribution.manifest.list.v2+json",
  "manifests": [
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "size": 1234,
      "digest": "sha256:abc123...",
      "platform": {
        "architecture": "amd64",
        "os": "linux"
      }
    },
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "size": 1234,
      "digest": "sha256:def456...",
      "platform": {
        "architecture": "arm64",
        "os": "linux"
      }
    }
  ]
}
```

---

## 🚀 实际使用场景

### 场景 1: 统一镜像，多环境部署

**一次构建，到处运行：**

```bash
# ========== 开发机（ARM Mac）==========
# 推送多架构镜像
./scripts/push-to-local-registry-multiarch.sh 5001

# ========== Intel 服务器 ==========
docker pull localhost:5001/addp-system-backend:latest
# 自动拉取 linux/amd64 版本

# ========== ARM 服务器（如树莓派）==========
docker pull localhost:5001/addp-system-backend:latest
# 自动拉取 linux/arm64 版本
```

**优点：**
- ✅ 部署脚本完全相同
- ✅ 无需关心服务器架构
- ✅ 镜像标签统一管理

---

### 场景 2: 仅推送需要的架构（节省空间）

**如果只有 Intel 服务器：**

```bash
# 仅构建 x86 镜像
TARGET_PLATFORM=linux/amd64 ./scripts/push-to-local-registry-multiarch.sh 5001
```

**如果只有 ARM 服务器：**

```bash
# 仅构建 ARM 镜像
TARGET_PLATFORM=linux/arm64 ./scripts/push-to-local-registry-multiarch.sh 5001
```

---

## 📊 存储空间对比

### 单架构 vs 多架构

| 镜像内容 | 单架构大小 | 双架构大小 | 说明 |
|---------|----------|----------|------|
| 基础镜像层（Alpine） | 5MB | 5MB | 共享 |
| Go 二进制文件 | 20MB | 38MB | 不同架构编译产物不同 |
| 依赖库 | 10MB | 18MB | 部分共享 |
| **总计** | **35MB** | **~60MB** | 约 1.7 倍 |

**实际测试（ADDP system-backend）：**
- 仅 amd64: ~45MB
- 仅 arm64: ~42MB
- 双架构: ~75MB（不是简单的 45+42，因为有共享层）

---

## 🔧 管理多架构镜像

### 查看 Registry 中的所有镜像

```bash
# 列出所有镜像
curl http://localhost:5001/v2/_catalog | jq

# 查看特定镜像的标签
curl http://localhost:5001/v2/addp-system-backend/tags/list | jq
```

### 删除特定架构的镜像

**注意**：Docker Registry 不支持直接删除特定架构，只能删除整个标签。

如果需要删除某个架构：
1. 重新推送仅包含需要的架构
2. 或使用 Registry API 手动编辑 Manifest

```bash
# 示例：重新推送仅 amd64
TARGET_PLATFORM=linux/amd64 ./scripts/push-to-local-registry-multiarch.sh 5001
```

### 清理未使用的镜像层

```bash
# 在 Registry 容器中执行垃圾回收
docker exec addp-registry bin/registry garbage-collect /etc/docker/registry/config.yml

# 或重启 Registry（自动清理）
docker restart addp-registry
```

---

## 🎯 推荐配置

### 对于你的场景（ARM Mac + Intel 服务器）

**方案 A：推送双架构（推荐）**

```bash
# 一次性支持所有平台
./scripts/push-to-local-registry-multiarch.sh 5001
# 默认: TARGET_PLATFORM=linux/amd64,linux/arm64
```

**优点：**
- ✅ 开发机本地测试（ARM）和服务器部署（x86）都可用
- ✅ 未来增加 ARM 服务器无需重新构建
- ✅ 与 Docker Hub 官方镜像一致的体验

**缺点：**
- ⚠️  构建时间约 2 倍（并行构建）
- ⚠️  Registry 存储空间约 1.7 倍

---

**方案 B：仅推送 x86（节省资源）**

```bash
# 仅构建服务器需要的架构
TARGET_PLATFORM=linux/amd64 ./scripts/push-to-local-registry-multiarch.sh 5001
```

**优点：**
- ✅ 构建速度快（仅单架构）
- ✅ 节省存储空间

**缺点：**
- ❌ 开发机无法直接拉取测试（架构不匹配）
- ❌ 添加 ARM 服务器需要重新构建

---

## 💡 最佳实践

### 1. 使用多架构镜像的场景

- ✅ 开发团队有不同架构的开发机（M1 Mac + Intel Mac）
- ✅ 部署环境多样化（云服务器 x86 + 边缘设备 ARM）
- ✅ 希望统一镜像标签，简化管理

### 2. 使用单架构镜像的场景

- ✅ 生产环境架构单一（仅 x86 或仅 ARM）
- ✅ 存储空间有限
- ✅ 构建时间敏感

### 3. 验证清单

部署前确认：

```bash
# 1. 查看镜像支持的架构
docker buildx imagetools inspect localhost:5001/addp-system-backend:latest

# 2. 在目标服务器验证
ssh user@server
docker pull localhost:5001/addp-system-backend:latest
docker inspect addp-system-backend:latest | grep Architecture

# 3. 确认匹配
# Intel 服务器应显示: "Architecture": "amd64"
# ARM 服务器应显示: "Architecture": "arm64"
```

---

## 📝 常见问题

### Q1: 推送多架构镜像会覆盖之前的单架构镜像吗？

**A:** 不会覆盖，而是**合并**。

```bash
# 第一次推送 amd64
TARGET_PLATFORM=linux/amd64 ./script.sh
# Registry 中: latest -> [amd64]

# 第二次推送 arm64
TARGET_PLATFORM=linux/arm64 ./script.sh
# Registry 中: latest -> [amd64, arm64]  ← 合并
```

---

### Q2: 如何只删除某个架构的镜像？

**A:** Docker Registry 原生不支持删除单个架构。

**变通方法**：
```bash
# 重新推送仅包含需要的架构
TARGET_PLATFORM=linux/amd64 ./script.sh
# 这会替换整个 Manifest List
```

---

### Q3: 拉取镜像时可以强制指定架构吗？

**A:** 可以，使用 `--platform` 参数。

```bash
# 在 ARM Mac 上强制拉取 x86 镜像
docker pull --platform linux/amd64 localhost:5001/addp-system-backend:latest

# 但运行时会报错（架构不匹配）
docker run localhost:5001/addp-system-backend:latest
# exec format error
```

---

### Q4: 多架构镜像构建速度慢怎么办？

**A:** 使用并行构建和缓存优化。

```bash
# 脚本已默认使用 Buildx 并行构建
# 进一步优化：

# 1. 启用缓存
export BUILDKIT_INLINE_CACHE=1

# 2. 使用本地缓存后端
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --cache-from type=local,src=/tmp/buildx-cache \
  --cache-to type=local,dest=/tmp/buildx-cache \
  ...
```

---

## 🎓 高级技巧

### 查看镜像层差异

```bash
# 分别拉取两种架构
docker pull --platform linux/amd64 localhost:5001/addp-system-backend:latest
docker pull --platform linux/arm64 localhost:5001/addp-system-backend:latest

# 比较镜像 ID
docker images | grep addp-system-backend
# 会看到两个不同的 IMAGE ID

# 查看层详情
docker history localhost:5001/addp-system-backend:latest
```

### 使用 Docker Manifest 手动操作

```bash
# 创建 Manifest List
docker manifest create localhost:5001/addp-system-backend:latest \
  localhost:5001/addp-system-backend:latest-amd64 \
  localhost:5001/addp-system-backend:latest-arm64

# 推送 Manifest List
docker manifest push localhost:5001/addp-system-backend:latest
```

---

## 📚 参考资料

- [Docker Manifest 官方文档](https://docs.docker.com/engine/reference/commandline/manifest/)
- [Buildx 多平台构建](https://docs.docker.com/build/building/multi-platform/)
- [OCI Image Manifest Specification](https://github.com/opencontainers/image-spec/blob/main/manifest.md)

---

## ✅ 总结

**回答你的问题：是的，镜像仓库可以同时存在两个 CPU 架构的镜像！**

**推荐配置（你的场景）：**
```bash
# 推送双架构镜像（默认）
./scripts/push-to-local-registry-multiarch.sh 5001

# 结果：
# - ARM Mac 本地测试 ✅
# - Intel 服务器部署 ✅
# - 统一标签管理 ✅
```

**验证：**
```bash
docker buildx imagetools inspect localhost:5001/addp-system-backend:latest
# 应该看到 linux/amd64 和 linux/arm64 两个 Platform
```
