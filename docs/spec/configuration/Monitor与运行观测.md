# ADDP 配置：Monitor与运行观测

[返回配置规范](../addp配置介绍.md)

## 平台观测配置边界

指标、集中日志和追踪的部署选择、中心设施端点及进程启动输入属于部署配置，专用凭据属于 Secret。Monitor 拥有监测目标、阈值、采样/查询策略及其他普通运行配置；System 只维护节点和已有模块/实例/引擎身份，不代存观测策略。

未选择的能力不要求其专用 Secret，不创建远端发送；已选择但配置不完整与运行中不可用分别报告。可选观测配置不加入业务模块或 Monitor 整体 Ready 条件，不以环境变量覆盖已持久化的 Monitor 策略，不新增另一条存储或采集 fallback。标准生命周期按已选能力准备和校验组合配置，未选能力不要求其专用 Secret。部署/状态契约及验收要求见 [平台运行观测规范](../addp平台运行观测规范.md)。

Go 自身进程采集由唯一启动输入 `ADDP_PROCESS_METRICS_DEPLOYMENTS` 选择。空值表示未选择；非空值是最多 1000 项、256 KiB 的严格 JSON 数组，每项包含 `module_name`、`role`、明确本机 IP:端口 `listen`、同时对 Monitor 和中心可达的 HTTPS `/metrics` 地址 `endpoint`、仓库外绝对证书目录 `tls_dir`。同一模块/role 只能出现一次，各监听地址和采集端点不得重复。每个进程只消费匹配自身正式登记模块/role 的一项；不同部署或副本由部署方注入各自值。不能从业务 URL 拼接采集地址，不自动寻找端口，也不新增每模块默认端口表。

标准开发入口使用唯一 `ADDP_DEV_PROCESS_METRICS_PLAN` 编排上述输入，开发根 `.env` 不直接填写 `ADDP_PROCESS_METRICS_DEPLOYMENTS`。计划为空表示未选择，非空为最多 1000 个启动单元、256 KiB 的严格 JSON 对象；键是具体启动单元名称，每个值是上述五字段部署对象。启动单元沿标准生命周期名称，例如 `meta-worker-1`、`meta-worker-2`、`transfer-bounded-worker-1`、`transfer-bounded-worker-2`、`transfer-continuous-worker-1`、`transfer-continuous-worker-2`；两类 Transfer Worker 的正式 role 仍均为 worker。不同启动单元可有相同 module/role，监听地址、采集端点和证书目录须各自独立。完整计划拒绝重复键、未知字段、非字符串或空字段以及重复来源；启动时校验选中项与正式 module/role 一致。SDK 继续拥有端点、IP、端口及 TLS 的完整校验，不复制到启动编排层。

标准入口在真正创建每个 Go 子进程时，将选中项派生为单项 SDK 数组；未选择时显式注入空字符串。SDK 的根环境补齐不得覆盖该值。旧根 SDK 输入非空与新计划冲突、计划结构错误或选中身份不符时，只关闭本次可选采集并输出封闭诊断码，业务照常启动；不读取旧输入作为 fallback，不自动换端口。产品容器及其他部署系统继续直接注入各自进程的 SDK 输入，不消费开发计划。开发计划不自动增加副本；Meta 的单、双 Worker 由 `ADDP_DEV_META_WORKER_REPLICAS` 选择，Transfer 两类分别由 `ADDP_DEV_TRANSFER_BOUNDED_WORKER_REPLICAS`、`ADDP_DEV_TRANSFER_CONTINUOUS_WORKER_REPLICAS` 选择，均默认 1、只接受 1 或 2；持续同步双副本拒绝固定 `TRANSFER_CONTINUOUS_WORKER_INSTANCE_ID`。其他同类型多副本也必须先由标准生命周期明确支持，再分别声明启动单元，由部署入口向各进程注入独立值，运行实例 ID 仍在每次启动时自动生成。标准 Hosted 开发部署同样使用该计划，不保留第二种开发启动路线。

