# Business Database

## 部署来源清单

Infra 生命周期管理独立发现进程，Business 和开发启动幂等确保其存在；进程每轮完成后间隔 30 秒刷新当前工作区的部署发现快照，Infra 与 Business 分别保存，发现失败不阻断服务。唯一读取实现为 `scripts/utils/deployment-inventory.py`；目录配置、权限及失败语义见[部署环境与密钥](../docs/spec/configuration/部署环境与密钥.md)。

管理员在 System“平台引擎部署”选择已发现的服务并确认承载主机，无需手工导出或导入文件。首批支持 Infra／Business 单容器 PostgreSQL；其他组件显示发现事实及未适配原因。发现、纳管和专业采集分别表达，平台运维关联不授予租户连接或业务数据权限。Docker Desktop 的来源属于 Linux VM，不属于原生 macOS 主机。

发现只读当前工作区 Compose 的本地 Unix Socket Context，拒绝远程端点及环境覆盖，不读取容器环境、业务表或凭据，也不启动或重启服务。确定性校验使用 `make test-deployment-discovery`，完整 Business 校验使用 `make test-business-config`，均由现有 Platform CI 覆盖。

## StarRocks Business 部署

独立 `-starrocks` profile，不进入 `-all`。使用 Compose 唯一固定的官方 3.5.21 ARM64／amd64 镜像，保留原生单机 FE／BE 和 director；SQL、FE HTTP、BE HTTP 仅绑定回环9031、8031、8041，端口可显式配置，冲突直接失败。`starrocks_fe_data` 与 `starrocks_be_data` 持久卷、固定 hostname 保证重建时不丢失引擎身份。Business 不登记 System 引擎。

在 `business/.env` 分别填写至少12字符且不同的 `STARROCKS_PASSWORD`（原生 root）和 `STARROCKS_APP_PASSWORD`，普通账号 `STARROCKS_USER` 默认 `business_reader`。通过 `bash business/scripts/start.sh -starrocks` 启动，`stop.sh -starrocks` 停止，`restart.sh -starrocks` 重建应用配置但保留卷。初始化先启动官方 FE，设置首次管理员密码，再启用使用该密码的官方 director 注册 BE，避免空密码与原生初始化检查竞态；不替换 FE／BE 运行实现。

首次空卷创建 `business.source` 的有序复合主键、bigint、Decimal(38,18)、中文、NULL／空串和尾随空格样例，并授予普通账号该库表的 SELECT。启动只补缺失样例，不覆盖已有行、不自动轮换密码；原生数据存在但没有 ADDP 管理员标记时拒绝初始化，密码错误时拒绝就绪，不删除卷。平台登记使用普通账号。

`starrocks/init.sql` 还在同一 `business` 库创建三张业务示范表：`customers`（5个湖南城市客户）、`products`（6项物联网设备／空间数据／分析服务）、`orders`（10笔订单）。可用于目录扫描、中文预览、日期／布尔／Decimal 类型查看、按城市／商品分类／订单状态聚合及三表关联；10笔订单金额合计39179.40，包含已取消和待付款订单，不能当作已实现收入。客户邮箱使用保留的 `.invalid` 示例域名。全部样例按固定主键执行 `WHERE NOT EXISTS`，已有记录和用户修改保留，缺失样例下次标准启动时补齐；普通 reader 沿现有 `business` 库 SELECT 授权可读取。对已运行实例，需要用户执行 `bash business/scripts/restart.sh -starrocks` 后重新扫描；不需要重启 ADDP。

示例查询：

```sql
SELECT c.city, COUNT(*) AS order_count, SUM(o.order_amount) AS order_total
FROM business.orders o
JOIN business.customers c ON o.customer_id = c.customer_id
JOIN business.products p ON o.product_id = p.product_id
WHERE o.status = '已完成'
GROUP BY c.city
ORDER BY order_total DESC;
```

