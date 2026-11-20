# ADDP 单集群 K3s 试点交接

范围为 System、Gateway、Catalog 和 PostgreSQL、Redis、MinIO、Meilisearch。安装在现有双架构 K3s 的新 namespace。System 和 Gateway 在 ARM64，Catalog 在 AMD64。默认由 Catalog 以 Pod IPv4 向 System 登记，Gateway 解析现有 System 实例租约。对外验证仅使用回环端口，不修改正式域名、现有 Compose 或集群入口。

试点的 ARM64 基础设施包随附 Compose 共用的 PostgreSQL 配置。安装脚本通过 ConfigMap 挂载该文件，数据目录仍在 PostgreSQL 的 PVC 上。扩充模块或 Worker 并发数前，按现场峰值核对数据库连接预算。可用以下命令查看连接上限、当前连接数及配置来源。

~~~bash
kubectl -n "$SITE_NAMESPACE" exec deploy/postgres -- psql -U addp -d addp -c \
  "SELECT setting, sourcefile, (SELECT count(*) FROM pg_stat_activity) AS current_connections FROM pg_settings WHERE name = 'max_connections';"
~~~

## 构建与交付

在两个原生 Linux Docker 构建节点上准备同一个干净 Git 提交，并从构建工具版本提供 `scripts/build/package-k3s-pilot.py` 及旁边的 `scripts/prod/k3s-pilot.{py,md}`。从目标提交的仓库根目录调用构建工具。Python 3.11+，依赖和标准 Makefile 入口与 Compose 构建一致。离线构建节点提前准备与源码匹配的 Go 工具链和模块缓存，覆盖三个服务及 IAM 初始化 CLI 的依赖。标准镜像脚本检查构建机 Registry，缺失时先用 `make registry-start` 准备默认的 `localhost:5001`。构建机使用其他 Registry 时，可用 `--builder-registry` 指定。镜像按 `--verify` 构建，现场从归档导入。

```bash
# ARM64 构建节点
python3 /path/to/tools/scripts/build/package-k3s-pilot.py \
  --commit FULL_SOURCE_SHA --output /path/to/arm64 \
  --infra-release /path/to/frozen-arm64-compose-release/manifest.json

# AMD64 构建节点
python3 /path/to/tools/scripts/build/package-k3s-pilot.py \
  --commit FULL_SOURCE_SHA --output /path/to/amd64
```

ARM64 节点的冻结 Compose 发布需包含四个所选 Infra 镜像，且本地已取得该清单指定的镜像 ID。两边均构建三个应用镜像。交付两个目录中的校验清单、镜像归档、制品声明、脚本和 IAM CLI。配置与密钥在目标站点单独生成和保管。

## 现场安装

Catalog Service 将 8192/TCP 命名为 `http`，供发现方按端口名核对 EndpointSlice。kubelet 在启动阶段探测 `/health/live`，每 5 秒一次，允许连续 48 次失败；启动成功后每 10 秒检查存活，连续 3 次失败触发容器重启。Service 端点是否就绪由 `/health/ready` 单独判定。安装后应分别演练进程失去响应和业务依赖失联，记录容器重启次数、Pod UID 和 Ready 变化。Gateway 只消费 System 的路由端点快照。

目标集群需要一个 Ready ARM64 和一个 Ready AMD64 节点，集群 Pod 网络可跨节点互通，并提供能动态分配 RWO 卷的 StorageClass。两个节点上的同名 `kubernetes.io/hostname` 标签应与传给安装脚本的节点名一致。操作端需已有正确上下文的 `kubectl`、Python 3 和 `sha256sum`，并具备创建本试点资源、读取节点与存储类、向本试点 Pod 执行短时部署命令的权限。确认待使用的 namespace 是本次新建。

部署沿用镜像自身用户及卷初始化流程。试点尚未适配强制所有容器以非 root 运行的 Restricted 策略；使用前按集群现行准入规则检查，不改变已有命名空间的安全策略。

收到交付包后，先在两个目录各运行一次 `sha256sum -c SHA256SUMS`。按现场交付记录核对 `release.json` 的 `commit` 和架构，再将各自架构目录放到对应 K3s 节点。分别执行以下命令：

