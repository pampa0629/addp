# 端口 5000 被占用问题解决方案

## 问题现象

```
docker: Error response from daemon: Ports are not available:
exposing port TCP 0.0.0.0:5000 -> 127.0.0.1:0: listen tcp 0.0.0.0:5000:
bind: address already in use
```

---

## 诊断步骤

### 1. 检查占用端口的进程

```bash
lsof -i :5000
```

**常见情况：**

| 进程名 | 原因 | 是否安全关闭 |
|--------|------|------------|
| `ControlCe` 或 `AirPlayXPCHelper` | macOS AirPlay Receiver | ✅ 安全 |
| `registry` | 之前启动的 Registry 容器 | ✅ 安全 |
| 其他应用 | 第三方软件 | ⚠️  需要确认 |

---

## 解决方案

### 方案一：关闭 macOS AirPlay Receiver（推荐）

**macOS Ventura (13.0+) / Monterey (12.0+):**

#### 图形界面方式
1. 打开 **系统设置**（System Settings）
2. 点击 **通用**（General）→ **隔空播放与接力**（AirDrop & Handoff）
3. 关闭 **隔空播放接收器**（AirPlay Receiver）

#### 命令行方式（临时）
```bash
# 方法 1: 使用系统命令（推荐）
sudo defaults write /Library/Preferences/com.apple.AppleAirPlayReceiver Disabled -bool true
sudo pkill AirPlayXPCHelper

# 方法 2: 直接停止服务
sudo launchctl bootout system/com.apple.AirPlayXPCHelper 2>/dev/null || true
```

**然后重新运行搭建脚本：**
```bash
./scripts/setup-local-registry.sh
```

**恢复 AirPlay（如果需要）：**
```bash
sudo defaults write /Library/Preferences/com.apple.AppleAirPlayReceiver Disabled -bool false
sudo launchctl bootstrap system /System/Library/LaunchDaemons/com.apple.AirPlayXPCHelper.plist
```

---

### 方案二：使用其他端口（推荐，不影响 AirPlay）

**使用端口 5001（或其他可用端口）：**

```bash
# 1. 搭建 Registry 使用 5001 端口
./scripts/setup-local-registry.sh 5001

# 脚本会输出类似：
# ✅ Registry 搭建成功！
# - 本机访问: http://localhost:5001
# - 局域网访问: http://192.168.1.100:5001

# 2. 推送镜像（智能参数解析，支持多种格式）
./scripts/push-to-local-registry.sh 5001               # 推荐：仅指定端口
# 或
./scripts/push-to-local-registry.sh localhost:5001     # 完整地址
# 或
./scripts/push-to-local-registry.sh                    # 自动检测端口

# 3. 在服务器上拉取时指定新端口
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

**智能参数解析功能：**
- `5001` → 自动解析为 `localhost:5001`
- `192.168.1.100:5001` → 使用指定的完整地址
- 无参数 → 自动检测运行中的 Registry 端口

**优点：**
- ✅ 不影响 macOS AirPlay 功能
- ✅ 可以同时运行多个 Registry（用于不同项目）
- ✅ 无需管理员权限
- ✅ 脚本自动检测和适配端口

---

### 方案三：清理已有的 Registry 容器

如果之前运行过 Registry，可能容器仍在运行：

```bash
# 查看是否有 Registry 容器
docker ps -a | grep registry

# 停止并删除旧容器
docker stop addp-registry 2>/dev/null || true
docker rm addp-registry 2>/dev/null || true

# 重新运行搭建脚本
./scripts/setup-local-registry.sh
```

---

### 方案四：强制停止占用进程（谨慎使用）

⚠️ **警告：仅在确认进程可以安全停止时使用**

```bash
# 查看占用 5000 端口的进程 PID
lsof -ti :5000

# 强制停止（替换 PID 为实际值）
sudo kill -9 <PID>

# 或者一行命令
sudo lsof -ti :5000 | xargs kill -9

# 然后重新运行脚本
./scripts/setup-local-registry.sh
```

---

## 快速决策树

```
端口 5000 被占用
    │
    ├─ 是 AirPlay？
    │   ├─ 不需要 AirPlay → 关闭 AirPlay（方案一）
    │   └─ 需要 AirPlay → 使用其他端口（方案二）
    │
    ├─ 是旧的 Registry 容器？
    │   └─ 清理容器（方案三）
    │
    └─ 是其他应用？
        ├─ 可以关闭 → 停止进程（方案四）
        └─ 不能关闭 → 使用其他端口（方案二）
```

---

## 验证解决

### 1. 验证端口已释放

```bash
lsof -i :5000
# 应该无输出，或者只显示你的 Registry
```

### 2. 验证 Registry 正常运行

```bash
# 检查容器状态
docker ps | grep addp-registry

# 测试 Registry API
curl http://localhost:5000/v2/
# 应该返回: {}

# 查看镜像列表
curl http://localhost:5000/v2/_catalog
# 应该返回: {"repositories":[]}
```

---

## 常见问题

### Q1: 关闭 AirPlay 后无法在 iPhone/iPad 上投屏了？

**A:** 这是正常的。如果需要恢复 AirPlay，有两个选择：
1. 重新开启 AirPlay，Registry 使用 5001 端口（推荐）
2. 临时开启 AirPlay，部署完成后再关闭

### Q2: 使用 5001 端口后，之前的脚本还能用吗？

**A:** 需要在所有命令中指定新端口：
```bash
# 搭建 Registry
./scripts/setup-local-registry.sh 5001

# 推送镜像
./scripts/push-to-local-registry.sh localhost:5001

# 服务器拉取
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

### Q3: 可以永久禁用 AirPlay 占用 5000 端口吗？

**A:** 可以，但不推荐。建议使用方案二（Registry 用 5001 端口）。

如果确实需要永久禁用：
```bash
# 禁用 AirPlay 服务
sudo launchctl disable system/com.apple.AirPlayXPCHelper

# 重启后生效
```

### Q4: 我用的是 Linux/Windows，也会有这个问题吗？

**A:** 这个问题主要出现在 macOS 上。Linux/Windows 系统通常不会默认占用 5000 端口。

---

## 推荐配置

**对于大多数用户，推荐方案二（使用 5001 端口）：**

1. 保留 AirPlay 功能
2. 避免权限问题
3. 不影响系统服务

**完整部署流程：**
```bash
# 开发机
./scripts/setup-local-registry.sh 5001
./scripts/push-to-local-registry.sh 192.168.1.100:5001

# 服务器
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```

---

## 参考链接

- [macOS AirPlay Receiver 说明](https://support.apple.com/guide/mac-help/use-airplay-mac-mchl15c9e4b5/mac)
- [Docker Registry 官方文档](https://docs.docker.com/registry/)
- [端口占用排查指南](https://stackoverflow.com/questions/3855127/find-and-kill-process-locking-port-3000-on-mac)