证书目录仅含部署提供的 `ca.crt/server.crt/server.key`，来源使用独立采集客户端 CA 做 mTLS；来源证书 SAN 必须覆盖声明地址及准入后固定的实际采集 IP，使用 DNS 声明时同时覆盖该 DNS 名称，不以 server_name 覆盖校验。SDK 启动校验声明地址，Monitor 与中心按唯一固定 IP 校验。平台不签发证书、不放宽 TLS、不复用中心健康身份。只有配置和端点实际启动成功才随当前进程登记发布私有采集声明；关闭、配置失败、证书错误或监听冲突只记录封闭诊断码，不阻塞业务登记或 Ready。此输入不是 System 普通配置或 Monitor 采集策略，改变它须重启对应进程生成新实例身份。System 普通实例 DTO 不公开端点或证书位置。

macOS 本地 Docker Desktop 的唯一宿主通道输入为 `ADDP_METRICS_DESKTOP_LOOPBACK_PORTS`，空值关闭转换，非空为最多 64 个不重复的规范非零端口，以逗号分隔且不含空项或空格。该输入只改变同一 HTTP SD Job 中已发现来源的传输地址：先保留原 `instance`，将列表内 `127.0.0.1:端口` 精确转换为同端口 `host.docker.internal`；其他地址与端口不转换。它不授权来源、不部署来源、不扩大采集预算、不覆盖 Monitor 的 CIDR/端口准入策略。来源服务器证书同时覆盖回环 IP 与宿主通道 DNS；不覆盖 TLS server_name。配置变更须由标准指标入口重新应用中心。原单端口输入已删除，不保留兼容读取。

## 告警评估与通知策略

Monitor 按当前配置周期评估每个任务最新根 execution 的公共 metadata，普通运行信号来自 pending/running，schema blocked 信号只来自该最新 execution 的 failed + schema_change.status=pending。更新的根执行取代旧信号，不回扫历史 pending 记录；具体口径见[Transfer 同步执行规范](../tasks/Transfer同步执行.md)。该配置不进入任务 JSON；Monitor 不因此读取 Transfer 私有表或业务 Kafka。

Monitor 的告警评估和 Webhook/邮件投递策略属于 Monitor-owned `platform_only` 普通运行配置，统一由配置管理入口 `monitor.configuration` 的运行策略分组维护。`/settings/runtime-policy` 读取和保存 Monitor 持久化配置，按版本执行并发校验；保存后按 `restart_required` 生效，Monitor Backend 在下次启动时加载配置，再创建 evaluator 和 dispatcher。配置读取或校验失败应明确报错，不回退环境变量。

运行策略的定义默认值如下，表中时间单位为秒；这些字段不能写入根 `.env`、任务 JSON 或普通用户请求：

| 策略 | 字段 | 定义默认值 |
| --- | --- | --- |
| 告警评估周期 | `alert_evaluation_interval_seconds` | 15 |
| Webhook / 邮件轮询周期 | `webhook_dispatch_interval_seconds` / `email_dispatch_interval_seconds` | 均为 2 |
| Webhook HTTP / 邮件 SMTP 超时 | `webhook_http_timeout_seconds` / `email_smtp_timeout_seconds` | 10 / 15 |
| Webhook / 邮件领取租约 | `webhook_lease_duration_seconds` / `email_lease_duration_seconds` | 均为 30 |
| Webhook / 邮件最大投递次数 | `webhook_max_attempts` / `email_max_attempts` | 均为 8 |
| Webhook / 邮件初始退避 | `webhook_retry_initial_backoff_seconds` / `email_retry_initial_backoff_seconds` | 均为 5 |
| Webhook / 邮件最大退避 | `webhook_retry_max_backoff_seconds` / `email_retry_max_backoff_seconds` | 均为 300 |

评估和轮询周期、超时、最大尝试次数及退避时间必须为正；领取租约必须大于对应投递超时，初始退避不得大于最大退避。默认值属于 owner 的配置定义，不是可与持久化值并行修改的另一份配置。

Webhook 私网访问开关和 Console 外部地址仍属于部署安全边界：

```bash
# 默认 false：只允许 HTTPS 公网目标；本地或受管内网联调时才显式开启。
MONITOR_WEBHOOK_ALLOW_PRIVATE_NETWORKS=false
```

