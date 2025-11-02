# SSH 连接被拒绝问题排查指南

## 问题现象

```bash
ssh pampa@192.168.1.182
ssh: connect to host 192.168.1.182 port 22: Connection refused
```

能 ping 通，但 SSH 连接被拒绝。

---

## 诊断步骤

### 1. 检查服务器 SSH 端口是否开放

```bash
# 从开发机检查服务器的 22 端口
nc -zv 192.168.1.182 22

# 或使用 telnet
telnet 192.168.1.182 22

# 或使用 nmap（如果已安装）
nmap -p 22 192.168.1.182
```

**预期结果：**
- ✅ 成功：`Connection to 192.168.1.182 22 port [tcp/ssh] succeeded!`
- ❌ 失败：`Connection refused` 或 `No route to host`

---

### 2. 确认服务器操作系统

**如果服务器是 macOS：**

macOS 默认**没有启动** SSH 服务（Remote Login）。

**如果服务器是 Linux：**

可能 SSH 服务未安装或未启动。

---

## 解决方案

### 方案 A：服务器是 macOS（最可能）

#### 图形界面启用 SSH

1. **打开系统偏好设置**（System Preferences / System Settings）
2. **共享**（Sharing）
3. **勾选"远程登录"**（Remote Login）
4. 选择允许访问的用户：
   - **所有用户**（All users）
   - 或**仅这些用户**（Only these users），添加 `pampa`

#### 命令行启用 SSH（如果你有物理访问权限）

```bash
# 在服务器上执行
sudo systemsetup -setremotelogin on

# 验证状态
sudo systemsetup -getremotelogin
# 输出: Remote Login: On
```

#### 验证 SSH 服务

```bash
# 在服务器上检查 SSH 服务状态
sudo launchctl list | grep ssh
# 应该看到: com.openssh.sshd

# 查看监听端口
sudo lsof -iTCP:22 -sTCP:LISTEN
# 应该看到 sshd 进程
```

---

### 方案 B：服务器是 Linux

#### Ubuntu/Debian

```bash
# 在服务器上执行

# 1. 安装 SSH 服务
sudo apt update
sudo apt install openssh-server

# 2. 启动 SSH 服务
sudo systemctl start ssh
sudo systemctl enable ssh

# 3. 检查状态
sudo systemctl status ssh

# 4. 检查防火墙
sudo ufw allow ssh
sudo ufw status
```

#### CentOS/RHEL

```bash
# 在服务器上执行

# 1. 安装 SSH 服务
sudo yum install openssh-server

# 2. 启动 SSH 服务
sudo systemctl start sshd
sudo systemctl enable sshd

# 3. 检查状态
sudo systemctl status sshd

# 4. 检查防火墙
sudo firewall-cmd --permanent --add-service=ssh
sudo firewall-cmd --reload
```

---

### 方案 C：SSH 端口不是 22（非标准配置）

某些服务器可能使用非标准 SSH 端口（如 2222）。

#### 扫描常用 SSH 端口

```bash
# 从开发机扫描
for port in 22 2222 2200 22022; do
  echo "Testing port $port..."
  nc -zv -w 2 192.168.1.182 $port 2>&1 | grep -v "refused"
done
```

#### 如果发现其他端口开放

```bash
# 使用非标准端口连接
ssh -p 2222 pampa@192.168.1.182
```

---

## 无法物理访问服务器的替代方案

如果你无法直接操作服务器（没有显示器/键盘），可以：

### 方案 1：使用屏幕共享（macOS）

macOS 支持 VNC 屏幕共享：

1. **在服务器上**（如果之前启用过）：
   - 系统偏好设置 → 共享 → 屏幕共享

2. **从开发机连接**：
   ```bash
   open vnc://192.168.1.182
   ```

### 方案 2：使用 Docker exec（如果 Docker 可访问）

如果服务器上已经运行 Docker：

```bash
# 从开发机通过 Docker API 连接
export DOCKER_HOST=tcp://192.168.1.182:2375
docker ps

# 进入容器执行命令
docker exec -it container_name bash
```

### 方案 3：本地部署（临时方案）

如果无法访问服务器，可以先在本地测试部署：

```bash
# 在开发机本地部署（用于测试）
cd /Users/pampa/code/addp

# 部署 Business 基础设施
cd business
cp .env.prod.example .env
vim .env  # 修改配置
docker-compose -f docker-compose.prod.yml up -d

# 部署 ADDP 系统（使用本地 Registry）
cd ..
REGISTRY=localhost:5001 ./scripts/deploy-from-registry.sh
```

---

## 快速决策树

