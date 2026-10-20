# ADDP 配置：Business数据引擎部署

[返回配置规范](../addp配置介绍.md)

## PostgreSQL 运行认证文件

标准 `business/scripts/start.sh -postgres` 在容器启动前，将仓库模板 `business/postgres/pg_hba.conf` 准备为仓库外的受管运行文件，数据库只读挂载整个认证目录。可选部署输入 `ADDP_POSTGRES_AUTH_STATE_DIR` 指定绝对、当前用户私有的状态根目录；为空时使用 `~/.local/state/addp/<工作区路径摘要>/postgres-auth`。`ADDP_BUSINESS_POSTGRES_AUTH_DIR` 仅由启动入口派生，不放入 `.env`；直接 Compose 启动缺少准备结果时拒绝挂载，不回退到仓库模板。首次切换在标准启动时重建 PostgreSQL 容器，保留原数据卷；不由 Monitor 或 Web 请求重启数据库。

首次复制保持模板规则，后续保留通过校验的 Business 采集账号首部块。模板版本更新只在运行文件未漂移时应用；未知 owner、链接、权限异常或中断更新均拒绝覆盖。认证文件准备本身不安装采集角色、不保存密码、不重载运行数据库，也不等同于已启用专业采集。文件锁、原子替换及真实重载验证边界见[平台运行观测规范 4.7](../addp平台运行观测规范.md#47-postgresql-受保护的固定指标来源)。

Business 的独立采集选择由同一 `business/.env` 中 `ADDP_POSTGRES_METRICS_ENABLED=false` 管理。明确启用并提供 TLS、监听与可选私有状态目录后，标准 PostgreSQL 启动会接续安装账号、重载认证及启动来源，标准停止先关闭来源与 Login；不读业务表、不向租户分配整体用量。具体输入、故障隔离及首次切换见[Monitor与运行观测](Monitor与运行观测.md#postgresql-独立来源部署选择)。

## Business 数据引擎部署参数

Business MySQL 作为本地 CDC 测试源时使用独立配置文件 `business/.env`：

本地 `business/scripts/start.sh` 将 `POSTGRES_PORT`、`MYSQL_PORT`、`MINIO_API_PORT`、`MINIO_CONSOLE_PORT` 视为首次启动首选值；首次冲突自动选择空闲宿主机端口，成功后固定在 `business/.business-state/ports.env`。已登记的 Engine Instance 不随重启改写物理端点。宿主机开发进程登记实际发布端口；同一 Compose 网络内的进程登记 `business-*` 服务名和固定容器端口。

```bash
MYSQL_ROOT_PASSWORD=change-in-production
MYSQL_DATABASE=business
MYSQL_PORT=3306
MYSQL_CDC_USER=addp_cdc
MYSQL_CDC_PASSWORD=change-in-production
```

`MYSQL_CDC_USER` 只允许字母、数字和下划线，`MYSQL_CDC_PASSWORD` 不得为空且不得与 root 密码复用。`business/scripts/start.sh -mysql` 在 MySQL ready 后每次执行专用账号初始化，因此已有 volume 也会更新密码并把权限收敛到 Debezium 所需集合；不要把 root 凭据登记为 System MySQL Engine。Business Compose 显式固定非零 server id、binlog、ROW format 和 FULL row image。

Business OceanBase CE 使用独立配置，固定可重现的 `oceanbase/oceanbase-ce:4.4.2-lts` 镜像，不使用 `latest`：

```bash
OCEANBASE_IMAGE=oceanbase/oceanbase-ce:4.4.2-lts
OCEANBASE_MODE=mini
OCEANBASE_TENANT_NAME=test
OCEANBASE_PASSWORD=change-in-production
OCEANBASE_DATABASE=business
OCEANBASE_PORT=2881
```

本地容器是单机测试形态，不表达生产集群拓扑。System 注册时使用 `engine_type=oceanbase`、容器网络地址 `business-oceanbase:2881`、账号 `root@test` 和配置的 database/password；不得改登记为 MySQL Engine。

OceanBase 的容器内网 IP 属于其持久化单节点集群身份。Business 启动时从 OBD 配置读取已部署地址，在显式网段的 `business_business-network` 上恢复该地址；网段保存在忽略版本控制的 `business/.business-state/network.env`。标准停止与重启保留网络，地址冲突或网段无法恢复时拒绝启动，不以新 IP 初始化旧数据。宿主机连接仍使用回环地址和 `OCEANBASE_PORT`，不登记笔记本的局域网 IP。

Business TiDB 固定使用 PingCAP 官方 8.5.8 `pd`、`tikv`、`tidb` 三组件镜像及 OCI digest，Compose 不提供镜像覆盖入口；同一镜像契约同时用于 Linux x86_64 GitHub Hosted 与 macOS Docker Desktop。TiKV 容器固定声明 `nofile` soft/hard limit 为 `1000000`，满足官方对进程文件描述符上限的要求，Business 与 disposable T2 不得分叉配置。启动不读取 License 文件、不执行激活，也不得替换为 Enterprise 或第三方重打包镜像。

```bash
TIDB_DATABASE=business
TIDB_USER=root
TIDB_PASSWORD=
TIDB_PORT=4000
```

`business/scripts/start.sh -tidb` 启动三组件并幂等初始化普通关系样例。System 注册时使用独立 `engine_type=tidb` 和配置的 database/user/password；ADDP 服务以宿主机进程运行时连接 `localhost:${TIDB_PORT:-4000}`，以 Business Compose 同网络容器运行时连接 `business-tidb:4000`。不得登记为 MySQL Engine，也不得据 MySQL 协议兼容性声明 CDC、空间或分区变化应用能力。

Business 瀚高使用 `highgo` 独立 profile，固定社区镜像 `qiuchenjun/hgdb-see:4.5.10.3` 与 OCI 摘要，不进入 `start.sh -all`。配置归 `business/.env`：管理员 `HIGHGO_PASSWORD` 和业务 `HIGHGO_APP_PASSWORD` 必填且独立；`HIGHGO_USER` 默认 `business`、`HIGHGO_PORT` 默认 `5866`。数据库固定 `highgo`，业务 schema 固定 `addp_business`，System 接入普通业务账号。`HIGHGO_APP_VALID_UNTIL` 留空保留原生策略，明确确认后可仅将业务账号设置为 `infinity`。仓库替换镜像的硬编码密码初始化及安全降级样例，只用配置密码初始化三权角色并保留默认安全策略、SM3 和 TCP TLS。已有卷不自动轮换管理员密码；每次标准启动幂等初始化业务账号和样例，只在配置业务密码无法登录时通过 SYSSSO 的原生 `psql` 变量绑定改密，不降低全局安全策略。System 登记独立 `engine_type=highgo`，Business 生命周期不调用平台 API。

Business GaussDB 使用固定官方混合云开发者版 V2.0-10.0.0 Ubuntu 22.04 双架构介质构建本地镜像，不使用第三方重打包镜像，不对外发布。`business/scripts/start.sh -gaussdb` 必须独立调用，不进入 `-all`；配置归 `business/.env`：`GAUSSDB_PORT=5437`、`GAUSSDB_USER=business`，`GAUSSDB_PASSWORD` 与 `GAUSSDB_APP_PASSWORD` 必填且不同。SQL 仅映射到回环地址，端口冲突直接失败。独立 `gaussdb_data` 卷保存数据库与初始化完成标记；只有空卷允许初始化，已有数据库缺少完成标记、版本或账号不一致时拒绝启动，不删除或重建。首次完成管理员改密、普通账号及 `business.addp_business` 样例初始化后才允许远程连接；重启核验已有密码，不自动轮换或重置用户数据。Business 独占镜像构建、启动与样例，Release 双架构认证复用同一路径，不调用 System API。该部署仍受开发者版非商业学习、适配及测试使用限制；正式 `engine_type=gaussdb` 插件和 T2/T4 接入另行验收。

Business StarRocks 使用独立 `starrocks` profile，不进入 `start.sh -all`；Compose 唯一固定官方 3.5.21 ARM64／amd64 镜像及摘要。`business/.env` 配置 `STARROCKS_PORT=9031`、`STARROCKS_HTTP_PORT=8031`、`STARROCKS_BE_HTTP_PORT=8041` 和 `STARROCKS_USER=business_reader`；`STARROCKS_PASSWORD`（管理员）与 `STARROCKS_APP_PASSWORD`（普通只读账号）必填、至少12字符且不同，已有账号不自动改密。三个端口只绑定回环，显式配置端口冲突时拒绝启动。`business/scripts/start.sh -starrocks` 保留官方单机 FE／BE 和 director，固定 hostname 与两个持久卷；仅新空卷允许首次设置管理员密码，无管理标记的既有元数据拒绝初始化。就绪同时核对 FE 认证、BE 心跳与非零磁盘报告，随后仅补缺失样例及只读账号。停止和重建不删除卷、不覆盖用户修改，Business 不调用 System API，平台登记使用独立 `engine_type=starrocks` 和普通账号。该部署的实库认证范围见 [Business README](../../../business/README.md#starrocks-business-部署)，未通过的架构和页面验收不计为通过。

Business openGauss 固定使用 openGauss 6.0.6 LTS 官方 Docker tar，不接受第三方镜像或可变 tag。当前 `business/scripts/start.sh -opengauss` 只允许在具备 NUMA 的 Linux x86_64 主机选择并校验官方 x86_64 介质，加载为 `opengauss:6.0.6`；首次下载约 1.4 GB，后续复用本地镜像和缓存。官方 6.0.6 Docker 构建包含 MOT，而 Docker Desktop for macOS 不提供它需要的 NUMA 拓扑，因此 macOS 本地巡检不启动 openGauss，实库门禁由 GitHub Actions Linux x86_64 Job 承担。`scripts/lib/opengauss-official-media.sh` 仍维护 aarch64 官方介质事实，供后续具备 NUMA 的 Linux ARM64 专项认证使用，未认证前不得作为 Business 默认路径。

Business KingbaseES 不进入通用 `.env` 或 `scripts/start.sh -all`。`business/docker-compose.yml` 只声明独立 `kingbase` profile；`business/scripts/start.sh -kingbase` 必须独立调用，并由 `business/scripts/kingbase.sh` 只从当前 shell 读取 `KINGBASE_DATABASE`、`KINGBASE_USER`、`KINGBASE_PASSWORD`、`KINGBASE_PORT`、`ADDP_KINGBASE_LICENSE_FILE` 和 `ADDP_KINGBASE_LICENSE_SHA256`，按 Compose create、License 注入、Compose start 的唯一顺序管理容器。License 必须是仓库外绝对路径，密码和 License 不得写入仓库。官方介质按宿主 CPU 在同一 build 的 x86_64 与 ARM64 tar 中选择并校验固定 SHA-256，加载后统一使用 `addp/kingbase:v009r001c010b0004` 本地引用；macOS ARM64 只承担本地功能评估，正式 T5/T2/T4 仍固定 owner-managed Linux x86_64。容器归属 Docker Desktop 的 `business` Compose 分组，只绑定 `127.0.0.1:${KINGBASE_PORT:-5436}`，不使用数据卷；System 必须以 `engine_type=kingbase` 和 PG 模式连接信息登记。

KingbaseES T5、T2 与 T4 workflow 的 `addp-kingbase` GitHub Environment 只配置 Repository Variable `ADDP_KINGBASE_GATE_ENV_FILE`，值为专用 Runner 上仓库外 owner-only 文件的绝对路径。该文件权限必须拒绝 group/other，至少包含 `ADDP_KINGBASE_LICENSE_FILE`、`ADDP_KINGBASE_LICENSE_SHA256` 与 T2 使用的 `ADDP_TEST_KINGBASE_PASSWORD`；License 路径同样必须位于仓库外。不得把 License 内容、License 路径、数据库密码或环境文件内容保存到 GitHub Repository Secret、checkout、日志或 Artifact。

Doris Engine 的 `host`、`port`、`database` 继续标识 MySQL 协议连接；`http_port` 独立描述同一 FE 的 HTTP 部署端口，表单默认 8030，不参与永久实例身份，也不由 JDBC 端口推断。Spark 保存使用该端口与官方连接器，已有登记缺少该事实时须显式补齐实际映射。Driver/Executor 同时需要到 FE 和导入 BE 的真实网络连通性；不把容器内部 HTTP 端口当作宿主映射。

Business 的本机 Doris 只发布回环端口：FE 的 MySQL/HTTP 端口由 `DORIS_FE_PORT`/`DORIS_FE_HTTP_PORT` 配置；BE 的 HTTP `8040`、heartbeat `9050`、RPC `9060`、brpc `8060` 固定内外一致。BE 公布 `host.docker.internal`，不检测笔记本局域网 IP。宿主机和 Doris 容器均将该名映射到各自的 `127.0.0.1`；其他容器使用 Docker Desktop 宿主网关解析，两端解析是启动前提；已有单 BE 原地更新 HOSTNAME，身份与数据保持不变。`business/doris/init.sh` 是唯一初始化及健康检查入口，复用官方 all-in-one 启停，不再提供独立 BE 容器初始化路线。重启仍由用户执行 `bash business/scripts/restart.sh -doris`。

Business 达梦 DM8 ARM64 不进入通用 `.env` 或 `scripts/start.sh -all`。`business/scripts/start.sh -dameng` 必须独立调用，仅从当前 shell 接受可选 `DAMENG_PORT` 和仓库外临时介质缓存目录 `ADDP_DAMENG_MEDIA_CACHE`；数据库测试账号与初始化密码固定在 disposable 本地镜像路线中，不得用于生产。脚本只接受 Docker Server `linux/arm64`，校验官方 ZIP 与内层 ISO SHA-256 后构建 `addp/dameng:dm8-20260708-arm64`，并用无卷 `business-dameng` 暴露 `127.0.0.1:${DAMENG_PORT:-5236}`。System 使用插件 ConnectionSpec 保存 `engine_type=dameng` 的 `host`、`port`、`user`、`password`；官方驱动仅由专用脚本从同版本镜像提取并校验后注入 Linux ARM64 Docker 执行进程，不进入 checkout、Module cache 或 Artifact。

```bash
OPENGAUSS_PASSWORD=change-in-production
OPENGAUSS_DATABASE=business
OPENGAUSS_PORT=5435
```

本地容器使用 PG 兼容 database，固定账号 `gaussdb`。System 注册时使用 `engine_type=opengauss`、容器网络地址 `business-opengauss:5432` 和配置的 database/password；不得登记为 PostgreSQL Engine。Business 环境只表达非空间表能力，不声明 PostGIS、CDC 或生产集群拓扑。

Business Kafka/Redpanda 是用户业务消息流，与 Infra Kafka 完全隔离。开发环境通过 `business/scripts/start.sh -redpanda` 启动独立 Redpanda 集群，再把只读账号作为 `engine_type=kafka` 的 System Engine 凭据；不得把 Infra Kafka 的 endpoint、principal 或内部 topic 注册为业务 Engine。

```bash
BUSINESS_KAFKA_PORT=29092
BUSINESS_KAFKA_ADMIN_USERNAME=admin
BUSINESS_KAFKA_ADMIN_PASSWORD=change-in-production
BUSINESS_KAFKA_READER_USERNAME=addp_transfer
BUSINESS_KAFKA_READER_PASSWORD=change-in-production
```

本地 Business Redpanda 固定使用 `SASL_PLAINTEXT/SCRAM-SHA-256`，关闭自动创建 topic。启动脚本幂等创建或轮换 reader 密码，并只授予 topic read/describe、consumer group read/describe 和 cluster describe；业务生产者使用独立 principal，不复用 ADDP reader 凭据。生产环境应升级为 `SASL_SSL`，并按业务流量配置多 broker、副本、retention 和磁盘告警。


Business FTP 通过独立 `-ftp` profile 启动，不加入 `-all`。Compose 固定官方 SFTPGo v2.7.6 多架构镜像及 digest，回环端口固定为控制2121／被动50000–50009、内外一致、冲突失败。`business/.env` 使用 `FTP_USER`、必填且至少12字符的 `FTP_PASSWORD`、默认 `FTP_SECURITY=explicit_tls`；选择明文必须显式配置 `plain`。脚本在私有 `.business-state/ftp/` 下生成并保留本机证书，容器以非 root 身份读取内层配置，客户端必须校验证书和名称。独立文件／账号状态卷持久化；只读账号、账号配置身份核验和三个增量示范文件由 `business/ftp/` 唯一维护；种子生成到 Git 忽略的 `ftp/data/`，再由唯一入口补入持久卷。协议根固定为账号虚拟 `/`，不配置客户端 `root_path`。初始化不覆盖用户文件、不隐式轮换账号，不调用 System API。FTP 只读引擎的实际支持范围与验收证据另行认定。