`MONITOR_WEBHOOK_ALLOW_PRIVATE_NETWORKS` 只能由部署者设置，API 不提供绕过开关；关闭时拒绝 HTTP、环回、私网、链路本地和云 metadata 目标。通知链接使用部署输入 `MONITOR_CONSOLE_BASE_URL`：本地开发由 `scripts/dev/ports.sh` 注入 Console 实际地址，产品 Compose 从 `ADDP_PUBLIC_ORIGIN` 派生统一入口。Webhook destination 的 HMAC secret 使用平台统一 `ENCRYPTION_KEY` 做 AES-256-GCM 加密，不新增 Monitor 私有加密密钥。

SMTP Relay 作为 Monitor-owned 平台强类型资源，由同一配置入口的 SMTP Relay 分组通过 `/settings/smtp-relay` 管理。地址、端口、TLS 模式、发件身份和用户名是普通资源字段；密码只能通过独立 `/settings/smtp-relay/credential` 接口写入或删除，使用平台 `ENCRYPTION_KEY` 加密保存，读取接口的 `credential` 仅返回 `configured/version`。资源版本与凭据版本分别管理，均在下次 Monitor Backend 启动时加载。

邮件只允许 `starttls|tls`，不支持明文或机会式降级，也不根据端口猜测 TLS 模式。SMTP 未配置或 Relay 未启用时邮件 dispatcher 不启动，既有 `pending` outbox 会在补齐配置并重启后继续投递。启用时必须具有合法地址、端口和发件身份；配置用户名时必须同时具有密码凭据。SMTP 密码不进入 System Engine、任务 JSON、租户 API 或投递审计。

## 模块服务运行日志配置

`ADDP_OBSERVABILITY_LOGS_ENABLED` 是集中日志部署选择，缺省 `true`，只接受 `true/false`。`false` 时标准 Infra 入口不创建 Loki、Alloy、查询代理、观察器与日志存储初始化容器；已部署组件在核实工作区归属后停止，数据卷保留。日志独立凭据不参与核心启动校验，System 停用日志观察器 Client 并撤销其授权，Monitor 暂停新观测及链路评估；既有告警与投递历史保留，关闭不产生恢复事件；新的检测生命周期清零连续异常/恢复样本数，重新取得足够的新样本后才能判定恢复。部署值无效时仅该能力不可用。部署选择随组件和 System/Monitor 生命周期生效，不承诺热更新。

本地进程输出接收器与独立清理器继续工作，容量、时长和目录权限约束保持有效。核心 Infra 先启动并验证，再预检和启动所选日志组件；日志失败返回非零整体结果，开发与生产入口按核心服务实际状态继续启动业务并保留部分失败报告。Compose 解析会校验未选 Profile 的端口，核心操作将不参与运行的日志端口设为无分配占位值，日志阶段再单独解析实际映射。System 正文查询在关闭时返回既有 `503` 未接入响应，不读取宿主文件；Monitor 观测上报在关闭时返回 `409`，部署配置无效时返回 `503`。

`ADDP_PROCESS_INSTANCE_ID` 由标准启动入口生成，不写入 .env；`ADDP_RUNTIME_LOG_ROOT` 指向节点受控日志根目录。日志按实例分段，应用统一 JSON 输出。`ADDP_RUNTIME_LOG_SEGMENT_BYTES`、`ADDP_RUNTIME_LOG_INSTANCE_BYTES`、`ADDP_RUNTIME_LOG_NODE_BYTES` 和 `ADDP_RUNTIME_LOG_SOURCE_HOURS` 分别控制段、实例、节点限额与最长源保留期；额度优先于时长。技术探针 `runtime-probe` 的已关闭源段按最后写入时间保留最多一小时（不超过源保留配置），持续探针接收器按小时轮转；业务源仍使用配置时长。`LOKI_URL` 是 System 服务端受控查询地址，空值表示未接入；`LOKI_RETENTION_HOURS` 默认 168，须与 Loki Compactor 配置一致。日志存储和源文件不是零丢失归档，不以 positions 证明远端收妥。完整契约见 [模块服务运行日志规范](../addp模块服务运行日志规范.md)。