```bash
sudo python3 arm64/k3s-pilot.py load-node --bundle arm64 --arch arm64
sudo python3 amd64/k3s-pilot.py load-node --bundle amd64 --arch amd64
```

操作端持有两个目录，按站点实际情况替换上下文、节点名和存储类：

```bash
umask 077
mkdir -p "$HOME/addp-pilot-site"
python3 arm64/k3s-pilot.py init-credentials --bundle arm64 \
  --credentials "$HOME/addp-pilot-site/credentials.json"
python3 arm64/k3s-pilot.py install \
  --context SITE_CONTEXT --namespace addp-registry-pilot \
  --arm64 arm64 --amd64 amd64 \
  --arm-node ARM_NODE --amd-node AMD_NODE --storage-class local-path \
  --credentials "$HOME/addp-pilot-site/credentials.json"
```

验证 Kubernetes 发现时，在上面的 `install` 和下文的 `verify` 命令都追加 `--catalog-source kubernetes`。安装脚本先创建 Catalog Service，记录创建响应中的 UID，将绑定写入不可变 ConfigMap，并为 System 单独投射集群 CA 和轮换令牌。发现身份只能 list/watch 指定的 Service 和本 namespace 的 EndpointSlice。System 首次就绪并完成声明发布后扩为两个副本，从共享数据库读取路由。Catalog 启动两个副本，停止登记与心跳。验收身份需能在试点 Catalog Pod 中读取 `/var/log/addp/catalog` 下的请求完成记录（`pods/exec`）。发现身份仅持有上述读取集群资源的权限。默认 `--catalog-source system` 使用单个 Catalog 副本。

初始化凭据命令拒绝覆盖已有文件。安装先校验两份包属于同一提交，再创建 namespace 及新卷。System Ready 后使用 System 镜像内已有发布命令部署三个声明，完成后才启动 Gateway 和 Catalog。发布密钥不进入 Kubernetes Secret 或长驻业务进程。安装失败应保留当前 namespace 和事件以便诊断。再次进行空库验收要先另选新的独占 namespace。

## 首次登录与验收

在操作终端用同一个 kube context 建立仅回环可达的 PostgreSQL 与 Gateway 转发。两个转发分别在独立终端运行。

```bash
kubectl --context SITE_CONTEXT -n addp-registry-pilot port-forward --address 127.0.0.1 svc/postgres 15433:5432
kubectl --context SITE_CONTEXT -n addp-registry-pilot port-forward --address 127.0.0.1 svc/gateway 18082:8000
```

在与操作端 CPU 匹配的 Linux 系统上使用本包的 CLI。根据随附 `iam-bootstrap.md` 准备三员资料并使用真实 TOTP，按以下顺序在直接终端交互执行。保留同一次准备生成的 Bootstrap Secret 供本次 apply 输入。

```bash
python3 arm64/k3s-pilot.py bootstrap prepare --bundle arm64 \
  --credentials "$HOME/addp-pilot-site/credentials.json"
python3 arm64/k3s-pilot.py bootstrap apply --bundle arm64 \
  --credentials "$HOME/addp-pilot-site/credentials.json" \
  --administrators "$HOME/addp-pilot-site/administrators.json"
```

使用 `http://127.0.0.1:18082` 的 System API 执行下表。写入请求使用 `Content-Type: application/json`。创建用户和租户时，使用相应管理员会话的 `Authorization: Bearer <access_token>`。

| 操作 | API | 请求与通过标准 |
| --- | --- | --- |
| 登录 | `POST /api/v1/system/login` | 发送 `username`、`password`。HTTP 200，根据响应继续 MFA 或上下文选择。 |
| 验证 MFA | `POST /api/v1/system/auth/mfa-verifications` | 发送 `mfa.challenge_token` 对应的 `challenge_token` 和当前六位 `code`。HTTP 200。 |
| 选择上下文 | `POST /api/v1/system/auth/context-selections` | 有 `selection` 时，发送 `selection.selection_ticket` 对应的 `selection_ticket` 和 `context_type`。平台三员用 `platform`。租户账号用 `tenant` 并发送对应的 `tenant_membership_id`。HTTP 200，从 `session.access_token` 读取令牌。 |
| 建试点账号 | `POST /api/v1/system/platform/users` | 安全管理员提交 `display_name`、`primary_email`、`locale` 和 `local_account`（含 `username`、`password`）。HTTP 201，保存响应的 `id`。 |
| 建试点租户 | `POST /api/v1/system/platform/tenants` | 系统管理员提交 `code`、`name`，用上一步的 `id` 设置 `initial_administrator_principal_id`。HTTP 201。 |