正式插件与范围内确定性测试已具备。Linux 双架构正式门禁 [39270948341 / ARM64 Job 118122783775](https://github.com/pampa0629/addp/actions/runs/39270948341/job/118122783775) 与 [amd64 Job 118122783767](https://github.com/pampa0629/addp/actions/runs/39270948341/job/118122783767)，源码 `22fce34b3b774ef32b148268df09c8a37cbda66c`，均通过五项原生协议测试、七组 Decimal 子例、两项 Provider 集成测试和 Business 持久化验证，无跳过、清理零残留；实库版本为 `3.5.21-85fccf8`。真实验证只读账号拒写、bigint／Decimal 精度、容器重建后 Backend ID 与用户修改保留。

2026-10-20 核验 [Hosted T4 39271441409 / Job 118124504710](https://github.com/pampa0629/addp/actions/runs/39271441409/job/118124504710) 的完整报告及执行日志：同一源码下六服务通过构建身份核验，独立身份登记引擎，普通十权限 User 完成 Meta 扫描、Manager API／Console 预览及属性 JSON、Develop 全表查询和文本／整数参数查询。bigint 与 Decimal(38,18) 精确字符串、中文、尾随空格、NULL／空串及参数原值均通过断言，应用清理通过、Infra 与 Business 零残留。该范围仍不含 Transfer 写入、Spark、外部 Catalog、视图、跨批快照或分布式部署。完整证据见[验收场景](../docs/spec/testing/scenarios/同步与引擎消费.md#starrocks-consumer-flow)。

2026-10-20 本机 macOS ARM64 Docker Desktop 已通过开发管理员交互验收：`Business StarRocks` 登记为 Engine 32，版本探测正常；`business.source` 为 DataItem 44684，深度扫描执行 `287694dd-0879-4bc3-a6c4-18523459acd9` 成功，识别1表4字段。Manager 真实单元格及属性 JSON 保留有序主键、bigint、Decimal(38,18)、中文、尾随空格和 NULL／空串；Develop 全表查询 `9df8653e-b7b5-404b-a81e-bf84297f514e` 与文本／整数参数查询 `95f1cffb-f687-4d2f-905a-aad55f95a96f` 均成功，含引号文本原样返回，整数参数3只返回目标一行。期间发现并修复参数面板对新增默认值属性的响应跟踪缺陷，空串、0、false 与未设置的语义不变。本机管理员证据不替代 Linux amd64 或普通最小权限 User 的 Hosted T4。

包含默认值修复 `bfe5dd48b` 的 [Hosted 复验39283946474 / Job118164522204](https://github.com/pampa0629/addp/actions/runs/39283946474/job/118164522204)，源码 `1280b746f42e17c3b2a2a6d3583259abba11e443`，已通过并完成领域报告审计：普通十权限 User 经页面勾选并填写文本／整数默认值，preflight 与执行请求保持原值和类型，全表／参数查询返回2／1行，精确数值及原生空值／尾随空格通过；六服务同提交、报告一致、应用清理通过且 Infra／Business 零残留。默认值修复的云端待验项已关闭，证据与范围见上述验收场景。

## GaussDB Business 部署

采用固定官方混合云开发者版 V2.0-10.0.0 Ubuntu 22.04 ARM64／amd64 介质，构建 ADDP 本地镜像；不是厂商预制镜像，也不对外发布。仅限开发者版声明允许的学习、适配及测试。介质 URL、内外层哈希与基础镜像固定事实复用 `scripts/lib/gaussdb-official-media.sh`，不替换为 openGauss。

在 `business/.env` 设置不同的 `GAUSSDB_PASSWORD`（管理员 omm）、`GAUSSDB_APP_PASSWORD`（普通账号），密码8至32字符且符合原生复杂度策略。`GAUSSDB_USER` 默认 `business`，SQL 默认 `127.0.0.1:5437`，可显式配置 `GAUSSDB_PORT`，冲突失败。由用户执行：

```bash
bash business/scripts/start.sh -gaussdb
bash business/scripts/stop.sh -gaussdb
bash business/scripts/restart.sh -gaussdb
```

`-gaussdb` 必须独立使用，不进入 `-all`。首次下载校验介质后构建本机架构镜像，首次空卷创建 `business` 数据库及 `addp_business` 下探针、客户、商品、订单和明细；样例事务仅补缺失主键，不覆盖用户修改。初始化脚本退出后，由普通业务账号另建连接，只读核对五张表中的基础样例主键全部存在，再写完成标记；表缺失、样例不全或查询失败均不写标记，不把客户端空跑当作初始化成功。`gaussdb_data` 卷在停止及重启后保留；已有数据库缺少完成标记、版本／账号不匹配或密码错误时拒绝启动，不重新格式化，不自动轮换密码。管理密码变更须先通过原生数据库维护完成，再同步配置。Business 不登记 System 引擎。

构建上下文中的 `Dockerfile`、`entrypoint.sh` 和 `init.sql` 不含凭据，由唯一构建入口显式设置为0644；镜像内入口另设为可执行。不能依赖调用终端的 umask：本机启动脚本使用077时，原先复制出的 `init.sql` 为0600，Docker 保留该权限，导致普通运行用户 `omm` 报 `cat: /addp/gaussdb/init.sql: Permission denied`。此错误会保留缺少完成标记的不完整卷；修复镜像权限后仍须先核对数据并获得用户许可才能清理重建，启动脚本不会自动删除或接续初始化。`make test-business-config` 覆盖077下的上下文权限，`umask 077; make test-common-gaussdb` 复验实际初始化和 Provider 读取。

原生 `gsql` 的凭据通道固定采用[厂商文档](https://support.huaweicloud.com/centralized-ref-v10-gaussdb/gaussdb-38-0862.html)的 `-2`：密码走标准输入管道，SQL 由独立文件描述符交给 `-f /dev/fd/3`，不生成临时凭据文件、不把密码放入参数。SQL 在私有 `/data/run` 中写入0600临时常规文件，打开只读描述符后立即删除路径，再启动客户端，使 `-f` 接收可定位的完整脚本；函数在子 shell 中关闭描述符，异常退出也清理临时路径。固定介质的客户端不能套用 PostgreSQL `psql` 的 `-w`、`PGPASSFILE` 或 `PGPASSWORD` 登录方式；介质中 `libpq` 出现某个环境变量字符串，不能证明 `gsql` 会使用它。连接失败保留原生诊断，认证与密码策略不降低。

TCP 自动化查询使用原生 `-o /dev/fd/4` 独立结果通道，调用方只接收查询数据，客户端普通运行信息不进入结果；SQL、密码和结果分别走独立描述符。固定客户端的 `-f` 模式即使带 `-q -At` 也仍会打印 `total time: … ms`，因此不能用安静模式保证健康检查的精确比较，也不通过过滤文本修补。Release 归档保留每次健康检查的退出码和诊断并脱敏；初始化阶段的临时内核不计为最终就绪。

Schema 使用[该版本通用 SQL 参考](https://support.huaweicloud.com/intl/en-us/centralized-ref-v10-gaussdb/gaussdb-38-0274.html)支持的普通 `CREATE SCHEMA`，只在首次初始化执行；已完成卷直接复用。`CREATE TABLE IF NOT EXISTS`、索引和缺失样例的幂等维护仍在同一条初始化路径，不切换数据库模式来接受其他方言。

构建／初始化由 `business/gaussdb/` 唯一维护，既有 `make test-release RELEASE_SUITE=gaussdb-official-media` 双架构门禁复用它，新增独占数据卷、删除并重建容器后用户数据及样例修改保留的检查；所有退出路径核对容器、网络、卷、镜像零残留。确定性验证为 `make test-business-config` 和 `make test-release-runner`。

2026-10-18：[原生验收39090295754](https://github.com/pampa0629/addp/actions/runs/39090295754)，运行源码 `454b71ec7f94310a29c4d06664cd21697a4e16a3`。Linux ARM64、amd64 均实际通过：五张样例表准确位于 `business.addp_business`，普通账号新连接核验基础记录，删除并重建容器后数据卷、用户修改“持久化保留中文”及额外记录42均保留；两种架构均实际执行并通过四平台编译、驱动共存、精确数值、只读事务及取消恢复，清理零残留。该证据确认 `-f` 采用可定位的常规文件后完整样例初始化与重建持久化可用；此前管道输入只有客户端成功退出而无样例表的失败不计通过。本机首启失败的容器和不完整卷已获用户授权清理，初始化模板权限修复见 `4ce23d780`；用户标准启动后，macOS ARM64 Docker 正式 Business 实例已健康，普通业务账号新连接核验五表及基础样例通过。正式 `engine_type=gaussdb` 只读插件已落地，System 登记为 Business GaussDB（本机 ID 31）且连接测试成功；当前开发管理员经 Console 深度扫描成功（执行 UUID `14e62759-6e5a-482d-9e5d-e5bfe1c912c6`，5个数据项、28字段），Manager 五表预览与行数1/3/4/3/3一致，中文、Boolean、时间及商品 Decimal(12,2) 金额和主键／字段类型核验通过；此本机管理员验收不替代普通消费 User 的 Hosted 隔离认证。Provider T2 已在 macOS ARM64 Docker 通过 SELECT-only 目录、原生复合主键顺序、读取会话、精确数值及取消恢复并核对清理零残留；Linux 双架构 Provider T2 和 Hosted 平台 T4 以各自运行结果认定，不将入口登记或排队计为通过。此前协议 T5 通过不替代本次 Business 生命周期证明。

2026-10-18：独立 GaussDB Provider 的[Linux 双架构 T2 39127525840](https://github.com/pampa0629/addp/actions/runs/39127525840)在精确源码 `4ce23d780b06d7f715ed8e7e754d70ac0ca66eb7` 上通过 ARM64 Job `117684461819`、amd64 Job `117684461837`；两项实库测试均执行且无跳过，退出码0、清理零残留。该记录只证明 GaussDB Job，不将同流水线的其他服务失败或聚合状态计入。Hosted 平台运行 `39127556160` 曾因 Meta 将原生复合主键 `name,id` 覆盖为字段顺序 `id,name` 失败；`b77649f3d` 删除深度扫描和单项刷新中的两处覆盖，Meta Go、22项真实 PostgreSQL 标准门禁及本机 GaussDB Provider 回归通过。[Hosted T4 39132521164](https://github.com/pampa0629/addp/actions/runs/39132521164)在 `b77649f3dd430d963bbf0dcbf81b1f765a4dabbc` 上已通过字段／有序主键／行数校验，但因测试部署缺少 Security 及 Manager 保护投影初始化，预览按正式规则返回403。`d38b7b4e2` 已补齐标准 Security 生命周期、同提交 preflight 和 System 既有只读 `--wait-protection-owners manager`，不增加用户权限或绕过保护检查；GaussDB单元及生命周期13项、场景分发12项与CI登记检查通过。[Hosted T4 39136787617](https://github.com/pampa0629/addp/actions/runs/39136787617)在精确源码 `d38b7b4e2492f8ff8993b06d4ced118276807803` 上已实际通过：System／Gateway／Meta／Manager／Security 同提交和构建指纹检查，普通消费 User 仅持有既定六项目录／扫描／预览权限，API 与 Console 同一主体；验证唯一获准源表、深度扫描、原生有序复合主键 `name,id`、bigint `9007199254740993`、Decimal(38,18)、中文、尾随空格与原生 NULL。浏览器预览及属性 JSON 断言通过并归档两张截图；原生 Meta 落库保留主键顺序、四个字段及精确2行。Business 夹具清理 `remaining_resources=0`、应用／凭据清理通过、Infra `zero_residuals`。该证明覆盖只读首版平台链路，不扩展为写入、空间、CDC 或 Spark 接入。

## 概述

业务库为 ADDP 平台提供业务数据存储，与 ADDP 系统库完全独立部署。

**包含服务**：
- **PostgreSQL (PostGIS)**：业务数据库，端口 5433
- **Oracle Free 23ai**：普通表与 Oracle Spatial 测试源，端口 15210，service name `FREEPDB1`；固定使用保留 Spatial/Locator 的常规镜像 `gvenzl/oracle-free:23`，不得使用 `-slim` 镜像。
- **SuperMap SDX+ for PostgreSQL**：独立原生 PostgreSQL 实例，端口 5434；不安装 PostGIS
- **MinIO**：业务对象存储，端口 9002-9003
- **ClickHouse** 🆕：高性能列式存储 OLAP，端口 9000, 8123
- **MongoDB** 🆕：文档型 NoSQL 数据库，端口 27017
- **MySQL 8.0**：支持 Spatial 与 CDC 的业务关系库测试源，端口 3306
- **OceanBase Community Edition 4.4.2 LTS**：Apache 2.0 授权、MySQL 模式的国产分布式关系数据库测试源，端口 2881；固定使用 `oceanbase/oceanbase-ce:4.4.2-lts`。
- **TiDB 8.5.8**：Apache 2.0 的国产分布式 SQL 数据库测试源，端口 4000；固定使用 PingCAP 官方 PD/TiKV/TiDB 三组件镜像及 OCI digest，不提供镜像覆盖入口。
- **openGauss 6.0.6 LTS**：Apache 2.0 授权、PG 兼容模式的国产关系数据库测试源，端口 5435；当前只在具备 NUMA 的 Linux x86_64 主机使用并校验官方 Docker tar，统一加载为 `opengauss:6.0.6`。
- **KingbaseES V9R1C10**：固定 `V009R001C010B0004` 官方 Docker tar 与 SHA-256、使用 owner 正规 License 的 PG 模式国产关系数据库测试源，端口 5436；只通过 owner-managed Linux x86_64 disposable 生命周期启动。
- **达梦 DM8 ARM64**：固定鲲鹏 920/麒麟 10 SP1 官方安装 ZIP、服务端/驱动 SHA-256，本机只构建不可推送的 disposable 镜像，端口 5236；以独立 `engine_type=dameng` 登记，真实 Provider 只在 Linux ARM64 Docker 中执行。
- **Apache Doris**：实时分析数据库，FE 端口 9030/8030，BE 端口 8040/9050/9060/8060，仅绑定回环
- **Apache Spark**：分布式计算引擎，主机端口 7077、18088、11000；默认 Worker 为 Thrift 查询和工作流执行分别保留执行资源
- **Redpanda**：兼容 Kafka API 的业务消息流，端口 29092

Business 的 Doris all-in-one 服务是固定单 FE、单 BE 的本地开发拓扑，FE 因此固定使用 `force_olap_table_replication_num=1`。生产 Doris 集群不复用该单节点配置，应按实际 BE 数量和容灾策略设置副本数。

Doris 的 BE 公布稳定主机名 `host.docker.internal`，宿主机须配置 `127.0.0.1 host.docker.internal`，仅 Doris 容器将该名称解析到自身回环地址，其他容器使用 Docker Desktop 宿主网关解析；笔记本换网不修改引擎地址。FE 的 MySQL/HTTP 端口继续由 `DORIS_FE_PORT`/`DORIS_FE_HTTP_PORT` 指定，BE 四个端口固定内外一致，不能自动改选。`doris/init.sh` 复用官方启停入口，已有单 BE 通过 `MODIFY BACKEND ... HOSTNAME` 原地迁移，保留 Backend ID、已有表与数据卷；不使用 DROP/ADD 重建已有节点。健康检查必须确认 FE 可查询、唯一 BE 的地址正确且 Alive。应用部署调整由用户执行 `bash business/scripts/restart.sh -doris`；普通 `start` 不接管已运行的旧配置容器。

首次初始化通过 FE 自身提供的 [PROC 节点目录](https://doris.apache.org/docs/2.1/sql-manual/sql-statements/system-info-and-help/SHOW-PROC/) 读取 Backend 登记事实，不能在注册 BE 前依赖需要可用 BE 执行的元数据查询。MySQL 批处理客户端在空结果时不输出表头；以查询退出状态区分失败与空目录，只有查询成功且目录为空时才首次登记 BE。独占 Hosted Spark 表发布门禁验证新库初始化、重复登记保留 Backend ID 和连接器写入；历史 IP 地址迁移与反例由 `make test-business-config` 的确定性夹具覆盖。本机已有 Backend 10004 的迁移另已完成真实重启验证，不把它计为 Hosted 迁移覆盖。

**关键特性**：
- ✅ 独立部署，无依赖
- ✅ CPU 架构自适应（ARM64/AMD64）
- ✅ PostGIS 空间数据支持
- ✅ MySQL 全二维几何族、SRID 与空间索引测试数据
- ✅ Oracle `MDSYS.SDO_GEOMETRY`、完整二维几何族、SRID、空间索引与 EWKB 读取测试数据
- ✅ Oracle ARCHIVELOG、LogMiner 专用 common user、普通表与 Oracle Spatial CDC readiness
- ✅ 幂等启动脚本
- ✅ 模块化启动（按需启动服务）
- ✅ Business Kafka 与 ADDP Infra Kafka 物理隔离

## 瀚高 V4.5 本地业务库

瀚高使用已确认的社区镜像 `qiuchenjun/hgdb-see:4.5.10.3`，固定 OCI 摘要 `sha256:e4dd0ac4877c3a8818ec4cef68108be8d54c410782a6248a01257ea66dd49725`，不是官方镜像。默认匹配 Docker Server 架构。Linux ARM64 已完成本机原生验证；Linux x86_64 已通过 GitHub Hosted [HighGo T4 验收](https://github.com/pampa0629/addp/actions/runs/38884152132)，覆盖普通业务账号与 TLS、四表目录和关系元数据、Console 预览及两轮 PG → HighGo 覆盖同步，并核实一次性容器与匿名卷零残留。完整事实与范围见 [数据引擎扩展指南](../docs/spec/addp数据引擎扩展指南.md)。

`highgo` 为独立 Compose profile，不进入 `start.sh -all`。标准入口 `bash business/scripts/start.sh -highgo` 从 Business `.env` 读取 `HIGHGO_PASSWORD`、`HIGHGO_USER`、`HIGHGO_APP_PASSWORD` 和 `HIGHGO_PORT`，只管理 `business-highgo`，不调用 System API。数据库固定为原生 `highgo`，业务样例在 `addp_business` schema；以独立 `engine_type=highgo` 登记，不改登记为 PostgreSQL。

初始化复用固定镜像的服务启动流程，由仓库入口替换硬编码三权密码的初始化函数；三个管理角色的初始密码直接来自 `HIGHGO_PASSWORD`。仓库只挂载自己的幂等样例 SQL，不执行镜像的 `10_setup.sh`，因此不降低口令策略、关闭审计、创建高权限额外账号或用 SQL 字符串拼接密码。保留 SM3，TCP 仅接受 TLS；仅发布 `127.0.0.1:${HIGHGO_PORT:-5866}`，数据保存在独立 `highgo_data` 卷。此本地开发路线不代表生产许可或完整瀚高能力认证。

```bash
# 在 business/.env 中设置独立的 HIGHGO_PASSWORD 和 HIGHGO_APP_PASSWORD，端口默认 5866。
bash business/scripts/start.sh -highgo
bash business/scripts/highgo.sh status
bash business/scripts/stop.sh -highgo
# 如需重启，由用户执行：
bash business/scripts/restart.sh -highgo
```

三权管理员密码 `HIGHGO_PASSWORD` 仅在首次建卷时写入，已有卷不自动轮换管理员密码。平台接入使用普通账号 `HIGHGO_USER`（默认 `business`），密码独立配置为 `HIGHGO_APP_PASSWORD`。首次建卷和每次标准启动共用 `highgo/init/01-business.sh`，初始化普通账号、业务 schema 和样例；已有同名账号若带管理属性或角色成员关系则拒绝使用。首次创建通过 stdin 传入原生 `psql` 变量并使用标识符／字面量引用绑定，口令不放入进程参数或临时文件。已有业务密码能登录时不重复改密，配置密码变化时由 `syssso` 通过 stdin 的原生 `psql` 变量绑定轮换，失败诊断不输出密码语句，密码不进入进程参数或临时文件。`HIGHGO_APP_VALID_UNTIL` 留空时保留原生有效期；可经明确确认仅对业务账号设置 `infinity`，不改变全局安全策略。样例由业务账号持有，旧 SYSDBA 探针只调整属主，不删除业务数据。停止只停止容器，保留数据。已有同名容器归属不符、端口冲突或 SQL 健康检查失败时直接报错。

业务样例由普通账号在 `addp_business` 中持有：探针 `addp_engine_probe` 1 行、客户 `customers` 3 行、商品 `products` 3 行、订单 `orders` 3 行、订单明细 `order_items` 4 行。字段与 Kingbase 普通关系样例一致，编号使用 `HG-` 前缀；保留主键、业务唯一键、三条外键及订单客户／更新时间索引，订单合计与明细合计均为 `6795.50`。整个样例 SQL 在一个事务中执行；重复初始化按固定主键更新这些受管理样例，保留额外记录，不删除或重建表。样例仍由唯一 `highgo/init/01-business.sh` 执行，首次建卷与已有卷共用此路径。

`make test-business-config` 覆盖 profile 隔离、固定镜像、回环端口、独立数据卷及初始化回归，已纳入 `make test-platform` 和现有 Platform CI。确定性回归验证三行密码文件、失败清理、拒绝空值／换行口令、SM3/TLS HBA 及数据卷复用；不等同于原生数据库运行验收。用户启动后需核验版本、TLS、样例及业务密码登录。System 使用同一数据库身份登记；已有 Engine 只更新业务账号凭据。

2026-10-15 本机运行复核：标准入口已启动 `business-highgo`，状态 healthy，实际映射 `127.0.0.1:5866`。原生只读事务确认版本 `HighGo Database Management System 4.5 on aarch64,build on 20250227`、数据库 `highgo`、SM3、TLS 1.3 和一行样例；配置密码可登录三个管理角色。数据库保留默认口令有效期，本轮初始化的三权账号提示截止到 2026-10-22，不自动改为无限期。原生 Go 标准入口 `scripts/test/common-postgres-gate.sh --test highgo-write --highgo-native` 通过建表、COPY/INSERT、精确类型、Abort 和失败批次回滚，随机测试 schema 已清理且零残留。System 页面首轮连接被旧插件的中文版本字串检查误拒绝；共享插件已修复并通过 Common/System Go T1。用户重启全部模块后，System 表单和保存后的连接检测均成功，新建 Engine 29「Business HighGo」（`127.0.0.1:5866/highgo`，`sslmode=require`）；原远程 Engine 28 保持删除状态。Meta 基础扫描成功，Manager 可浏览 `addp_business` 和 `public`，并在数据项 44352 预览 `addp_business.addp_engine_probe` 的 1 行、2 字段，内容为 `1 / HighGo V4.5 Business`。此复核确认本地接入、基础目录及样例预览，不代表全量深度扫描或所有消费链路认证。

2026-10-15 审计对象预览回归：`SYSDBA` 对 `public.hg_t_audit_log` 的通用 SELECT ACL 虽返回 true，原生读取仍按三权分立拒绝；Manager 原失败发生在 `SELECT COUNT(*)`。另一个运行监控视图 `pg_stat_statements` 因未预加载而不可读。HighGo 插件复用现有系统对象过滤，排除 `hg_t_audit_log`、`pg_buffercache`、`pg_stat_statements`，不删除源对象、不扩大账号权限、不关闭审计。原生目录过滤／旧路径拒绝及写入门禁已通过，测试 schema 零残留；用户重启全部模块后，Engine 29 根节点的完整范围深度扫描成功，技术数据项数量由 4 收敛为 1；`public` 目录显示 0 项，三个系统对象已不在业务目录中。原生只读目录查询确认源数据库中的三个对象仍保留。Manager 对数据项 44352 的业务样例预览再次通过（1 行、2 字段）；旧审计表链接无法再定位误发现的数据项，页面回到待选择状态。运行复验完成。

2026-10-15 业务账号收敛：用户明确确认仅业务账号长期有效。已在现有卷创建普通账号 `business`，三权角色的权限和 2026-10-22 有效期保持原值；`addp_business` 与探针表均由 `business` 持有，探针仍为 1 行。独立业务密码保存在忽略的 Business `.env`，原生 SM3/TLS 登录、幂等重复初始化及密码轮换已实测；普通业务账号通过现有 HighGo 原生建表／COPY／INSERT／精确类型／回滚门禁，随机 schema 零残留。`make test-business-config` 通过新增账号初始化回归（共 11 个瀚高用例）及已有 Business 配置检查。工作区 `make test-changed` 因其他受影响 owner 缺少 T2 连接配置在 preflight 阻断，不计为通过。System 凭据更新过程中发现通用地址比较误判：JSONB 返回的键顺序和空格与新生成文本不同。System 已改为按地址对象内容比较，Go T1 与 `scripts/test/system-iam-postgres-gate.sh --package repository --test engine-address` 真实 JSONB 回归通过；改变实际端口仍被拒绝为未确认搬迁。新 PostgreSQL 用例由现有 repository T2 自动发现，不新增 CI 路线。用户通过标准入口重启后，容器已采用 `business` 账号健康检查并恢复 healthy。随后 Engine 29 的连接账号已实际保存为 `business`，原 ID、`highgo` 数据库及地址键不变；保存后的连接测试成功。Manager 对既有数据项 44352 的 `addp_business.addp_engine_probe` 预览通过，显示 1 行、2 字段及 `1 / HighGo V4.5 Business`。重启后再次以普通业务账号运行原生读写门禁通过，测试 schema 零残留；本机业务账号切换、容器健康检查和样例预览验收完成。

2026-10-15 管理员长期密码：按用户对本机测试环境的明确要求，三个管理账号分别使用自身身份轮换为同一组新的强密码，现有 `sysdba`、`syssso`、`syssao` 的 `rolvaliduntil` 均已核验为 `infinity`；`business` 继续长期有效。管理密码仅保存到忽略且权限为 `0600` 的 `business/.env` 中 `HIGHGO_PASSWORD`，不进入日志或仓库。操作期间临时将原生密码有效期策略设为 0，轮换完成后已恢复原来的 7 天默认值，三权分立、最高口令复杂度、身份鉴别、SM3 和 TLS 保留。新密码登录、使用新配置执行既有 Business 幂等初始化及业务探针读取均通过，无需为此立即重启；后续标准启动会将更新的管理员配置应用到容器。长期有效期保存在现有数据卷；新建卷仍遵循初始化默认策略。此前 10 月 22 日管理员到期提醒已解除。

2026-10-15 普通关系样例验收：沿用 Business 初始化入口补齐客户／商品／订单／订单明细四表，`business` 原生连续初始化两次后逐表内容指纹一致，行数分别为 3／3／3／4；探针仍为 1 行。三条外键、普通账号属主、逐单金额及订单／明细合计 `6795.50` 均核验通过。Manager 在 Engine 29 的 `addp_business` 执行完整范围深度扫描成功，目录由 1 项增加至 5 项；四张新表逐表预览通过，字段数分别为 6／7／7／5，中文、布尔、小数和时间正常显示。`make test-business-config` 通过（11 个瀚高用例及已有 Business 检查）。`make test-module MODULE=business` 已完整通过：平台检查、66 项原生运行时等生命周期回归、38 项 Infra 定向回归及独占 Redis T2 的 5 项断言均通过。Infra 测试夹具改用 Shell 模拟 Docker，命令序列保持一致，参数记录覆盖空值、回车、换行及中文；完整启动场景采用 30 秒有界等待。该组由唯一 `make test-infra-runtime-log-lifecycle` 子入口接入既有生命周期和 Platform CI，不修改生产重启逻辑。工作流审计使用经 PyPI SHA-256 校验的固定版本本地 wheel，通过原标准审计入口执行。此次样例沿用现有 SQL 与配置验证入口，无新增依赖、生命周期或 CI 路线。


2026-10-15 PostgreSQL → 瀚高本机同步验收：通过 Console 创建 Transfer 任务 13（`瀚高同步验收-PG订单-20261015_ac48d5`），从 Engine 2 的 `public.highgo_sync_orders_src_20261015_ac48d5` 同步到 Engine 29 的独立新表 `addp_business.orders_from_pg_20261015_ac48d5`。源表为三行合成订单，源字段经单表深度扫描取得；采用 bounded snapshot、append 和 7 个同名字段映射，普通账号 `business` 自动建表并写入。执行 `2895342d-b40a-4f3c-94aa-b9fc4aec0507` 成功，读取／写入均为 3，统一监控记录 573 显示成功。原生只读查询逐行比较全部七字段一致，金额列为 `numeric(14,2)`，两端合计均为 `6795.50`；原五张样例表行数仍为 1／3／3／3／4。自动扫描发现目标数据项 44575，Manager 预览显示三行七字段，订单号为 `PG-SYNC-2026-0001` 至 `0003`。保留任务和验收表供复查；append 再次执行会追加同一批记录，不应将其当作幂等刷新任务。

本次同时发现范围限制：初次选择 replace 的执行 `86e59858-424d-41cb-a6e6-59c6cad34f66` 在读写前失败，原因是当时 HighGo 尚未声明／实现 ResourceDeleteProvider；失败后原生确认目标表仍不存在，随后在同一任务改用已支持的 append 才成功。只读配置预检此前仍显示定义受支持，Transfer 执行详情也未展示已记录的错误 message，其中覆盖能力及配置预检已在后续修复；本段保留首次验收事实，执行失败诊断由共享观测能力的 Owner 维护。本记录只证明本机一次快照新表写入及预览，不计为隔离 T4、replace、upsert、增量或 CDC 认证。本轮未变更运行代码、API、依赖或 CI 路线；验收证据来自正式页面与两端原生只读查询，文档执行 `git diff --check -- business/README.md`，不重复执行上节已经通过的完整 Business 门禁。

2026-10-16 覆盖运行复验：用户重启共享代码后，Engine 29 连接检测成功且能力声明 `delete=true`。Console 创建独立合成任务 14，目标为 `addp_business.orders_replace_20261016_2f83c643`；只读配置预检返回受支持，正式执行 `de0fdd08-41d3-4fce-8458-592b826a5dc1` 成功，读取／写入各 3 行。原 1 行哨兵已被替换，七字段与 PostgreSQL 源逐值一致，金额合计 `6795.50`；Meta 自动发现数据项 44581，Manager 预览显示 3 行、7 字段。原任务 13 未重新执行，原追加表仍为 3 行。此记录只证明本机 Console／原生链路，不代替隔离 Hosted T4 或完整模块门禁。

2026-10-16 关系元数据运行复验：用户重启全部模块后，在 Console 对 Engine 29 执行连接检测成功，能力声明中的 `storage.facts.indexes`、`constraints`、`partitioning` 均为 `true`，覆盖所需 `storage.store.delete` 仍为 `true`。分别深度刷新 `customers`、`products`、`orders`、`order_items`，从 Manager 正式属性 JSON 取得的 11 个具名约束和 10 个索引与原生目录逐项一致，包括主键字段标记、订单 `updated_at, id` 索引顺序、明细 `order_id, product_id` 复合唯一键及三条外键的引用 schema／表／字段。四表预览分别为 3 行 6 字段、3 行 7 字段、3 行 7 字段、4 行 5 字段。本机样例均为普通表，不生成分区事实；RANGE／LIST／HASH 和子分区边界由已有原生 HighGo 标准测试入口覆盖，此处不计为分区表页面验收、隔离 Hosted T4 或完整 Transfer 模块门禁。

瀚高自动化链路使用 `make test-online ONLINE_SUITE=highgo-consumer-flow`，仅由手动 Hosted profile `scripts/test/online-hosted-highgo-gate.sh` 编排。物理夹具复用本节固定镜像和初始化，创建独立 PG 源与瀚高目标；核验四表关系事实、Console 预览及两次覆盖同步，退出删除当次平台、业务容器和匿名卷。`make test-highgo-online-runner` 覆盖确定性反例、隔离清理和登记一致性。首次真实 Hosted 结果另行记录，不以脚本实现完成代替运行通过。

## 快速开始

### 默认启动（PostgreSQL + MinIO）

```bash
# 1. 配置环境变量
cp .env.example .env
# 编辑 .env 修改密码

# 2. 启动服务
bash scripts/start.sh

# 3. 验证服务
docker-compose ps
```

### 按需启动特定服务

```bash
# 启动所有服务
bash scripts/start.sh -all

# 只启动 ClickHouse
bash scripts/start.sh -clickhouse

# 只启动 SuperMap SDX+ for PostgreSQL 专用实例
bash scripts/start.sh -supermap-postgresql

# 只启动 MongoDB
bash scripts/start.sh -mongodb

# 只启动 MySQL，并幂等初始化专用 CDC 用户
bash scripts/start.sh -mysql

# 只启动 OceanBase CE，并幂等初始化可查询样例
bash scripts/start.sh -oceanbase

# 只启动 TiDB 三组件，并幂等初始化可查询样例
bash scripts/start.sh -tidb

# 只启动 openGauss，并幂等初始化可查询样例
bash scripts/start.sh -opengauss

# 启动 License 受控的 disposable KingbaseES 样例；必须独立使用且不进入 start.sh -all
bash scripts/start.sh -kingbase

# 在 ARM64 Docker 中启动 DM8 官方介质技术夹具；必须独立使用且不进入 start.sh -all
bash scripts/start.sh -dameng

# 只启动 Oracle，并幂等初始化普通表与 Spatial 样例
bash scripts/start.sh -oracle

# 只启动业务 Redpanda，并幂等初始化只读 Engine 账号
bash scripts/start.sh -redpanda

# 启动 ClickHouse + MongoDB
bash scripts/start.sh -clickhouse -mongodb

# 启动 PostgreSQL + MinIO + ClickHouse
bash scripts/start.sh -postgres -minio -clickhouse
```

本地启动 PostgreSQL、MySQL、MinIO 时，脚本会检查首选宿主机端口；首次冲突时自动选择空闲端口，并把成功启动的实际映射写入忽略版本控制的 `.business-state/ports.env`。后续重启沿用该端口，避免已登记的 Engine Instance 身份漂移；固定端口再次被占用时启动会明确失败。容器内部端口保持不变。本机开发注册引擎时使用 `127.0.0.1:实际宿主机端口`，同一 Docker 网络中的服务使用 `business-*` 服务名与固定内部端口。

Business bridge 网络由标准启动脚本创建并保留，网段记录在 `.business-state/network.env`。OceanBase 单节点的旧数据依赖首次部署时的容器内网 IP；标准启动脚本只读其 OBD 配置，并在重建容器前恢复同一地址。地址冲突或网段丢失时会拒绝启动，不迁移或重建旧数据；其他引擎仍使用服务名，不依赖容器 IP。

OceanBase 的 `observer/run` 使用 tmpfs 保存 PID 与 socket；它们不进入持久数据卷，容器重建不会误用上次进程的 PID。

`-nfs` 使用 macOS 系统 NFS。已有正确导出且服务正在运行时可直接复用；首次配置仍需使用 `sudo bash scripts/start.sh -nfs`。

## 目录结构

```
business/
├── docker-compose.yml              # Docker Compose 配置
├── .env / .env.example             # 环境变量
├── README.md                       # 本文档
│
├── scripts/                        # 管理脚本
│   ├── start.sh                    # 启动服务
│   ├── stop.sh                     # 停止服务
│   ├── restart.sh                  # 重启服务
│   ├── kingbase.sh                 # owner-managed KingbaseES disposable 样例
│   ├── dameng.sh                   # DM8 ARM64 官方介质 disposable 技术夹具
│   ├── online-engine-fixture.sh    # T4 专用 PostgreSQL Fixture 生命周期
│   ├── online-workbench-mysql-fixture.sh # Workbench T4 专用只读 MySQL Fixture
│   ├── online-tidb-consumer-fixture.sh # TiDB 消费链路 T4 专用三组件 Fixture
│   ├── online-kingbase-consumer-fixture.sh # KingbaseES 消费链路 T4 专用许可受控 Fixture
│   ├── online-manager-minio-fixture.sh # Manager 血缘与文档快显 T4 专用 MinIO Fixture
│   ├── online-raster-minio-fixture.py # 栅格 T4 隔离 MinIO、发布后清理故障代理、原生像元与 COG 校验
│   └── online-security-transfer-fixture.sh # Security/Transfer T4 复合 Fixture
│
├── postgres/                       # PostgreSQL 配置
│   ├── init.sql                    # 数据库初始化脚本
│   └── pg_hba.conf                 # 访问控制配置
│
├── clickhouse/                     # ClickHouse 配置
│   └── init.sh                     # ClickHouse 初始化脚本
│
├── mongodb/                        # MongoDB 配置
│   └── init.sh                     # MongoDB 初始化脚本
│
├── mysql/                          # MySQL 配置与测试数据
│   ├── init-cdc.sh                 # 专用 CDC 用户幂等初始化
│   └── test-data.sh                # 普通表与全二维几何族显式测试数据
├── oceanbase/                      # OceanBase CE 测试数据
│   └── init.sql                    # 幂等创建探针与普通关系业务样例表
├── tidb/                           # TiDB 测试数据
│   └── init.sql                    # 幂等创建探针与普通关系业务样例表
├── opengauss/                      # openGauss 测试数据
│   └── init.sql                    # 幂等创建探针与普通关系业务样例表
├── kingbase/                       # KingbaseES 测试数据
│   └── init.sql                    # 幂等创建探针与普通关系业务样例表
├── dameng/                         # DM8 ARM64 官方介质技术夹具
│   ├── Containerfile               # 固定 ARM64 基础镜像与静默安装
│   ├── install.xml                 # 静默安装与初始化参数
│   ├── startup.sh                  # 前台启动 dmserver
│   └── init.sql                    # 幂等探针与普通关系样例
├── oracle/                         # Oracle 普通表与 Spatial 测试数据
│   ├── init.sql                     # 幂等初始化 SQL
│   ├── init-cdc.sh                  # ARCHIVELOG、LogMiner 账号与权限幂等初始化
│   └── test-data.sh                 # 容器内执行初始化并验证
│
├── doris/                          # Apache Doris 配置
│   └── init.sh                     # Doris 集群初始化
│
├── spark/                          # Apache Spark 配置
│   ├── spark-defaults.conf         # Spark 默认配置
│   └── init-test-data.sh           # Spark 测试数据初始化
│
└── minio/                          # MinIO 配置（预留）
```

## 架构说明

### 与 ADDP 系统库的区别

| 组件 | ADDP 系统库 | Business 业务库 |
|------|------------|----------------|
| PostgreSQL | 端口 5432 | 端口 5433 |
| SuperMap SDX+ for PostgreSQL | - | 独立实例，端口 5434 |
| MinIO | 端口 9000-9001 | 端口 9002-9003 |
| ClickHouse | - | 端口 9000, 8123 |
| MongoDB | - | 端口 27017 |
| TiDB | - | 端口 4000 |
| openGauss | - | 端口 5435 |
| KingbaseES | - | 端口 5436；仅 owner-managed disposable 样例 |
| 达梦 DM8 | - | 端口 5236；ARM64 官方介质 disposable 数据库 |
| 用途 | ADDP 元数据（用户、资源配置、任务定义） | 用户业务数据（上传的数据、文件） |
| 示例数据 | 用户账号、资源配置表 | Shapefile 空间数据表、用户上传文件 |

### CPU 架构自适应

启动脚本自动检测架构并选择最优镜像：

| CPU 架构 | PostgreSQL 镜像 | 性能 |
|---------|----------------|------|
| **ARM64** (Apple Silicon) | `imresamu/postgis-arm64:15-3.4` | ⚡ 原生性能 |
| **AMD64** (Intel/AMD) | `postgis/postgis:15-3.4` | ⚡ 原生性能 |

openGauss 不使用 Docker Hub 第三方镜像。`scripts/lib/opengauss-official-media.sh` 固定维护 6.0.6 官方 x86_64/aarch64 tar URL 与 SHA-256；当前 Business 和 T2 只使用已通过发布认证的 x86_64 介质。6.0.6 官方 Docker 构建包含 MOT，MOT 需要有效 NUMA 拓扑；Docker Desktop for macOS 不提供该拓扑，官方 ARM64 与 x86_64 镜像都会在启动期失败。因此 macOS 本地巡检不启动 openGauss，Provider 实库门禁由 GitHub Actions 的 Linux x86_64 Job 承担，不能用跳过或第三方镜像伪装本地通过。

KingbaseES 不使用第三方镜像，也不把官方介质或 License 写入仓库、Compose、日志或 Artifact。`scripts/lib/kingbase-official-media.sh` 固定 V9R1C10 `V009R001C010B0004` 官方 x86_64 与 ARM64 tar、官方 MD5 和 ADDP 校验的 SHA-256，并按宿主 CPU 选择后统一加载为固定中性本地镜像；`business/docker-compose.yml` 只声明一个独立 `kingbase` profile 和无卷容器结构。`business/scripts/kingbase.sh` 要求仓库外 `ADDP_KINGBASE_LICENSE_FILE` 及其 `ADDP_KINGBASE_LICENSE_SHA256`，按 `docker compose create`、License 注入、`docker compose start` 的唯一顺序启动，容器因此显示在 Docker Desktop 的 `business` Compose 分组中。macOS ARM64 可使用官方 ARM64 介质做本地功能评估，但内置 90 天试用 License 不作为可重复门禁路线；正式 T5/T2/T4 仍只在 owner-managed Linux x86_64 Runner 使用正规 License，GitHub Hosted 与 macOS Docker Desktop 不承担正式验证。

达梦 DM8 只使用 `dm8_20260708_HWarm920_kylin10_sp1_64.zip`，外层 ZIP SHA-256 为 `d6871147cd4a04e1595d9dedf9d05245c55b17bff2ae738dded37fe568df9824`，内层 ISO SHA-256 为 `2a8a4844527e901718a88b4c747d7fd44460bcb2a862956bba98146760b8423e`，同介质官方 Go 驱动 ZIP SHA-256 为 `96516ecd7c9ec3c405109b81135f2c9d87ed0631632e99cd2de0e06f03b1392e`。`bash scripts/start.sh -dameng` 仅接受 Docker Server `linux/arm64`，在本机从官方介质构建 `addp/dameng:dm8-20260708-arm64` 并启动无卷 `dameng` profile；不会上传、归档或重分发介质、驱动与镜像。该介质面向鲲鹏 920/麒麟 10 SP1，本机 Ubuntu ARM64 容器只属于非认证 ABI 技术验证；内置试用授权固定于 2027-07-07 到期，不能作为长期门禁或通过重建重置。System 可在 macOS 开发模式登记 `engine_type=dameng`，但宿主进程不执行连接测试；真实 Provider T2/T4 从运行中的 Business 镜像提取并校验官方驱动后，在 Linux ARM64 Docker 内执行。

## 脚本说明

### scripts/start.sh - 启动服务

```bash
bash scripts/start.sh
```

**功能**：检查配置、检测架构、启动服务、验证健康状态、安装 PostGIS
**特性**：幂等执行（可重复运行）

`-supermap-postgresql` 启动的 `business-supermap-postgresql` 使用独立 volume 和原生 PostgreSQL 15 镜像。启动脚本会拒绝已安装 PostGIS 的实例；SuperMap `sm*` 系统表只能在 System 中通过 `SuperMap SDX+ for PostgreSQL` 高危启用入口，由 `supermap_workflow` 的 SDK 算子创建。

启用 MySQL 时，脚本还会在数据库 ready 后执行 `mysql/init-cdc.sh`。该脚本每次都创建或更新 `${MYSQL_CDC_USER:-addp_cdc}@%`，并将权限收敛为 Debezium 所需的最小权限集，因此已有数据卷也会生效。连接 MySQL CDC Engine 时使用 `.env` 中的 `MYSQL_CDC_USER` 和 `MYSQL_CDC_PASSWORD`，不要使用 root。

`bash scripts/start.sh -oceanbase` 启动官方 OceanBase CE 固定 LTS 镜像，并通过纯 SQL 脚本幂等初始化 `${OCEANBASE_DATABASE:-business}`。样例包含连接探针表，以及与 MySQL 普通业务样例对齐的 `customers`、`products`、`orders`、`order_items` 四张关联表，覆盖主外键、唯一约束、索引、Decimal、JSON、布尔和微秒时间字段；当前支持非空间 InnoDB 基表的 bounded watermark 一致性读取、安全建表、可空列增量演进、事务性分批 insert、覆盖策略所需的精确目标表删除，以及按显式非空稳定键执行的事务性幂等 upsert。该容器不初始化或声明空间、CDC 或 Oracle 模式能力，是 `MODE=mini` 的单机测试形态，建议预留至少 2 CPU / 8 GB 内存，不用于生产部署。System 中必须注册为 `engine_type=oceanbase`，容器内连接地址为 `business-oceanbase:2881`，默认账号为 `root@test`；不要登记为 MySQL。

OceanBase Provider 的唯一 T2 入口是仓库根 `make test-common-oceanbase`。对当前 Business 容器验证时，显式传入 `ADDP_TEST_OCEANBASE_HOST`、`ADDP_TEST_OCEANBASE_PORT`、`ADDP_TEST_OCEANBASE_USER` 和 `ADDP_TEST_OCEANBASE_PASSWORD`；门禁固定使用名称含 `disposable` 的专用 database，覆盖连接、实时目录、字段与统计 Facts、BatchRead、可执行查询样例、命名参数、受控只读事务，以及非空间普通表的 bounded watermark resume 和 prepare/session/delete/upsert 写入契约。测试生命周期创建并删除该 database，各用例只清理自己拥有的 gate 表，不读取或修改上述固定业务样例。

`bash scripts/start.sh -tidb` 启动 PingCAP 官方 TiDB 8.5.8 三组件，并幂等初始化 `${TIDB_DATABASE:-business}`。样例包含连接探针及 `customers`、`orders` 普通关系表；System 中必须注册为 `engine_type=tidb`，宿主机连接 `localhost:${TIDB_PORT:-4000}`，容器网络连接 `business-tidb:4000`，默认账号 `root`、空密码。镜像在 Compose 中直接固定版本和 digest，不接受环境变量覆盖。首版只开放经真实 T2 验证的非空间目录、查询、bounded watermark 和普通表写入能力，不声明 MySQL replication、TiCDC、空间或分区变化应用。

TiDB Provider 的唯一 T2 入口是仓库根 `make test-common-tidb`。门禁拥有独立的无卷三组件 Compose project，创建并删除名称含 `disposable` 的 database，覆盖真实目录/Facts、参数查询、BatchRead、Snapshot Isolation 下的 bounded watermark resume，以及 prepare/session/delete/upsert；成功、失败和中断均执行 `down --volumes --remove-orphans` 并验证容器零残留。同一 owner 脚本可在 Linux x86_64 GitHub Hosted 和 macOS Docker Desktop 执行。

`bash scripts/start.sh -opengauss` 启动官方 openGauss 6.0.6 LTS 介质并幂等初始化 `${OPENGAUSS_DATABASE:-business}`。样例与 MySQL/OceanBase 的普通业务域一致，包含 `customers`、`products`、`orders`、`order_items` 和连接探针，覆盖主外键、唯一约束、复合索引、Decimal、Boolean 与时间字段。System 中必须注册为 `engine_type=opengauss`；宿主机连接 `localhost:${OPENGAUSS_PORT:-5435}`，容器网络连接 `business-opengauss:5432`，固定账号 `gaussdb`。不得登记为 PostgreSQL，也不得据 PG 兼容性宣称 PostGIS、CDC 或 PostgreSQL 扩展能力。

`bash scripts/start.sh -kingbase` 在 owner-managed Linux x86_64 主机启动 `kingbase` Compose profile 下的无卷 KingbaseES PG 模式容器，并反复执行 `business/kingbase/init.sql` 收敛探针、`customers`、`products`、`orders` 与 `order_items` 样例。调用前必须从 owner-only 当前 shell 注入 `KINGBASE_PASSWORD`、`ADDP_KINGBASE_LICENSE_FILE` 与 `ADDP_KINGBASE_LICENSE_SHA256`；可选覆盖 `KINGBASE_DATABASE`、`KINGBASE_USER` 与 `KINGBASE_PORT`。该选项必须独立使用，且不属于 `-all`。System 中必须注册为 `engine_type=kingbase`，宿主机连接 `127.0.0.1:${KINGBASE_PORT:-5436}`。停止和重启分别使用 `bash scripts/stop.sh -kingbase`、`bash scripts/restart.sh -kingbase`；状态检查使用 `bash scripts/kingbase.sh status`。停止会删除本轮容器并验证零残留。

`bash scripts/start.sh -dameng` 在 ARM64 Docker Server 上构建并启动 DM8 官方介质数据库，反复执行 `business/dameng/init.sql` 收敛 `ADDP_ENGINE_PROBE` 与 `ADDP_RELATIONAL_SAMPLE`。宿主机连接为 `127.0.0.1:${DAMENG_PORT:-5236}`，固定 disposable 测试用户由脚本创建；System 以 `engine_type=dameng` 登记该端点。停止使用 `bash scripts/stop.sh -dameng`，状态检查使用 `bash scripts/dameng.sh status`；停止会删除本轮容器并验证零残留，本地镜像与官方介质缓存由显式清理或 T5 owner 门禁回收。

### scripts/online-workbench-mysql-fixture.sh - Workbench T4 MySQL Fixture

该入口只允许 `ADDP_ONLINE_HOST=1` 的 macOS 专用 Runner 使用，且只接受仓库外 `ADDP_ONLINE_WORKBENCH_MYSQL_*` 环境变量。它不读取或生成 `business/.env`，只操作由 `business/mysql` Compose service 拥有的 `business-mysql`，重建确定性测试数据，并将预置 Engine Instance 使用的账号收敛为仅有 `SELECT` 权限。个人开发环境不得调用该脚本。

```bash
bash scripts/online-workbench-mysql-fixture.sh start
bash scripts/online-workbench-mysql-fixture.sh status
bash scripts/online-workbench-mysql-fixture.sh stop
```

### scripts/online-oceanbase-consumer-fixture.sh - OceanBase 消费链路 T4 Fixture

该入口只允许 `ADDP_ONLINE_HOST=1` 的 macOS 专用 Runner 使用，且只接受仓库外 `ADDP_ONLINE_OCEANBASE_*` 环境变量。它固定使用 `oceanbase/oceanbase-ce:4.4.2-lts` 和 `business/oceanbase` Compose service，不读取或生成 `business/.env`。`start` 将 `addp_online_consumer_source` 恢复为 5 行 watermark 基线并将同构目标表清空；`advance` 只更新 1 行并新增 1 行；`stop` 在移除容器前再次恢复基线，因此成功、失败和中断后的物理数据边界一致。永久 Engine Instance 必须以 `engine_type=oceanbase` 指向同一端点，Fixture 不创建、修改或删除 Engine Instance；正式 T4 通过 Meta 定位目标后，分别由 Manager、Develop 和 Service 读取 Transfer 结果并比较一致性。个人开发环境不得调用该脚本。

```bash
bash scripts/online-oceanbase-consumer-fixture.sh start
bash scripts/online-oceanbase-consumer-fixture.sh advance
bash scripts/online-oceanbase-consumer-fixture.sh status
bash scripts/online-oceanbase-consumer-fixture.sh stop
```

### scripts/online-tidb-consumer-fixture.sh - TiDB 消费链路 T4 Fixture

该入口只允许 `ADDP_ONLINE_HOST=1` 的 macOS 专用 Runner 使用，只接受仓库外 `ADDP_ONLINE_TIDB_*` 环境变量，并复用 `scripts/test/docker-compose.tidb-t2.yml` 中固定 digest、无数据卷的 PD/TiKV/TiDB 三组件拓扑。`start` 重建 5 行 watermark 源表和同构空目标表，`advance` 更新 1 行并新增 1 行，`stop` 执行 `down --volumes --remove-orphans` 并验证本次 Compose project 容器零残留。永久 Engine Instance 必须以 `engine_type=tidb` 指向 `localhost:${ADDP_ONLINE_TIDB_PORT}`；Fixture 不创建、修改或删除 Engine Instance。个人开发环境不得调用该脚本。

```bash
bash scripts/online-tidb-consumer-fixture.sh start
bash scripts/online-tidb-consumer-fixture.sh advance
bash scripts/online-tidb-consumer-fixture.sh status
bash scripts/online-tidb-consumer-fixture.sh stop
```

### scripts/online-manager-minio-fixture.sh - Manager 内部产物血缘 T4 Fixture

该入口要求 `ADDP_ONLINE_HOST=1`：Manager 内部产物套件由 GitHub Hosted Linux x86_64 运行，混合检索套件仍由 macOS 专用 Runner 运行。它通过 `business/minio` Compose service 管理独立 Business MinIO，用 Python 标准库在临时目录确定性生成 LAS 1.2 三点 EPSG:3857 夹具，并发布仓库已跟踪的 3 页 PPTX；Hosted 套件无需本机 NFS 样例或永久 Engine Instance，退出时销毁业务容器、网络和数据卷。只有混合检索套件额外发布专用部署中的 JPG。模型夹具由脚本在操作系统临时目录用 Python 标准库确定性生成：`model3d/dae/model.dae` 和 `model3d/3ds/model.3ds` 各包含一个带 UV 的静态三角形，并引用同目录的 `texture.png`；两者不引入外部下载。IFC 与单体 OSGB 使用 `business/fixtures/manager/addp_online_model_fixture.*` 自有夹具：IFC 来源于 Model3D 原生预检的 IFC4 米制立方体；OSGB 由现有 `docker/converter/tests/osgb_compression_smoke.cpp` 的 `generate ... dxt5` 生成，保留 zlib 压缩、8 × 8 红色 DXT5 贴图和带 UV 的三角形，不使用第三方数据或开发容器。SKP 自有夹具 `addp_online_model_fixture.skp` 由固定 OpenSKP 1.3.0 的 `create()` 生成：同一组件中的三角形坐标为 `(0,0,0)`、`(1,0,0)`、`(0,2,0)`（英寸），正背面使用同一内嵌 4 × 4 彩色 PNG；放置两个组件实例，第二个沿 X 位移 2 英寸。生成的 GLB 共享一个网格，场景米制尺寸为 `[0.0762, 0, 0.0508]`，贴图 SHA-256 为 `3168c3aa8cd3338d29b9ca74ee6794a2290b69d4da04d0869788187cb6ac4cf3`。夹具直接随仓库复制到 `model3d/skp/model.skp`，Business 不安装解析器或生成解析元数据。MAX 夹具 `addp_online_model_fixture.max` 是未经修改的 0 A.D. 项目贡献者马模型，由 Stanislas Dolcini（StanleySweet）分享，许可为 [CC-BY-SA-3.0](https://creativecommons.org/licenses/by-sa/3.0/legalcode)，来源为 [io_scene_max issue 1](https://github.com/nrgsille76/io_scene_max/issues/1) 和其中的 [horses.zip](https://github.com/user-attachments/files/15525512/horses.zip)。模型 SHA-256 为 `916714595bf7a2953264b8afaafe339fa60bdb4944b4664f6802ccb288190653`；不声明动画或骨架保真。ADDP 自建红色 4 × 4 测试 PNG `addp_online_max_texture.png` 并非原始 0 A.D. 贴图，复制为同目录 `max/texture.png`，由验收显式声明源 bitmap 引用映射。DAE 使用厘米单位，生成后的两类模型均为 1 × 2 的三角形。模型 object 路径固定为该专用夹具的协议，不增加部署配置。脚本只接受仓库外 `ADDP_ONLINE_MANAGER_MINIO_*` 变量，不读取或生成 `business/.env`，也不接触 Manager infra MinIO。个人开发环境不得调用该脚本。

```bash
bash scripts/online-manager-minio-fixture.sh start
bash scripts/online-manager-minio-fixture.sh status
bash scripts/online-manager-minio-fixture.sh stop
```

### scripts/online-security-transfer-fixture.sh - Security/Transfer T4 Fixture

该入口只允许 `ADDP_ONLINE_HOST=1` 的 macOS 专用 Runner 使用。它复用 PostgreSQL Online Fixture，并管理独立的 `business-mongodb`：幂等建立 3 条合成文档和只读 MongoDB 账号，同时准备固定 PostgreSQL 目标表。平台长期预置的 MongoDB Engine Instance 只能使用只读账号；root 凭据仅由 Fixture 生命周期使用。脚本不读取或生成 `business/.env`，个人开发环境不得调用。

```bash
bash scripts/online-security-transfer-fixture.sh start
bash scripts/online-security-transfer-fixture.sh status
bash scripts/online-security-transfer-fixture.sh stop
```

### mysql/test-data.sh - MySQL Spatial 测试数据

```bash
bash scripts/start.sh -mysql
bash mysql/test-data.sh
```

该脚本幂等重建普通业务表，以及 `POINT`、`LINESTRING`、`POLYGON`、`MULTIPOINT`、`MULTILINESTRING`、`MULTIPOLYGON`、`GEOMETRYCOLLECTION`、通用 `GEOMETRY`、多几何列和 3857 样例，并校验几何有效性与空间索引。测试数据只允许显式执行，不挂接 `scripts/start.sh -mysql`，避免启动业务数据库时破坏已有数据。

### Oracle Spatial 测试源

Oracle 必须使用常规镜像 `gvenzl/oracle-free:23`。官方 `-slim` 镜像会卸载 Oracle Spatial 和 Oracle Locator，不能通过用户授权或普通初始化 SQL 恢复。

`bash scripts/start.sh -oracle` 会幂等初始化普通表、`CUSTOMER_LOCATIONS` 点要素表和 `SPATIAL_FEATURES` 综合空间表，并验证：

- `MDSYS.SDO_GEOMETRY` 可用；
- `Point`、`LineString`、`Polygon`、`MultiPoint`、`MultiLineString`、`MultiPolygon` 和 `GeometryCollection` 均可转换为标准 WKB；三维 `SDO_GEOMETRY` 由 Oracle `TO_GEOJSON` 保留 Z 后再由 Transfer 归一化为 EWKB；
- `USER_SDO_GEOM_METADATA` 中存在 SRID 4326 元数据；
- 两张空间表均具有 Oracle Domain Spatial Index。

已有 `business_oracle_data` 如果由 `-slim` 镜像创建，推荐重建该 Business Oracle volume，避免在已裁剪的数据库字典上手工补装组件。该操作会删除现有 Business Oracle 数据，执行前必须确认其中没有需保留的数据：

```bash
docker compose -f business/docker-compose.yml stop oracle
docker compose -f business/docker-compose.yml rm -f oracle
docker volume rm business_oracle_data
cd business && bash scripts/start.sh -oracle
```

### Oracle CDC 测试源

`bash scripts/start.sh -oracle` 会在数据库 ready 后执行 `oracle/init-cdc.sh`，幂等启用 `ARCHIVELOG`、force logging、minimal supplemental logging，并创建 `${ORACLE_CDC_USER:-C##ADDP_CDC}` LogMiner 专用 common user。`CUSTOMERS`、`CUSTOMER_LOCATIONS` 和 `SPATIAL_FEATURES` 由 `init.sql` 幂等启用 `SUPPLEMENTAL LOG DATA (ALL) COLUMNS`，分别作为普通字段、单一 Point 和混合二维几何族 CDC 样例。Oracle Engine 使用 schema owner 的 business 主账号做 Catalog/读取，并供 Transfer 创建 generation-owned Spatial WKB/GeoJSON 镜像表、行级触发器和 DDL guard；XYZ 场景使用 GeoJSON CLOB 保留 Z，Transfer 最终仍输出 EWKB。System 的 `connection_info` 另存 `cdc_database_name`、`cdc_user` 和加密的 `cdc_password`，LogMiner 不复用业务账号或 SYS。

启用 Redpanda 时，脚本会创建或轮换 `${BUSINESS_KAFKA_READER_USERNAME:-addp_transfer}` 的 SCRAM-SHA-256 密码，并只授予读取 Topic、消费组和描述集群所需权限。System 中统一注册为 `engine_type=kafka`，连接 `localhost:${BUSINESS_KAFKA_PORT:-29092}`；不要注册 `addp-redpanda` 的 Infra Kafka 地址。

### scripts/stop.sh - 停止服务

```bash
bash scripts/stop.sh
```

停止所有业务库服务。

### scripts/restart.sh - 重启服务

```bash
bash scripts/restart.sh
```

检测架构、清理旧镜像、重启服务（幂等）。

### spark/init-test-data.sh - Spark 测试数据

```bash
bash spark/init-test-data.sh
```

幂等重建并真实查询验证 `default.addp_sample_orders`。Spark Master 启动时会自动执行该脚本，因此 Develop 查询工作台可以从 Spark 实时 Catalog 动态生成可执行样例。

## 常用操作

### 查看日志

```bash
docker-compose logs -f postgres    # PostgreSQL 日志
docker-compose logs -f supermap-postgresql # SuperMap SDX+ for PostgreSQL 专用实例日志
docker-compose logs -f minio       # MinIO 日志
docker-compose logs -f clickhouse  # ClickHouse 日志
docker-compose logs -f mongodb     # MongoDB 日志
docker-compose logs -f oceanbase   # OceanBase CE 日志
docker-compose logs -f tidb        # TiDB SQL 节点日志
docker-compose logs -f business-redpanda # Business Redpanda 日志
docker-compose logs -f doris-fe    # Doris 日志
docker-compose logs -f spark-master  # Spark 日志
```

### 数据备份

```bash
# 备份
docker exec business-postgres pg_dump -U business business > backup.sql

# 恢复
docker exec -i business-postgres psql -U business business < backup.sql
```

### PostGIS 验证

```bash
docker exec business-postgres psql -U business -d business -c "SELECT PostGIS_Version();"
```

## 生产部署

### 1. 部署到服务器

```bash
# 复制文件
scp -r business/ user@server:/opt/addp-business/

# 登录并启动
ssh user@server
cd /opt/addp-business
cp .env.example .env
vim .env  # ⚠️ 修改密码（必须！）
bash scripts/start.sh
```

### 2. 安全加固（必须！）

1. **修改默认密码**：使用 `openssl rand -base64 32` 生成强密码
2. **限制网络访问**：仅允许 ADDP 系统访问
3. **定期备份**：配置 crontab 自动备份
4. **监控磁盘**：定期检查数据卷大小
5. **更新镜像**：定期运行 `docker-compose pull && ./restart.sh`

## 故障排查

### 端口冲突

```bash
lsof -nP -i :5433           # 查看占用
# 修改 .env 中的 POSTGRES_PORT
```

### 架构不匹配

```bash
bash scripts/restart.sh  # 自动检测并使用正确架构
```

### 数据恢复

```bash
bash scripts/stop.sh
docker volume rm business_postgres_data
bash scripts/start.sh
docker exec -i business-postgres psql -U business business < backup.sql
```

## 技术细节

- **PostgreSQL**: 15.x + PostGIS 3.4.x
- **MinIO**: `RELEASE.2025-10-15T17-29-55Z`（由 `scripts/infra/Dockerfile.minio` 从固定官方源码修订构建，包含 `mc`）
- **ClickHouse**: 23.8
- **MongoDB**: 7.0
- **OceanBase CE**: 4.4.2 LTS (MySQL mode)
- **Apache Doris**: 2.1.0 (all-in-one)
- **Apache Spark**: 3.5.0
- **网络**: business-network (bridge)
- **持久化**: Docker volumes
- **架构**: ARM64, AMD64

## 常见问题

**Q: 业务库和系统库有什么区别？**  
A: 系统库存储 ADDP 平台元数据，业务库存储用户实际业务数据。

**Q: 为什么需要 PostGIS？**  
A: 支持 Shapefile、GeoJSON 等空间数据的存储和查询。

**Q: 可以替换为云服务吗？**  
A: 可以！支持 AWS RDS/S3、阿里云 RDS/OSS 等云服务。

**Q: 脚本可以重复执行吗？**  
A: 可以！所有脚本都是幂等的。

## Elasticsearch

通过 `bash business/scripts/start.sh -elasticsearch` 按需启动官方 Elasticsearch 9.5.4 单节点，支持 ARM64 和 AMD64。停止和重启使用同目录 `stop.sh -elasticsearch`、`restart.sh -elasticsearch`，数据卷保留。镜像固定 manifest digest，内存上限 2 GiB。宿主机只开放回环 HTTP，启用 Basic 认证；远程环境使用 HTTPS，插件允许提供 PEM CA，验证服务器证书。

配置位于 `business/.env` 的 `ELASTICSEARCH_PORT`、`ELASTICSEARCH_PASSWORD`、`ELASTICSEARCH_READER_USER`、`ELASTICSEARCH_READER_PASSWORD`；实际端口查看 `business/.business-state/ports.env`。初始化使用管理员，仅创建/更新固定 ID 的样例记录，不删除已有索引；创建 `addp_orders.v1` 和空索引 `addp_empty.v1`，内容包括对象、nested、multi-field、数组、null、缺失字段和超出安全整数范围的 long。注册 ADDP 引擎时使用只读用户，该角色仅有 `addp_*` 的 `read` 与 `view_index_metadata`，以及 Spark 官方连接器版本与健康检查需要的 `cluster:monitor/main`、`cluster:monitor/health` 两项集群动作；没有写入、节点发现及集群配置权限。

本机标准 `scripts/utils/register-business.sh` 在 ES 容器运行时会读取实际端口并注册只读接入。System 新增 `elasticsearch` 引擎填写 `endpoint`、`user`、`password`，按需填写 `tls_ca_cert`。目录仅展示普通、打开、非隐藏的具体索引；Meta 获取 Mapping；Manager 读取文档；Develop 在选定索引后使用 `es_dsl`。首版不提供聚合、脚本、别名、数据流、跨索引查询或 Transfer 写入/同步。

Spark Workflow 的 `load` 通过具体索引 locator 读取，Spark 计算集群独立选择。公开 `array_fields=["tags"]` 声明普通数组字段，nested 自动映射为对象数组；源连接与索引名在执行时派生。首版固定官方 ES Spark 9.5.4 连接器，仅 HTTP Basic 开发实验接入，HTTPS 明确拒绝；不支持别名、data stream、隐藏索引、DSL、写回、流式或全 DAG 统一 PIT。JSON 摘要中超出 JavaScript 安全整数范围的 bigint 使用十进制字符串，计算内部保留原类型。

验证入口：`make test-business-config`、`make test-common-elasticsearch-unit`、`make test-common-elasticsearch`。后者创建独占容器、认证用户及样例索引，验证 Common、Manager、Meta 及真实 Spark Worker 的多分片读取、空索引、数组声明和大整数后删除容器、卷和网络，不使用现有 Business 实例。

正式 T4 使用 `make test-online ONLINE_SUITE=elasticsearch-consumer-flow`，由 `online-hosted-elasticsearch-gate.sh` 在 Hosted Ubuntu 临时部署运行；物理夹具复用官方镜像、只读账号与 25 条文档/空索引初始化。System 独立身份经正式 API 登记引擎，普通用户从 Console 完成 Meta 重扫、Manager 预览和 Develop DSL 查询，并查看通过正式 Develop API 提交的 ES → Spark 工作流结果；该场景通过根产品构建入口生成 Spark Runtime 镜像，核对实际 image ID 与 `python api_server.py` 默认入口，使用独立 Master/Worker，核对四个节点、八次 Runtime 状态查询及实际 Executor 任务，归档同一身份的报告及五张截图。所有退出路径销毁临时业务容器、Infra 和凭据，核对零残留；仅人工触发，不登记 schedule。

产品容器默认入口已于 2026-10-10（北京时间）复验通过：[ES T4 37954898673](https://github.com/pampa0629/addp/actions/runs/37954898673)，提交 `ddaa982600608c94db56e04ac5ed130765cd77c1`。归档报告确认同一租户与普通用户完成 Meta 页面重扫、Manager 25 条文档/空索引预览、Develop 25 条 DSL 查询及正式 ES → Spark 四节点工作流，保留五张 Console 截图。工作流读取 25 行、金额合计 562.5，大整数 `9007199254740993` 在摘要中保持十进制字符串；实际 Worker 完成 14 个任务，连续八次 Runtime 状态查询一致。产品镜像实际 image ID 与默认入口核验通过，最终 `result=passed`、`cleanup=passed`、`infra_cleanup=zero_residuals`。该记录只证明对应提交的 HTTP Basic 只读消费范围，不包含 Spark HTTPS 接入，也不替代后续变更的重新验收。

## Business Redis

`bash business/scripts/start.sh -redis` 按需启动 Redis 7.2.13 单机；停止和重启使用 `stop.sh -redis`、`restart.sh -redis`，保留独立 `redis_data` 卷。镜像统一使用 Docker 官方发布的 ECR Public 多架构镜像并固定 OCI digest，支持 AMD64 和 ARM64，不复用 Infra Redis 容器、账号或数据。Business 首选回环端口为 `6380`，首次启动解析空闲端口，后续沿用 `business/.business-state/ports.env` 中的实际映射。

配置项为 `BUSINESS_REDIS_PORT`、`BUSINESS_REDIS_ADMIN_PASSWORD`、`BUSINESS_REDIS_READER_PASSWORD`，避免与 Infra `REDIS_*` 混淆。关闭匿名 default 用户，管理员为 `addp_business_admin`，只读用户为 `addp_business_reader`；角色使用显式命令白名单。该实例只开放 DB 0，读取账号可读取实例内全部业务 key，但不能执行写入、脚本、CONFIG、ACL 或 FLUSHALL。ACL 文件在容器 tmpfs 中由密码哈希生成；数据使用 AOF 持久化。

启动后仅补齐缺失的 `addp:sample:*` 固定样例，包含 string、大整数计数、hash、list、set、zset、stream、TTL 和带非 UTF-8 字节的 key/value。重复初始化不重复追加 list/stream、不重置仍存在的 TTL、不删除或覆盖既有 key；样例出现不符的原生类型时先整体拒绝初始化。初始化脚本只能由管理员执行，不属于读取账号的查询能力。

已实现独立 Business 部署、原生数据夹具、Redis Engine Plugin、System 连接注册、Meta 键值数据集身份扫描与 Manager 原生有限样本预览。System 通用表单按 ConnectionSpec 配置 ACL 用户、数据库编号与 TLS，注册脚本读取真实容器映射并使用只读账号登记数据库 0；连接检测执行 SELECT 和 DBSIZE，不读写业务 key。Catalog 为 `server -> keyspace`，Meta 只登记一个 `data_type=key_value` 数据集，内容键使用 `k:` 加无填充 Base64URL 令牌，原生类型、长度和 TTL 实时读取，不能伪装为 document collection。SCAN 是有预算的弱一致遍历，COUNT 不保证返回条数；扫描失败保留既有目录。Develop 查询、写入、迁移及 Cluster/Sentinel 尚未实现。消费链路验证使用 `make test-common-redis-unit` 与 `make test-common-redis`。

验证入口为 `make test-business-config` 和 `make test-business-redis`。后者创建独占 Compose project、随机回环端口和随机密码，核验认证拒绝、只读权限、原生类型、精确字节、初始化幂等和重启持久化，退出后检查容器、数据卷和网络零残留。该入口已纳入 `make test-integration`、模块/变更自动发现和 `release-and-t2-gates.yml`，不连接 Infra 或现有 Business Redis。

Redis 插件与消费契约验证入口：`make test-common-redis-unit`、`make test-common-redis`。后者复用同一独占夹具生命周期，验证真实 ACL 凭据、数据库选择、权限不足、System 密码加密/脱敏与在线状态更新，以及 Meta 键值数据集扫描和 Manager 原生预览，并已登记 Common/System/Meta/Manager/Business 变更触发的 CI 门禁。

Manager 内部产物验收 `manager-internal-artifact-lineage` 使用 GitHub Hosted Ubuntu x86_64 临时部署，由 `scripts/test/online-hosted-manager-gate.sh` 从零建立最小权限身份、Business MinIO 与真实 Runtime，复用 `make test-online` 业务断言并在退出时销毁环境；无需永久账号或自备 Runner。首次真实运行成功前只登记手工触发，不计为 T4 通过。

Transfer SQL ETL 与字段血缘验收 `transfer-relational-sql-etl` 唯一使用 GitHub Hosted Ubuntu x86_64 临时部署，由 `scripts/test/online-hosted-transfer-gate.sh` 编排。Business owner `scripts/online-transfer-relational-sql-etl-fixture.sh` 从零创建独占 tmpfs PostgreSQL 容器、5 行固定源表及只读取源表/创建目标表的数据库用户，输出 owner-only Engine 描述；System 负责临时身份和正式 Engine API 注册。业务断言沿用 `make test-online`，owner 核对 SQL 投影过滤、原生 replace/两跳结果和 decimal 精度，退出删除容器并验证零残留，不接管本地 `business-postgres` 或读取 Business `.env`。

Redis 正式 Online 入口为 `make test-online ONLINE_SUITE=redis-consumer-flow`，由 `online-t4-gates.yml` 人工触发 `redis-hosted-t4` Job，经 `scripts/test/online-hosted-redis-gate.sh` 准备一次性 Ubuntu x86_64 部署。物理夹具复用上面的固定镜像、ACL 与九个样例，使用随机回环端口、随机密码和 tmpfs；System helper 建立非默认 Tenant、最小权限普通用户与独立引擎登记身份。断言覆盖 System 实时目录/事实、Meta keyspace 身份及重扫稳定性、Manager 九个原生预览、大整数/二进制/TTL，并实际登录 Console 从 Meta 页面重扫及打开 Manager 预览，归档截图和同一身份的报告。成功、失败及中断都销毁当次业务容器、平台 Infra 和凭据目录；不接管个人开发环境。脚本确定性验证纳入 `make test-online-runner`，仅人工触发，不登记 schedule。

Hosted T4 已于 2026-10-04（北京时间）复验通过：[运行 37169297656](https://github.com/pampa0629/addp/actions/runs/37169297656)，提交 `7398d24d743b96316a338dce4a4b39675b979d7b`。归档报告确认同一租户、普通用户和引擎完成 Meta 页面重扫及九个原生预览，并保留重扫和各样例共十张 Console 截图。该记录只证明对应提交的消费链路，不替代后续变更的重新验收。


## HDFS Simple 实验环境

现有手动 Hosted `hdfs-spark-consumer-flow` 同时验收 Spark → MySQL/Doris 的正式页面链路。Business 夹具复用真实 Worker T2 的固定镜像与 Doris 唯一初始化，为当次 Run 建立独占数据库；`online-spark-table-oracle.py` 只核对原生五行精确值、类型、表身份、DDL 与暂存清理，不访问平台 API。首次创建经 Develop 执行并自动扫描后，System 准备身份签发精确表读取 Grant，随即撤出准备凭据；普通 User 继续覆盖、Manager 预览和 Spark 复用。全部退出路径先检查目标表删除及零暂存，再销毁容器与卷。Console 的 Meta 重扫必须经过正式范围／深度确认框，确认仅扫描 `samples`、基础扫描与强制识别，并核对实际 POST 载荷后继续观察执行；不能点击重扫后直接等待请求或通过 API 代替页面确认。原 HDFS 汇总结果的六个计数／金额列均为 bigint：Manager 预览必须按 API 规范返回十进制字符串，Spark Runtime 摘要和下游复用继续核对原生整数；测试分别比较两个边界，不转换实际响应来兼容两种表示。协议回归仍为 `make test-hdfs-online-runner`，真实页面证据由原 Hosted suite 归档；实现和脚本回归不代表扩展已通过真实 T4。

扩展后的 Hosted T4 已于 2026-10-18（北京时间）通过：[运行 39067187419](https://github.com/pampa0629/addp/actions/runs/39067187419)，受验提交 `f82b1f8904dc21bc6b17396073950f93bf2ca316`。六个服务与 Spark Runtime 均核对同一源码，独立 Tenant 普通 User 完成 Meta 范围确认重扫、CSV／JSON／Parquet 及中文空格百分号路径四源预览，各 20 行、金额合计 2100；JSON 在 basic 重扫后保持已由 deep 确认的表格语义。PostgreSQL 结果、两次独立 MinIO whole Parquet 产物及后续 Spark 复用通过；MySQL／Doris 的五行精确值、首次创建、覆盖、自动扫描、原生表身份／DDL 保持及 Spark 复用通过，保留 bigint、Decimal(38,18)、null／空串／尾随空格。已归档并核对 20 张真实 Console 截图及 browser 收据；业务夹具、Runtime 和 Infra 清理通过，双库目标已删除、暂存表为零。此记录只证明该源码下声明的手动 Simple／Standalone 消费与发布场景，不替代其他部署形态或故障注入验证。

该 Hosted 夹具使用 Linux host 网络，必须显式将 Doris 容器主机名 `doris` 映射到 `127.0.0.1`；只设置 `--hostname` 不会建立地址解析。FE 启用 FQDN 模式后会解析自身主机名，映射缺失时因 `UnknownHostException` 退出，而 BE 仍可能运行并等待心跳。因此就绪核验继续要求 FE 可查询且唯一 BE 为 Alive；失败归档保留 FE 启动初段、日志文件和主机名解析结果，不能通过延长等待或仅检查容器存活掩盖启动失败。

五行边界样例的物理列类型与生产建表映射一致：MySQL 为 `BIGINT / LONGTEXT / DECIMAL(38,18)`，Doris 为 `BIGINT / VARCHAR(65533) / DECIMAL(38,18)`。核验须精确匹配类型、符号、字符串容量及数值精度，不能把 Doris 的原生 VARCHAR 错当 STRING，也不接受任意字符串类型替代。物理核验子进程只将 JSON 回执写入 stdout，错误诊断保留在 stderr；凭据继续留在容器环境，不进入命令参数或归档。

该场景的隔离部署同时启动 Security，并将其纳入同提交服务预检；System 身份夹具在导出消费凭据前，通过既有 `--wait-protection-owners develop,manager` 核验新租户的持久保护投影已完成初始化。服务监听就绪不替代租户投影就绪；缺少 Security 或初始化失败须在业务验收前退出并清理，不增加消费用户权限，也不绕过 Manager／Develop 的保护门禁。

在仓库根运行 `bash business/scripts/start.sh -hdfs`，启动固定官方 Hadoop 3.5.0 NameNode 与 DataNode，并使用固定 Spark 3.5.0 容器幂等生成样例。数据保留在两个独立 volume；`stop.sh -hdfs`、`restart.sh -hdfs` 沿用同一标准生命周期，不重新格式化已有 NameNode。初始化只更新 `/addp/samples/orders.csv`、`orders.json`、`orders.parquet` 和根文件 `/addp/订单 100%.csv`；每份 20 条订单，金额总和 2100，其他业务文件不变。

本机 Docker Desktop 部署的 WebHDFS 首选端口 9870，原生 RPC 首选 8020；DataNode 数据端口 9866 与 HTTP 端口 9864 在容器内外保持一致。四个宿主端口均只绑定 `127.0.0.1`，DataNode 广播稳定主机名 `host.docker.internal`。宿主机需在 `/etc/hosts` 配置 `127.0.0.1 host.docker.internal`，容器继续使用 Docker Desktop 内置解析；标准入口在启动前验证宿主机映射。宿主机 Go 服务、Spark Driver 和容器 Worker 使用同一主机名访问 RPC 与 DataNode，换 Wi-Fi、VPN 或局域网 IP 后无需重绑端口或修改 System 连接。删除 `HDFS_SHARED_HOST` 和自动检测局域网 IP 的旧逻辑；四个端口冲突直接失败。其他部署由部署方提供实际可达的地址，不复用此本机部署的地址假设。

System 中选择独立 `hdfs` 引擎：填写启动输出的 `webhdfs_endpoint` 和 `rpc_uri`，`root_path=/addp`、`authentication=simple`、`user=addp_business_reader`。根目录不进入 locator；例如 `samples/orders.parquet` 保持 `type=file`。Spark 计算集群单独选择，源文件通过已授权 locator 在执行时派生原生 HDFS URI；任务不保存连接参数。Spark Workflow 部署设置 `HADOOP_USER_NAME=addp_business_reader`，Worker 使用同一应用用户；身份不符或 HDFS 保存请求会被拒绝。Simple 是开发实验的主体声明，首版不提供 Kerberos、HA、YARN、MapReduce 或 HDFS 写回。

`make test-common-hdfs-unit` 与 `make test-spark-workflow` 验证确定性契约；`make test-common-hdfs` 创建专属无宿主端口集群，验证 WebHDFS、共享格式解析、样例幂等、真实 Worker 的 CSV/JSON/Parquet 读取聚合、PostgreSQL 普通和空间结果事务保存、已有表结构及权限保留、失败回滚、实际行数与结果再次读取，以及退出后的容器、卷、网络零残留。正式消费链路使用手动 Hosted T4 `hdfs-spark-consumer-flow`；`scripts/online-hdfs-spark-fixture.sh` 只在干净 Linux x86_64 Runner 管理当次宿主网络 Hadoop/Spark/PostgreSQL/MinIO 容器及现有样例，输出 owner-only 描述，由 System 独立登记。独立准备 User 通过 System 正式独立批准命令，仅为普通消费 User 授予 `results.hdfs_totals` 的精确读取 Grant，并核对授予前拒绝、授予后通过；消费 User 不拥有签发 Grant 或管理委派的 Permission，准备凭据在业务验收前撤出环境。T4 在原三格式读取基础上验证该 User 的结果保存、自动扫描、血缘、Manager 预览和下游读取；执行或写入权限不替代当前读取 Grant。PostgreSQL 凭据及 seed SQL 只保存在当次 secret 目录，不归档。Spark Master 与 Worker 固定同一官方 digest。`make test-hdfs-online-runner` 验证隔离生命周期与断言；真实 System/Meta/Manager/Develop 和 Console 仍须由该 T4 实际运行通过，不能仅以 T1/T2 声明平台验收完成。

Hosted T4 已于 2026-10-05（北京时间）通过：[运行 37217772982](https://github.com/pampa0629/addp/actions/runs/37217772982)，提交 `11bbacfd577cb568f22c47a0a9fe1a39fd3a70bc`。报告确认同一租户与普通用户完成 Meta 目录重扫、四个文件预览和 Develop 正式工作流详情；三格式各 20 行、金额合计 2100，Standalone Application 对应的真实 Worker 完成 37 个 Executor 任务，归档六张 Console 截图。应用、业务夹具和 Infra 清理通过且零残留。该记录证明此提交在标准开发入口下的 Simple 只读消费链路，容器部署入口另须验收；仍只登记手动触发。

产品容器默认入口已于 2026-10-10（北京时间）复验通过：[HDFS T4 37954899129](https://github.com/pampa0629/addp/actions/runs/37954899129)，提交 `ddaa982600608c94db56e04ac5ed130765cd77c1`。Spark Workflow 产品镜像自动登记后，同一租户与普通用户完成扫描、四文件预览及八节点正式工作流，保留六张 Console 截图；三格式各 20 行、金额合计各 2100，实际 Standalone Worker 完成 37 个任务，连续八次 Runtime 状态查询一致。产品镜像实际 image ID 与 `python api_server.py` 默认入口核验通过；Business 容器与 Runtime/临时镜像仓库分别按自身所有权清理，最终 `result=passed`、`cleanup=passed`、`infra_cleanup=zero_residuals`。该记录只证明对应提交的 Simple 只读消费范围，仍只登记手动 T4。

MinIO Parquet 成果沿用上述同一 T2/T4 入口。T2 另有两个独占 MinIO endpoint，验证同名 bucket 不串用连接、空成果读取、Magic Committer 提交、pending 记录中 upload ID 的精确取消及兄弟上传保留、记录缺失／损坏时保留证据；T4 在 System 正式登记当次 MinIO，连续两次发布到独立目录，核对 `table/parquet/whole`、自动扫描、精确血缘、Manager 预览和下游 Spark 读取，并复查第一份成果保持不变。MinIO 凭据不归档，不使用 Infra 的数据或凭据。此处描述验收契约，实际通过记录须对应当前提交的运行证据。

## Business FalkorDB

独立业务图实例使用固定 FalkorDB 4.20.6 镜像摘要、独立持久卷和随机凭据。先在 `business/.env` 配置 `BUSINESS_FALKORDB_PASSWORD`，然后运行 `bash scripts/start.sh -falkordb`；首选端口 `6381`，实际映射以标准启动输出为准。`stop.sh -falkordb` 停止容器并保留数据卷，`restart.sh -falkordb` 复用原数据和端口；属于 `-all`。System 以 `falkordb` 类型登记业务地址、用户 `default`、密码和 Redis DB `0`，不能登记 Ontology 私有端点。

应用户明确要求，可在首次启动前将 Infra RDB 的一次性快照恢复到空业务卷，以便 Graph 浏览内部图结构。该操作不是日常初始化或持续同步，不覆盖已有业务数据，不修改或共用源卷。快照和临时凭据放仓库外私有目录并在校验后清理。Graph 从副本推导本体并绑定具体 graph key，需要全文搜索时选择“由 Graph 维护”，只在业务副本创建索引。配置验证为 `make test-business-config`；Graph / Develop 通用引擎实库门禁沿用 `make test-common-falkordb`。本机副本体验不计为隔离环境 T4。