### 平台日志链路观测与通知

日志凭据初始化统一使用 `scripts/utils/observability-env.sh`。启用集中日志时，开发启动将缺失的日志秘密写入根 `.env`；Online 启动必须提供仓库外绝对路径 `ADDP_ONLINE_ENV_FILE`，且该文件不能位于 Artifact 目录。Hosted 在已准入的 owner-only 秘密目录内创建 `runtime.env`，在 Infra 启动前生成并导出凭据，退出时销毁；不生成根 `.env`，不放宽个人环境数据卷删除保护。已配置的秘密不轮换，无新增配置时不改写文件。

`ADDP_RUNTIME_LOG_OWNER` 是日志观察器与清理器的数值 `UID:GID`。标准宿主启动使用调用用户身份并提前创建 `logs` 和源目录；直接容器部署未覆盖时使用 `0:0`，覆盖值须与受控源目录所有者一致。root 业务容器中的独立日志接收器使用源目录所有者身份写入，业务进程身份保持原样；目录 `0700`、文件 `0600` 不放宽，也不自动修改既有目录所有权。

Infra `runtime-log-observer` 与应用接收器共享显式 `ADDP_HOST_NODE_NAME`；Monitor 使用相同节点配置绑定观测来源。缺少节点身份或独立 `LOG_OBSERVER_SERVICE_CLIENT_SECRET` 时观测器拒绝启动，不从载荷自由接入节点。开发 `infra/up.sh` 只生成缺失的凭据，并在没有节点配置时持久化当前宿主机名称；生产部署必须明确设置节点身份和凭据。System 使用这份 Secret 启用 `addp-log-observer` 的独立 Platform 服务账号。

`LOG_OBSERVER_SYSTEM_URL` / `LOG_OBSERVER_MONITOR_URL` 是 Infra 到控制面的受控地址，默认 `http://host.docker.internal:8180` / `http://host.docker.internal:8100`；使用非默认端口或容器部署时必须按实际地址设置。观测器每 30 秒采样，探针最多 20 秒，上报最多尝试两次，不上传日志正文。监测阈值通过 Monitor 平台规则 API 统一管理。

通知目标由平台管理员在 Monitor 唯一“平台告警”页面配置，System 日志链路页仅提供诊断和上下文跳转，Webhook 签名凭据与目标字段分开写入、加密保存；企业微信 `wecom` 目标的完整机器人地址仅通过凭据接口写入，key 使用现有 ENCRYPTION_KEY 加密，普通目标和投递读取不回显 key。邮件复用 Monitor-owned SMTP Relay。未配置目标或 SMTP 时明确显示未配置状态，不计为已通知。通知采用事务 outbox 和至少一次投递，通用 Webhook 接收方可按投递 ID 去重；企业微信群消息保留投递 ID，但可能重复或乱序，不提供接收方自动去重保证。失败达到尝试上限后保留最终失败记录。


## PostgreSQL 独立来源部署选择

Infra 根 `.env` 与 `business/.env` 各自的 `ADDP_POSTGRES_METRICS_ENABLED=false` 选择 PostgreSQL 采集账号和来源；只接受 `true/false`，独立于指标中心。启用需要 `ADDP_POSTGRES_METRICS_TLS_DIR`（仓库外、仅 CA／服务器证书／私钥）与 `ADDP_POSTGRES_METRICS_LISTEN`（显式本机私网或回环 IP:端口，无默认与避让）；可选 `ADDP_POSTGRES_METRICS_STATE_DIR` 为仓库外私有状态根，不设置时按当前用户与工作区隔离。密码首次随机生成并保存在私有文件，不进入环境变量、参数或发现结果。

