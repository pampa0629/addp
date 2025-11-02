# 推送脚本使用指南

## 功能说明

`push-to-local-registry.sh` 脚本支持**智能参数解析**，可以灵活指定 Registry 地址和端口。

---

## 使用方法

### 1️⃣ 无参数（自动检测）

```bash
./scripts/push-to-local-registry.sh
```

**行为：**
- 自动检测本机运行的 `addp-registry` 容器
- 如果检测到，使用容器实际端口
- 如果未检测到，默认使用 `localhost:5000`

**输出示例：**
```
ℹ️  自动检测到 Registry 运行在端口 5001
目标 Registry: localhost:5001
```

---

### 2️⃣ 指定端口号（推荐）

```bash
./scripts/push-to-local-registry.sh 5001
```

**行为：**
- 自动组装为 `localhost:5001`
- 适用于本机测试场景

**适用场景：**
- AirPlay 占用 5000 端口，使用 5001
- 运行多个 Registry 实例

---

### 3️⃣ 指定完整地址

```bash
./scripts/push-to-local-registry.sh localhost:5001
```

**行为：**
- 直接使用指定的完整地址

---

### 4️⃣ 指定局域网 IP + 端口

```bash
./scripts/push-to-local-registry.sh 192.168.1.100:5001
```

**行为：**
- 使用局域网 IP 地址
- 适用于准备推送到局域网可访问的 Registry

**适用场景：**
- 服务器需要从开发机拉取镜像
- 局域网部署

---

### 5️⃣ 仅指定 IP（使用默认端口 5000）

```bash
./scripts/push-to-local-registry.sh 192.168.1.100
```

**行为：**
- 自动补全端口为 5000
- 最终地址为 `192.168.1.100:5000`

---

## 完整工作流示例

### 场景 1: AirPlay 占用 5000，使用 5001 端口

```bash
# 步骤 1: 搭建 Registry（使用 5001 端口）
./scripts/setup-local-registry.sh 5001

# 步骤 2: 构建并推送镜像（自动检测到 5001）
./scripts/push-to-local-registry.sh

# 或显式指定端口
./scripts/push-to-local-registry.sh 5001
```

---

### 场景 2: 局域网部署，使用自定义端口

```bash
# 步骤 1: 获取本机局域网 IP
ipconfig getifaddr en0
# 输出: 192.168.1.100

# 步骤 2: 搭建 Registry（使用 5001 端口）
./scripts/setup-local-registry.sh 5001

# 步骤 3: 推送镜像（使用局域网 IP）
./scripts/push-to-local-registry.sh 192.168.1.100:5001

# 步骤 4: 在服务器上拉取
# （在服务器执行）
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

---

### 场景 3: 默认配置（5000 端口）

```bash
# 步骤 1: 搭建 Registry（默认 5000）
./scripts/setup-local-registry.sh

# 步骤 2: 推送镜像（自动检测）
./scripts/push-to-local-registry.sh

# 或显式指定
./scripts/push-to-local-registry.sh localhost:5000
```

---

## 参数解析逻辑

脚本使用智能参数解析：

| 输入参数 | 解析结果 | 说明 |
|---------|---------|------|
| （无） | `localhost:5000` 或自动检测 | 尝试检测运行中的 Registry |
| `5001` | `localhost:5001` | 纯数字视为端口 |
| `localhost:5001` | `localhost:5001` | 包含冒号视为完整地址 |
| `192.168.1.100:5001` | `192.168.1.100:5001` | 完整的 IP:端口 |
| `192.168.1.100` | `192.168.1.100:5000` | 仅 IP 时补全默认端口 |

---

## 错误提示和自动修复建议

### 错误 1: Registry 未运行

```
❌ 错误: 无法连接到 Registry: localhost:5001

请确保：
  1. Registry 服务已启动:
     docker ps | grep addp-registry

  2. 如果 Registry 未运行，请先启动:
     ./scripts/setup-local-registry.sh 5001