```
1. 服务器操作系统是什么？
   ├─ macOS → 启用"远程登录"（System Settings → Sharing → Remote Login）
   ├─ Linux → 安装并启动 sshd 服务
   └─ 不确定 → 继续下一步

2. 能否物理访问服务器？
   ├─ 能 → 直接在服务器上启用 SSH
   └─ 不能 → 尝试以下方法：
       ├─ VNC 屏幕共享
       ├─ iCloud 查找我的 Mac（远程唤醒）
       └─ 本地部署测试

3. 是否使用非标准 SSH 端口？
   ├─ 是 → ssh -p <端口> pampa@192.168.1.182
   └─ 否 → 继续排查
```

---

## 验证修复

SSH 启用后，测试连接：

```bash
# 基本连接测试
ssh pampa@192.168.1.182

# 第一次连接会提示接受指纹
# The authenticity of host '192.168.1.182 (192.168.1.182)' can't be established.
# ... Are you sure you want to continue connecting (yes/no)?
# 输入: yes

# 成功登录后
pampa@server:~$
```

---

## 常见错误和解决方法

### 错误 1: Connection refused

**原因**：SSH 服务未启动

**解决**：
- macOS: 启用"远程登录"
- Linux: `sudo systemctl start sshd`

---

### 错误 2: No route to host

**原因**：防火墙阻止或网络不通

**解决**：
```bash
# macOS 关闭防火墙（临时）
sudo /usr/libexec/ApplicationFirewall/socketfilterfw --setglobalstate off

# Linux 允许 SSH
sudo ufw allow 22/tcp
```

---

### 错误 3: Permission denied (publickey)

**原因**：需要密钥认证或密码

**解决**：
```bash
# 使用密码登录
ssh -o PreferredAuthentications=password pampa@192.168.1.182

# 或生成 SSH 密钥
ssh-keygen -t ed25519
ssh-copy-id pampa@192.168.1.182
```

---

### 错误 4: Host key verification failed

**原因**：服务器 SSH 指纹变化

**解决**：
```bash
# 删除旧的指纹
ssh-keygen -R 192.168.1.182

# 重新连接
ssh pampa@192.168.1.182
```

---

## macOS 远程登录详细步骤

### macOS Ventura (13.0+) 或更新版本

1. **系统设置**（System Settings）
2. **通用**（General）
3. **共享**（Sharing）
4. 打开 **远程登录**（Remote Login）开关
5. 点击 **允许完全磁盘访问权限**（可选）
6. 在 **允许访问**中选择用户

### macOS Big Sur/Monterey (11.0-12.x)

1. **系统偏好设置**（System Preferences）
2. **共享**（Sharing）
3. 勾选 **远程登录**（Remote Login）
4. 选择 **所有用户** 或 **仅这些用户**

### 验证设置

```bash
# 在服务器上查看 SSH 配置
cat /etc/ssh/sshd_config | grep -v "^#" | grep -v "^$"

# 查看 SSH 日志
sudo log show --predicate 'process == "sshd"' --last 1h
```

---

## 如果所有方法都失败

### 最后的手段：使用文件共享替代 SSH

**macOS 文件共享（SMB）：**

```bash
# 从开发机挂载服务器共享
# 1. Finder → Go → Connect to Server (Cmd+K)
# 2. 输入: smb://192.168.1.182
# 3. 手动复制部署文件到服务器

# 或使用命令行
mount_smbfs //pampa@192.168.1.182/Users/pampa /Volumes/server
cp -r /Users/pampa/code/addp /Volumes/server/addp-deploy
```

---

## 推荐配置

SSH 启用后，建议配置免密登录：

```bash
# 在开发机生成密钥（如果还没有）
ssh-keygen -t ed25519 -C "pampa@macbook"

# 复制公钥到服务器
ssh-copy-id pampa@192.168.1.182

# 测试免密登录
ssh pampa@192.168.1.182
# 不需要输入密码 ✅
```

---

## 快速参考命令

```bash
# 测试连接
ping 192.168.1.182           # 网络连通性
nc -zv 192.168.1.182 22      # SSH 端口
ssh pampa@192.168.1.182      # SSH 登录

# macOS 服务器启用 SSH
sudo systemsetup -setremotelogin on

# Linux 服务器启用 SSH
sudo systemctl start sshd
sudo systemctl enable sshd
```

---

## 下一步

SSH 连接成功后，继续部署：

```bash
# 传输部署文件
scp docker-compose.prod.yml pampa@192.168.1.182:/opt/addp/
scp scripts/deploy-from-registry.sh pampa@192.168.1.182:/opt/addp/scripts/

# SSH 登录
ssh pampa@192.168.1.182

# 运行部署
cd /opt/addp
REGISTRY=192.168.1.100:5001 ./scripts/deploy-from-registry.sh
```
