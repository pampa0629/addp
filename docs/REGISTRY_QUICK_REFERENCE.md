# Registry 脚本快速参考

## 📋 常用命令速查

### 搭建 Registry

```bash
# 默认端口 5000
./scripts/setup-local-registry.sh

# 自定义端口（推荐 5001，避免 AirPlay 冲突）
./scripts/setup-local-registry.sh 5001

# 查看运行状态
docker ps | grep addp-registry
```

---

### 推送镜像

```bash
# 自动检测 Registry 端口（最简单）
./scripts/push-to-local-registry.sh

# 指定端口号
./scripts/push-to-local-registry.sh 5001

# 指定完整地址
./scripts/push-to-local-registry.sh localhost:5001

# 使用局域网 IP（用于服务器部署）
./scripts/push-to-local-registry.sh 192.168.1.100:5001
```

---

### 验证 Registry

```bash
# 查看 Registry API
curl http://localhost:5001/v2/

# 查看所有镜像
curl http://localhost:5001/v2/_catalog

# 查看特定镜像的标签
curl http://localhost:5001/v2/addp-system-backend/tags/list
```

---

## 🔧 故障排查

### 端口被占用

```bash
# 检查占用进程
lsof -i :5000

# 使用其他端口
./scripts/setup-local-registry.sh 5001
./scripts/push-to-local-registry.sh 5001
```

### Registry 无法连接

```bash
# 检查容器状态
docker ps | grep addp-registry

# 重启 Registry
docker restart addp-registry

# 查看日志
docker logs addp-registry
```

---

## 📖 完整部署流程

### 本机操作

```bash
# 1. 搭建 Registry
./scripts/setup-local-registry.sh 5001

# 2. 构建并推送镜像
./scripts/push-to-local-registry.sh 5001

# 3. 记录本机 IP（脚本会自动显示）
# 例如: 192.168.1.100
```

### 服务器操作

```bash
# 1. 传输部署文件
scp docker-compose.prod.yml user@server:/opt/addp/
scp scripts/deploy-from-registry.sh user@server:/opt/addp/scripts/

# 2. SSH 登录服务器
ssh user@server

# 3. 部署
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

---

## 🎯 推荐配置

| 场景 | Registry 端口 | 原因 |
|------|-------------|------|
| macOS 开发 | 5001 | 避免 AirPlay 占用 5000 |
| Linux 开发 | 5000 | 默认端口，无冲突 |
| 局域网部署 | 5001 | 统一配置 |

---

## 📚 相关文档

- [端口 5000 故障排查](TROUBLESHOOT_PORT_5000.md)
- [推送脚本详细指南](PUSH_TO_REGISTRY_GUIDE.md)
- [完整部署指南](DEPLOY_WITH_LOCAL_REGISTRY.md)