```

**解决方法：**
```bash
./scripts/setup-local-registry.sh 5001
./scripts/push-to-local-registry.sh 5001
```

---

### 错误 2: 端口冲突

如果 Registry 启动失败（5000 被占用）：

```bash
# 使用其他端口
./scripts/setup-local-registry.sh 5001
./scripts/push-to-local-registry.sh 5001
```

---

## 验证推送成功

```bash
# 查看 Registry 中的镜像列表
curl http://localhost:5001/v2/_catalog

# 预期输出
{
  "repositories": [
    "addp-gateway",
    "addp-manager-backend",
    "addp-manager-frontend",
    "addp-meta-backend",
    "addp-portal",
    "addp-system-backend",
    "addp-system-frontend"
  ]
}

# 查看特定镜像的标签
curl http://localhost:5001/v2/addp-system-backend/tags/list

# 预期输出
{
  "name": "addp-system-backend",
  "tags": ["latest"]
}
```

---

## 常见问题

### Q1: 如何知道当前 Registry 运行在哪个端口？

```bash
docker ps --format 'table {{.Names}}\t{{.Ports}}' | grep addp-registry

# 输出示例
# addp-registry    0.0.0.0:5001->5000/tcp
# 说明 Registry 运行在 5001 端口
```

### Q2: 推送时如何使用局域网 IP？

```bash
# 方法 1: 获取本机 IP
ipconfig getifaddr en0  # macOS Wi-Fi
ipconfig getifaddr en1  # macOS 以太网

# 方法 2: 使用脚本自动输出（setup-local-registry.sh 会显示）
./scripts/setup-local-registry.sh 5001
# 输出会包含: 局域网访问: http://192.168.1.100:5001

# 方法 3: 推送时指定
./scripts/push-to-local-registry.sh 192.168.1.100:5001
```

### Q3: 可以同时推送到多个 Registry 吗？

脚本一次只能推送到一个 Registry，但可以多次运行：

```bash
# 推送到本地测试 Registry
./scripts/push-to-local-registry.sh localhost:5001

# 推送到局域网 Registry
./scripts/push-to-local-registry.sh 192.168.1.100:5001
```

### Q4: 如何查看推送进度？

脚本会实时显示：

```
[1/7] 处理: system-backend
  - 标记镜像: localhost:5001/addp-system-backend:latest
  - 推送镜像...
  ✅ 推送成功

[2/7] 处理: system-frontend
  ...
```

最后会显示统计信息：

```
统计信息：
  - 成功推送: 7 个镜像
  - 失败/跳过: 0 个镜像
  - 总耗时: 180s
```

---

## 高级技巧

### 技巧 1: 跳过构建，仅推送已有镜像

如果本地已有构建好的镜像，可以注释掉构建步骤：

```bash
# 临时跳过构建
sed -i.bak 's/make docker-build-all/# make docker-build-all/' scripts/push-to-local-registry.sh
./scripts/push-to-local-registry.sh 5001
mv scripts/push-to-local-registry.sh.bak scripts/push-to-local-registry.sh
```

### 技巧 2: 推送到多个 Registry

```bash
# 创建循环脚本
for registry in localhost:5001 192.168.1.100:5001; do
  echo "推送到 $registry..."
  ./scripts/push-to-local-registry.sh $registry
done
```

### 技巧 3: 验证所有镜像是否推送成功

```bash
REGISTRY="localhost:5001"

for service in system-backend system-frontend manager-backend manager-frontend meta-backend gateway portal; do
  echo -n "检查 addp-$service... "
  if curl -sf "http://$REGISTRY/v2/addp-$service/tags/list" | grep -q "latest"; then
    echo "✅"
  else
    echo "❌ 缺失"
  fi
done
```

---

## 相关文档

- [setup-local-registry.sh 使用指南](SETUP_REGISTRY_GUIDE.md)
- [端口 5000 被占用问题解决](TROUBLESHOOT_PORT_5000.md)
- [完整部署指南](DEPLOY_WITH_LOCAL_REGISTRY.md)
