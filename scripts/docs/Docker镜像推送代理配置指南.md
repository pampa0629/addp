# Docker 镜像推送代理配置指南

[返回脚本导航](../README.md) · [故障排查总入口](../../docs/guide/addp常见故障排查.md#按现象查找) · [返回推送故障排查](../build/README.md#故障排查) · [工作流常见问题](工作流与常见问题.md#q2-镜像能拉取但推送失败)

本文定位 macOS Docker Desktop 的代理解析与传输问题。推送参数、服务选择及预演统一见[推送脚本专题](../build/README.md#服务选择与预演范围)，结果按[推送退出与验收边界](../build/README.md#推送退出与验收边界)认定；代理观察不能替代实际镜像上传。

## 问题现象

通过[正式推送入口](../build/README.md#push-imagessh)上传时，可能一直停在 `Waiting`，并出现以下错误：

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

以下是原环境的排查示意，内部代理地址、端口与转发方式须按当前 Docker Desktop 版本和配置核对，不是所有请求都已走通的证明：

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

选择代理地址前，先确认该地址由哪一层解析。

- 如果地址是在 Docker VM 或容器网络里解析，`127.0.0.1` 往往指向 VM 或容器自身，需要核对该层可达的宿主机地址；`host.docker.internal` 是候选，不表示它在所有代理解析位置都能使用。
- 如果地址是在 Docker Desktop 的内部代理或宿主侧逻辑里解析，`127.0.0.1` 可能就是 macOS 宿主机；而 `host.docker.internal` 反而可能解析失败，报 `lookup host.docker.internal: no such host`。

Docker Desktop 版本、代理模式或网络变化后，按当前解析位置和实际请求重新验证地址。

认证请求与 blob 上传在目标、数据量和持续时间上不同；小请求或登录成功不能证明后续上传稳定。`unexpected EOF`、`broken pipe` 需结合发生阶段判断，不能仅凭错误文本确认代理就是根因。

### system 模式的检查范围

系统代理模式读取宿主机代理，手动模式使用显式地址；不能据模式名称判断链路稳定性。当前版本还可能区分 Desktop 与 Containers 代理，应按[Docker Desktop 官方设置说明](https://docs.docker.com/desktop/settings-and-maintenance/settings/#proxies)确认请求采用的配置。Desktop 忽略 `daemon.json` 中的代理配置，设置归属见[官方 daemon 代理说明](https://docs.docker.com/engine/daemon/proxy/)；不要把独立 Linux Engine 的配置方式直接套入本文。

## 解决方案

### 先确认宿主机代理端口

先在 macOS 宿主机上确认代理端口可用：

```bash
curl -I --max-time 8 -x http://127.0.0.1:17890 https://registry-1.docker.io/v2/
```

若确认响应来自目标 Registry，`401` 等预期认证响应只说明该宿主机请求收到了目标回应，不表示凭据或上传权限有效。此命令使用显式 `-x`，不能证明 Docker daemon 使用了同一代理；实际范围见[分层验证](#验证范围与实际操作)。

### 配置一：优先尝试 `127.0.0.1`

原环境曾在代理地址解析时报 `lookup host.docker.internal: no such host`，因此先尝试以下回环配置。表中端口和 bypass 范围均是该环境示例，实际值按所用代理、网络与目标 Registry 核对：

| 字段 | 值 |
|------|-----|
| Web Server (HTTP) | `http://127.0.0.1:17890` |
| Secure Web Server (HTTPS) | `http://127.0.0.1:17890` |
| Bypass proxy settings | `localhost,127.0.0.1,192.168.65.0/24,10.0.0.0/8` |

由环境维护者核对正在运行的容器及 Docker 重启影响，再执行界面的 **Apply & Restart**；之后按[验证范围与实际操作](#验证范围与实际操作)分别记录读取和上传结果。一次小镜像 pull 成功不作为 push 就绪的判断。

### 配置二：如果 `127.0.0.1` 不稳定，再尝试 `host.docker.internal`

原环境在回环配置出现连接或 blob 上传中断时，也尝试过以下宿主机域名配置。它是另一组待验证的地址示例，不能仅凭 `unexpected EOF` 或 `broken pipe` 判定必须切换：

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

这只确认本次解析失败；应核对出错的是代理地址还是目标 Registry，再按实际解析层选择可达地址。原环境据此切回 `127.0.0.1`，不能把该结果推广为所有 Docker 网络都不能使用 `host.docker.internal`。

## 操作步骤

1. 在 Docker Desktop 的 Settings → Resources → Proxies 核对当前版本的代理入口及生效配置，确认宿主机实际代理端口。
2. 需要手动配置时，根据[配置一](#配置一优先尝试-127001)与[配置二](#配置二如果-127001-不稳定再尝试-hostdockerinternal)的解析条件选择地址，并核对 bypass 范围。
3. 核对运行中容器与重启影响后，由环境维护者执行 **Apply & Restart**；这不是只读诊断步骤。
4. 按[分层验证](#验证范围与实际操作)分别核对当前 daemon、样本拉取和所需上传；保留失败阶段，不将 pull 成功或 `--dry-run` 记为实际 push 通过。
5. 地址解析失败按对应解析层处理；上传中断则核对实际目标、权限及传输记录，不仅凭错误关键词切换代理或重复推送。

## 验证范围与实际操作

| 检查 | 能说明的范围 | 操作与未覆盖项 |
| --- | --- | --- |
| 宿主机 `curl -x` | 显式代理下，这一次宿主机 HTTP 请求的响应。 | 发起网络请求，不读取 Docker 的生效代理，也不验证完整镜像传输。 |
| `docker info` 的代理字段 | 当前 Docker 上下文所连 daemon 报告的代理信息。 | 查询配置摘要；字段存在不证明上游可达或上传稳定。 |
| 样本 `docker pull` | 该次、该 Registry 与该镜像的读取结果。 | 可能写入本地镜像存储，也可能复用已有层；不证明有新层下载、大镜像吞吐、其他 Registry 或 push 可用。拉取及层复用见[官方说明](https://docs.docker.com/reference/cli/docker/image/pull/)。 |
| `docker login` | 对所选 Registry 的认证结果。 | 可能更新凭据存储，不证明目标 namespace 的写权限或 blob 上传。认证职责见[官方说明](https://docs.docker.com/reference/cli/docker/login/)。 |
| 推送脚本预演与实际推送 | 分别核对本地源引用预演和指定镜像的实际上传。 | 操作、缓存、模拟计数及失败语义统一见[推送专题](../build/README.md#服务选择与预演范围)和[验收边界](../build/README.md#推送退出与验收边界)。 |

需要检查 Docker Hub 样本读取时，由操作者执行以下命令并记录实际结果：

```bash
docker pull hello-world:latest
```

该样本失败可定位它对应的读取链路；推送脚本不依赖这个样本，因此它不是排查其他目标 push 的强制前置条件。上传属于独立操作，见 [Docker push 官方说明](https://docs.docker.com/reference/cli/docker/image/push/)；需要真实验证时，由操作者选择有权限的目标和镜像版本，沿[既有推送示例](../build/README.md#push-imagessh)执行，不能用预演、缓存命中或部分成功代替完整送达。即使指定 push 成功，目标环境的拉取、平台匹配和产品运行仍按对应范围另行验收。

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

上述输出只表示该 daemon 报告了内部代理端点，不证明其上游配置或目标请求成功。没有匹配输出时先核对完整 `docker info` 的退出结果与当前上下文，不能据此认定没有代理；pull、login 和 push 的判定统一见[分层验证](#验证范围与实际操作)。

## 端口说明

`17890` 是原排查环境使用的 Clash HTTP 代理端口，不是项目分配的服务端口或当前机器的已验证值。`7890`（HTTP）、`7891`（SOCKS5）也只作端口示例；Clash、ClashX、V2RayX 等工具均须在实际界面核对协议、监听地址和端口，再替换上文参数，不从工具名称推断。