由各自标准启动入口在 PostgreSQL 就绪后安装固定函数、验证并重载认证规则，成功后启用 Login 并部署来源；标准停止或关闭选择则先停来源并关闭本方 Login，保留历史与数据卷。可选失败不阻止核心运行，未知会话及凭据丢失拒绝自动修复，不重启数据库。Business 首次认证目录切换另由部署方安排标准重建。配置随下一次相应生命周期生效，不承诺热更新；来源启动不表示正式发现、专业查询或页面已就绪。完整操作见[PostgreSQL 采集账号与指标来源启停](../../../scripts/infra/docs/指标中心与来源.md#postgresql-采集账号与指标来源启停)。

## 指标中心部署选择

`ADDP_OBSERVABILITY_METRICS_ENABLED` 缺省 `false`，只接受 `true/false`，不根据端口探测或证书是否存在推断部署意图。中心采用 `observability-metrics` Profile，与 `observability-logs` 独立；共享生命周期准备只由 `scripts/utils/observability-env.sh` 组织。选择、证书和启动配置随下一次标准 Infra 生命周期生效，不承诺热更新。缺失或故障只影响指标设施，不加入业务 Ready。

`PROMETHEUS_PORT` 首选 `19090`，宿主固定回环发布，开发实际映射由标准入口解析；`ADDP_METRICS_TLS_DIR` 是外部部署证书目录，不是用户可修改的 Monitor 普通配置。启用需要独立 CA、服务器和健康客户端证书；未启用不校验这些专用输入、不创建证书目录。具体文件、用途与容器可读要求见 [Infra 指标中心](../../../scripts/infra/docs/指标中心与来源.md#可选指标中心)。运行状态分别报告 Disabled、Unconfigured、Not deployed、Unavailable 或中心 Ready；当前自身采集与业务资源覆盖分别解释。

节点发现的控制面 origin 由 `PROMETHEUS_SYSTEM_URL/PROMETHEUS_MONITOR_URL` 显式部署，固定 HTTPS 和 API 路径；`ADDP_METRICS_DEPLOYMENT_DIR` 提供独立控制面 CA、来源 CA、采集客户端证书与 Prometheus OAuth Secret 文件。Secret 值精确匹配 System 的独立服务凭据，生成配置只引用文件路径。30 秒 HTTP SD 刷新、15 秒采样及限额由同一版本化模板维护，生成产物不能成为第二份可编辑配置。未选择指标不校验这些输入；已选择但输入缺失只使该可选能力启动失败。节点 exporter 和来源防护入口独立部署，不因选择中心而自动安装或取得宿主权限。


主机来源由独立 `ADDP_NODE_METRICS_ENABLED=false` 选择；`scripts/infra/node-metrics.py` 只消费显式导出的根模板部署键，不创建 `.env` 或凭据。选择后要求 `ADDP_NODE_METRICS_LISTEN` 的规范本机 IP:端口，以及仓库外三文件 `ADDP_NODE_METRICS_TLS_DIR`；不根据中心开关或容器探测推断是否采集。同一正式入口支持本机原生 Linux Docker，以及 macOS 上的本地 Docker Desktop Linux VM；后者只发布已验证的 CPU、内存、负载和启动时间等内核全局事实，不提供磁盘/网卡全局指标。仍拒绝远程 Engine 和未知环境；详细权限和证书轮换边界见 [独立 Linux 主机指标来源](../../../scripts/infra/docs/指标中心与来源.md#独立-linux-主机指标来源)。部署成功不替代 System 身份登记、Monitor 目标准入或生产覆盖验收。


Monitor 的可选资源查询使用 `MONITOR_PROMETHEUS_URL` 与独立 `MONITOR_PROMETHEUS_CA_FILE/CLIENT_CERT_FILE/CLIENT_KEY_FILE` 部署输入；不复用采集、节点准入或中心健康证书。缺配置只使资源 API 返回能力未配置，不影响原有 Ready。端点/证书随服务生命周期生效；查询预算是 Monitor 平台普通配置 `/settings/resource-query-policy`，版本/CAS 保存后对新请求立即生效，不通过环境变量配置或回退；在途请求保留开始时的预算版本。具体保护上限和指标目录见 [平台运行观测规范](../addp平台运行观测规范.md#62-资源查询与预算)。


本地可选指标 TLS 转发由 `ADDP_METRICS_CONTROL_ENABLED=false` 显式选择，仅在指标中心也启用时生效。`ADDP_METRICS_CONTROL_DIR` 为仓库外服务器证书目录，`ADDP_METRICS_CONTROL_GATEWAY_URL` 为明确的规范 HTTP origin（含端口、无路径）；标准预检生成 nginx.conf，不签发证书。选中此入口后两个 `PROMETHEUS_*_URL` 均须为 `https://metrics-control:9444`，中心信任该入口独立 CA。关闭入口时由部署方提供原有 HTTPS 控制面输入，不校验入口专用目录或上游；关闭中心也停止其可选转发服务。入口只转发 Token POST 与指标发现 GET，不发布宿主端口，不参与业务 Ready。HTTP 上游属于受控开发网络部署边界，生产 TLS 和密钥权限须独立验收。


macOS 本地 Docker Desktop 的稳定指标访问由 `ADDP_METRICS_DESKTOP_LOOPBACK_PORTS` 显式列出来源回环端口，格式与限制见本文“平台观测配置边界”中的部署契约。中心唯一 HTTP SD Job 只将列出的 `127.0.0.1:端口` 传输地址映射到 `host.docker.internal:端口`，instance 仍保留 Monitor 的规范来源地址。叶子证书须包含两种访问名称，mTLS 身份与信任根不变；不放宽通配监听、自动改端口或读取任意目标。中心配置经标准 Infra 生命周期生效，不隐式重启业务进程；来源及 Monitor 的部署输入有变更时，分别由用户沿对应模块标准入口重启。

## Go HTTP 追踪部署

二期第一批使用 `ADDP_OBSERVABILITY_TRACES_ENABLED=true` 显式选择；默认关闭。`ADDP_TRACE_DEPLOYMENTS` 是严格 JSON 数组，每项仅含正式 `module_name`、`role`（backend/ingress）、HTTPS `/v1/traces` 的 `endpoint` 及仓库外绝对 `tls_dir`，每个 module/role 唯一。单个进程只消费自身项，未选择时不导出；不使用 OTEL_* 作为另一套配置。每个 TLS 目录仅含 `ca.crt/client.crt/client.key`，目录 owner-only、私钥 0600，部署者提供独立客户端证书，不复制到仓库、普通配置或浏览器。

中心 `ADDP_TRACE_DEPLOYMENT_DIR` 也是仓库外 owner-only 目录，部署者提供 `server.crt/server.key/source-ca.crt/query-ca.crt`；服务端证书 SAN 覆盖实际来源/查询访问地址，两套客户端 CA 分开。标准 Infra 入口从 Monitor 的 `internal/tracequery/policy.json` 生成共用 Alloy 与 Tempo 配置；日志启用时沿原管线合并，不能再启动第二个 Alloy。Tempo WAL 时间容差随等待加 180 秒生成，避免尾采样后原始历史窗口被排除。

Tempo 使用独立 `TEMPO_BUCKET/TEMPO_S3_ACCESS_KEY/TEMPO_S3_SECRET_KEY`，不得复用日志或 MinIO 根账号；初始化只管理该 bucket 的 20 GiB 配额及其最小账号。72 小时为按容量约束的保留配置，另预留约 5 GiB WAL/工作空间，不构成文件系统硬配额。Alloy/Tempo 资源、采样及预算计算见[正式规范 6.5](../addp平台运行观测规范.md#65-分布式追踪与日志关联二期第一批)。

Monitor 查询通过独立 `MONITOR_TEMPO_URL` HTTPS origin 和 `MONITOR_TRACE_TLS_DIR` 三文件客户端目录访问只读代理，不能复用来源证书。原生开发显式使用 Infra 实际回环映射；容器部署由唯一生产入口将 SDK 数组与私有目录投影为每服务独立只读挂载，查询目录仅挂 Monitor。改变部署输入由用户按标准生命周期重启相应进程；配置失败或中心故障不影响业务 Ready，接受导出不等于采样保留回执。

本轮只使用 `make test-monitor-traces` 的当次隔离设施验证，不接管个人服务，不扩展或运行 Hosted。