用新账号重新登录并取得 Tenant Context 的访问令牌。将令牌存入 0600 文件供下列命令读取。保管三员密码、TOTP 和访问令牌，交付包与验收日志只记录步骤和状态。

```bash
python3 arm64/k3s-pilot.py verify \
  --context SITE_CONTEXT --namespace addp-registry-pilot \
  --arm64 arm64 --amd64 amd64 \
  --reader-token-file "$HOME/addp-pilot-site/reader.token"
```

保存安装退出码、声明发布阶段、卷和 Pod 分布、验证输出。通过标准为实际节点架构正确，Gateway 的生效地址等于 AMD64 Catalog Pod IP，租户请求返回 200 和正确列表，空列表有效。启用 Kubernetes 发现时，两次真实查询的请求 ID 应分别出现在两个 Catalog Pod 的日志，输出记录各自的 UID 和请求 ID。

## 现场核对 Catalog 端点

安装时在受信集群上下文中记录 Catalog Service UID。后续用原始记录核对同名 Service 和由原生控制器发布的 EndpointSlice。操作身份需能读取指定 Service 并列举试点 namespace 内的 EndpointSlice。检查只输出当次符合条件的 Pod UID、地址及具名 TCP 端口。Service 身份不符时命令失败；绑定有效且无可用端点时输出空数组。

```bash
python3 arm64/k3s-pilot.py inspect-service \
  --context SITE_CONTEXT --namespace addp-registry-pilot --service catalog-backend \
  --service-uid "$APPROVED_SERVICE_UID" --port-name http
```

故障演练前、摘除后、恢复后分别保存输出。Kubernetes 端点状态和 Gateway 实际转发各自取证；Gateway 使用的发现来源见部署配置。

使用多模块 Kubernetes 发现的 System 从 `ADDP_KUBERNETES_DISCOVERY_BINDINGS` 指定的文件读取绑定。文件以模块名为键，例如 `{"catalog": {...}, "meta": {...}}`。每个值都按该模块实际的 Service UID、端口和信任材料填写。升级已有 Catalog 试点时，应在同一次 System 工作负载变更中更新配置文件及环境变量，并使所有 System 副本采用同一份配置。Meta 的部署继续保留原 Backend 和 Worker 的 System 登记凭据，以支持内嵌 Scheduler 及 Worker 领取工作。端点查询验收应分别使用 Gateway、Monitor 和 Transfer 自身的服务身份。

扩展到 Manager 等已部署模块时，先从站点交付记录取得获准的 Service 名、UID 和端口名，用同一条 `inspect-service` 命令核对。部署管理员将该 Service 名加入本 namespace 发现 Role 的 services 规则 resourceNames，保留 list、watch；EndpointSlice 规则和 RoleBinding 沿用原配置。将新增模块的绑定与现有绑定合并到新建的不可变 ConfigMap，更新 System Deployment 的投射卷引用，等所有 System 副本采用新配置并 Ready 后验收。Service 被同名重建时需重新确认新 UID。

Manager 验收先保留切换前的导出和 CSV 摘要。切换后用 Transfer 自身的平台服务身份查询 Manager 端点，核对 Pod UID、具名端口和地址，再通过 Gateway 完成同一测试表的导出并比较 CSV。临时撤回隔离试点的 Manager 端点时，Transfer 查询应返回空数组，Gateway 的 Manager 业务请求应返回 503。同期真实 Manager 进程继续向 System 续租。恢复原 Service 选择器后，应再次观察到可用端点和成功的业务请求。
