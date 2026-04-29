# Docker 镜像推送代理配置指南

## 问题现象

执行 `bash scripts/build/push-images.sh --registry docker.io/<username>` 时，push 一直卡在 `Waiting` 状态，最终报错：

```
failed to authorize: failed to fetch oauth token: Post "https://auth.docker.io/token": unexpected EOF
```

或：

```
failed to copy: failed to do request: Put "https://registry-1.docker.io/...": write tcp ...: broken pipe
```

浏览器能正常访问 hub.docker.com，`docker login` 也显示成功，但 push 就是失败。

也可能在执行 `docker pull hello-world:latest` 或推送镜像时看到：

```
connecting to host.docker.internal:17890: dial tcp: lookup host.docker.internal: no such host
```

## 根本原因

### 网络层次

Docker Desktop 在 macOS 上运行在一个轻量虚拟机（Linux VM）中，网络路径如下：

```
Docker daemon (VM 内部)
    ↓
Docker Desktop 内部代理 (http.docker.internal:3128)
    ↓
宿主机代理 (Clash, 127.0.0.1:17890)
    ↓
互联网 (Docker Hub)
```

### 问题根源：代理地址由谁解析

Docker Desktop 的代理设置比较绕，关键不在于 `127.0.0.1` 或 `host.docker.internal` 哪个永远正确，而在于这个地址最终由哪一层来解析。

- 如果地址是在 Docker VM 或容器网络里解析，`127.0.0.1` 往往指向 VM 或容器自身，不是 macOS 宿主机。这时应该使用 `host.docker.internal`。
- 如果地址是在 Docker Desktop 的内部代理或宿主侧逻辑里解析，`127.0.0.1` 可能就是 macOS 宿主机；而 `host.docker.internal` 反而可能解析失败，报 `lookup host.docker.internal: no such host`。

所以同一台机器上，Docker Desktop 升级、代理模式变化、网络配置变化之后，之前可用的配置可能会失效。不要只记一个固定答案，要用下面的验证方法判断当前环境该填哪个地址。

小请求（如认证 token）有时能成功，是因为连接建立快、数据量小；大 blob 上传时连接持续时间长，更容易暴露代理链路不稳定、上游代理不可达、连接被重置等问题。

### 为什么 `system` 模式也不稳定

Docker Desktop 的 "自动检测系统代理" 模式同样经过内部代理层（3128）中转，存在相同的转发不稳定问题。

## 解决方案

### 先确认宿主机代理端口

先在 macOS 宿主机上确认代理端口可用：

```bash
curl -I --max-time 8 -x http://127.0.0.1:17890 https://registry-1.docker.io/v2/
```

如果返回 `HTTP/2 401` 或类似 Docker Registry 的认证响应，说明宿主机上的 HTTP 代理是通的。`401` 是正常结果，表示已经连到了 Docker Registry，只是未带认证。

### 配置一：优先尝试 `127.0.0.1`

当前 Docker Desktop 环境中，如果 `host.docker.internal` 报 `lookup host.docker.internal: no such host`，应在 Docker Desktop 的 Proxy 设置里使用：

| 字段 | 值 |
|------|-----|
| Web Server (HTTP) | `http://127.0.0.1:17890` |
| Secure Web Server (HTTPS) | `http://127.0.0.1:17890` |
| Bypass proxy settings | `localhost,127.0.0.1,192.168.65.0/24,10.0.0.0/8` |

配置后点击 **Apply & Restart**，然后验证：

```bash
docker pull hello-world:latest
```

这个命令能成功，说明 Docker daemon 到 Docker Hub 的代理链路已经打通，可以继续执行：

```bash
./scripts/build/push-images.sh --registry docker.io/<username>
```

### 配置二：如果 `127.0.0.1` 不稳定，再尝试 `host.docker.internal`

如果使用 `127.0.0.1` 时出现 `unexpected EOF`、`broken pipe`、大镜像 blob 上传中断等问题，可以改用：

| 字段 | 值 |
|------|-----|
| Web Server (HTTP) | `http://host.docker.internal:17890` |
| Secure Web Server (HTTPS) | `http://host.docker.internal:17890` |
| Bypass proxy settings | `localhost,127.0.0.1,192.168.65.0/24,10.0.0.0/8` |

`host.docker.internal` 是 Docker Desktop 提供的特殊域名，在容器或 Docker VM 网络里通常会被解析为宿主机的实际 IP，从而访问宿主机上运行的 Clash 代理。

但如果验证时看到下面这种错误：

```
lookup host.docker.internal: no such host
```

说明当前 Docker Desktop 的代理解析链路不适合使用 `host.docker.internal`，需要切回 `127.0.0.1`。

## 操作步骤

1. 打开 Docker Desktop → Settings → Resources → Proxies
2. 开启 **Manual proxy configuration**
3. 优先按“配置一”填写 `127.0.0.1`
4. 点击 **Apply & Restart**
5. 执行 `docker pull hello-world:latest` 验证
6. 如果报 `lookup host.docker.internal`，不要继续重试 push，先把代理地址改回 `127.0.0.1`
7. 如果 `127.0.0.1` 出现上传中断，再尝试“配置二”的 `host.docker.internal`

## 快速排查命令

查看 Docker daemon 当前实际使用的代理：

```bash
docker info | grep -A5 -i proxy
```

正常情况下会看到类似：

```
HTTP Proxy: http.docker.internal:3128
HTTPS Proxy: http.docker.internal:3128
```

这表示 Docker daemon 先连接 Docker Desktop 内部代理，再由内部代理转发到你在界面中配置的上游代理。

验证 Docker Hub 访问是否通畅：

```bash
docker pull hello-world:latest
```

只有这个命令能成功，`push-images.sh` 推送镜像才有继续排查的意义。

## 端口说明

`17890` 是本项目环境中 Clash 的 HTTP 代理端口（来自 macOS 系统网络设置）。如果你的代理工具使用不同端口，替换对应数字即可。常见默认端口：

- Clash: `7890`（HTTP）、`7891`（SOCKS5）
- V2RayX / ClashX: 视配置而定，可在代理工具界面查看
