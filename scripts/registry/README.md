# Registry 脚本说明

本目录包含 Docker Registry 管理脚本，用于离线部署和镜像管理。

## 📋 脚本清单

### init.sh
**用途**: 初始化本地 Docker Registry

**功能**:
- 检查并释放端口（默认 5000）
- 创建 Registry 数据目录和配置文件
- 启动 Registry 容器（registry:2）
- 生成详细的使用说明

**使用场景**: 首次设置本地镜像仓库

**命令**:
```bash
./scripts/registry/init.sh [port]
# 示例：使用默认端口 5000
./scripts/registry/init.sh

# 示例：使用自定义端口 5001
./scripts/registry/init.sh 5001
```

---

### start.sh
**用途**: 启动或重启已存在的 Registry

**功能**:
- 检查容器运行状态
- 智能去重（已运行则跳过）
- 健康检查（10次重试）
- 快速启动无需重新配置

**使用场景**: 日常启动 Registry

**命令**:
```bash
./scripts/registry/start.sh
```

---

### check.sh
**用途**: 检查 Registry 状态和镜像列表

**功能**:
- 验证容器是否运行
- 健康检查
- 列出已存储的镜像
- 彩色输出状态信息

**使用场景**: 诊断 Registry 问题或查看镜像

**命令**:
```bash
./scripts/registry/check.sh
```

---

### configure.sh
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
./scripts/registry/configure.sh localhost:5001

# 配置信任局域网 Registry
./scripts/registry/configure.sh 192.168.1.100:5001
```

---

## 🔄 典型工作流

### 首次设置

```bash
# 1. 初始化 Registry
./scripts/registry/init.sh

# 2. 配置 Docker 信任
./scripts/registry/configure.sh localhost:5001

# 3. 验证状态
./scripts/registry/check.sh
```

### 日常使用

```bash
# 启动 Registry
./scripts/registry/start.sh

# 检查状态
./scripts/registry/check.sh

# 推送镜像
docker tag my-image:latest localhost:5001/my-image:latest
docker push localhost:5001/my-image:latest
```

---

## ⚠️ 注意事项

1. **端口配置**
   - 默认使用 5000 端口
   - 如需修改，init.sh 接受端口参数
   - 修改后需要重新配置 Docker daemon 信任

2. **权限要求**
   - Linux 系统修改 daemon.json 需要 sudo 权限
   - macOS 系统需要手动在 Docker Desktop 中配置

3. **数据持久化**
   - Registry 数据存储在 `~/.addp-registry/data/`
   - 配置文件存储在 `~/.addp-registry/config.yml`

4. **幂等性**
   - init.sh 可以重复运行（会跳过已存在的资源）
   - start.sh 会检测已运行的容器

---

## 🔗 相关文档

- [构建脚本](../build/README.md) - 镜像构建和打包
- [生产部署](../prod/README.md) - 生产环境使用 Registry

---

## 📞 故障排查

### 问题 1: Registry 启动失败

**症状**: 容器无法启动

**诊断**:
```bash
docker logs addp-registry
```

**解决**:
- 检查端口是否被占用: `lsof -i :5000`
- 检查数据目录权限: `ls -la ~/.addp-registry/`

### 问题 2: 无法推送镜像

**症状**: `http: server gave HTTP response to HTTPS client`

**解决**:
```bash
# 重新配置信任
./scripts/registry/configure.sh localhost:5001

# macOS 用户: 在 Docker Desktop 中手动添加:
# Settings → Docker Engine → insecure-registries: ["localhost:5001"]
```

### 问题 3: 镜像列表为空

**症状**: check.sh 显示没有镜像

**原因**: 可能还没有推送任何镜像

**解决**:
```bash
# 推送测试镜像
docker pull alpine
docker tag alpine localhost:5001/alpine:latest
docker push localhost:5001/alpine:latest

# 再次检查
./scripts/registry/check.sh
```
