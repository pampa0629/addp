# ADDP 数据引擎扩展指南

本文只说明新增一种数据引擎时的开发步骤。接口边界见 [addp引擎插件接口规范.md](addp引擎插件接口规范.md)，能力声明结构见 [addp引擎能力声明规范.md](addp引擎能力声明规范.md)，路径规则见 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

---

## StarRocks 准入范围（2026-10-18 已确认）

先验证官方 `starrocks/allin1-ubuntu:3.5.21` OCI 索引 `sha256:faf5f6b17a301b276b829482af11d350004833dc9162232e1083a2aad0fa8b2a` 的 Linux ARM64／amd64 原生启动和 MySQL 协议，不以镜像 manifest 或交叉编译代替实库。协议测试归 `common/engine/certification/starrocks/`，Business 只拥有数据库与样例，不登记 Engine；准入未通过前不添加生产插件或 capabilities。

首版拟接入独立 `engine_type=starrocks`，仅 `default_catalog` 内部普通表的目录、基础字段／键事实、扫描、预览及参数化只读查询。不得直接继承 MySQL／Doris 的写入、事务和元数据假设：StarRocks 的部分 Information Schema 视图仅为占位，事务只读变量是否真正约束写入须用具有写权限的账号做反例实测。无法证明的能力不声明。通过后复用已有纯 Go MySQL 驱动，不引入第二套驱动；本阶段不包含外部 catalog、Spark、Stream Load、写入或 CDC。

Business 后续使用独立 `-starrocks` profile，先不加入 `-all`，官方默认单机 FE／BE 路线、独立持久卷及管理员／业务账号；SQL／FE HTTP／BE HTTP 首选回环端口分别为 9031／8031／8041。测试使用带归属标签的临时镜像、容器、卷和随机回环端口，退出验证零残留，不操作个人 Business 服务。

## 一、扩展步骤

1. 对只有厂商离线介质、或运行环境受 OS/CPU 架构限制的引擎，先通过 T5 官方介质认证确认许可证、介质完整性、原生启动、驱动协议和关键 SQL 能力；认证不得伪装成平台已经支持该引擎。
2. 在 `common/engine/plugins/<engine_type>/` 新建插件包。
3. 实现 `EnginePlugin` 基础接口；可由用户注册的引擎同时实现 `ConnectionSpecProvider`，并让默认端口、必填、敏感和身份字段接口从同一 `ConnectionSpec` 派生。
4. 按引擎能力实现需要的 provider。
5. 返回结构化 `Capabilities()`。
6. 按 `EngineOrigin()` 加入 `common/engine/plugins/builtin/general` 或 `common/engine/plugins/builtin/extension` 聚合包。
7. 补充单元测试和必要的 integration 测试。

官方介质事实由 `scripts/lib/<engine>-official-media.sh` 唯一持有版本、按架构下载地址和完整性校验值；Release owner 脚本持有认证专用启动参数、测试夹具、证据和清理逻辑，协议断言放在 `common/engine/certification/<engine>/`，不能用只有测试文件的插件目录冒充生产实现；workflow 只能调用 `make test-release RELEASE_SUITE=<suite>`。认证通过后仍必须建立独立 `engine_type` 和插件实现，不得因 wire protocol 兼容而复用其他数据库身份。

GaussDB 的首阶段选用[官方混合云开发者版](https://www.huaweicloud.com/product/gaussdb/download.html) V2.0-10.0.0、20260915 Ubuntu 22.04 介质。x86 ZIP SHA-256 为 `c53cf2021d86c4820e8c6fddf13a098e1b2b1f4a2ca142c6dcf1ff2fa9ab54a8`，ARM ZIP 为 `a6442f7c12c396aae2e96e3dde8b4eca2eac45ab26dce1735efc530f447990e5`。ARM64 是必需准入架构；`make test-release RELEASE_SUITE=gaussdb-official-media` 在原生 Linux x86_64、ARM64 Runner 分别用对应介质构建独占 Ubuntu 22.04 镜像并运行同一协议断言，不以 QEMU 或交叉编译代替原生实库。官方当前下载页提供安装包，尚未确认可直接使用的官方容器镜像；门禁构建的是 ADDP 测试镜像，不冒称厂商镜像、不对外发布介质。每轮报告记录架构、镜像 ID、官方介质校验值及驱动版本，退出时删除本轮资源并确认零残留。

正式只读接入阶段采用[华为公开 gaussdb-go](https://github.com/huaweicloud-samples/database-gaussdb-go) `github.com/HuaweiCloudDeveloper/gaussdb-go v1.0.0-rc1`，固定 Go module／go.mod 校验和，由 `scripts/lib/gaussdb-official-media.sh` 唯一维护。它经 `database/sql` 适配单独注册 `gaussdb`；认证须与 Common 固定版本 `lib/pq` 同进程共存且不设置 `GOENV_REGIST_ALL`。认证源保存在 `common/engine/certification/gaussdb/testdata/`，由标准入口复制到仓库外独立临时 module；已通过同源 `39090295754/454b71ec7` 的 Linux ARM64／amd64 原生 Business 生命周期与协议准入后，将该固定版本纳入 Common 正式依赖；标准检查必须核对 Common go.mod／go.sum 与认证事实一致，不保留离线驱动认证分支。`make test-gaussdb-certification-build` 检查 Linux／Darwin 的 amd64／arm64 四组合构建并在当前宿主运行驱动共存断言，不生成 T5 通过报告。真实 T5 保持 SHA-256 密码认证，验证只读事务、参数绑定、Decimal(38,18) 精确往返、目录及取消后的连接池恢复；跳过、执行失败或清理失败均不通过。Business 正式部署复用已确认的双架构介质，唯一镜像构建和持久化启动归 `business/gaussdb/`；Release 门禁复用该路径并检查容器重建后的卷和用户修改保留，不再内联另一套一次性 initdb。双架构和驱动验证通过后，正式插件仍须独立 `engine_type=gaussdb`，并完成对应 T2／T4，不能因协议兼容沿用 PostgreSQL 身份。

正式插件独立注册 `engine_type=gaussdb`，通过既有 PostgreSQL 协议适配器绑定独立 `gaussdb` SQL Driver，不继承 PostgreSQL 的能力声明。首版限定非空间 schema/table 目录、字段及保留原生键列顺序的主键 Facts、BatchRead、TableReadSession、QueryReadSession、参数化只读 SQL、System 登记与检测、Meta 扫描和 Manager 预览；只读由数据库事务执行边界约束；schema 沿共享协议层的 `pg_namespace` + 当前身份 `USAGE` 过滤，表／字段仍沿 `information_schema.tables/columns` 的可见性，不提升账号权限。Business 保留原生默认 A 兼容模式，因此数据库已将空字符串存为 NULL；Provider 如实返回原生 NULL，不修改数据库兼容模式或虚构空字符串。首版不声明写入、COPY、TableResult、BoundedWatermarkRead、空间、CDC、索引／分区等未经正式 Provider 验证的能力。普通业务账号的新连接、无权表隔离、大整数及 Decimal 精度、查询取消和读取集合必须由对应 T2／T4 核验，不能以 T5 协议通过代替平台消费验收。测试复用 Business 唯一镜像和初始化，使用独占资源并核对零残留，不接管个人开发容器或数据卷。

开发者版[服务声明](https://www.huaweicloud.com/declaration/gaussdb-developer.html)只允许学习、适配验证和测试，不能用于商业服务；官方说明无 License 控制，不支持逻辑解码、主备和容灾。本准入不代表商业版、其他版本、分布式集群、DWS、CDC、写入或完整 ADDP 消费链已获支持。

GaussDB 官方内核首次启动后必须先通过本地 `gsql` 执行 `ALTER ROLE omm PASSWORD` 完成初始用户改密，再检查 `SELECT 1` 和创建认证夹具；相关用户规则见[官方管理员说明](https://support.huaweicloud.com/centralized-devg-v10-gaussdb/gaussdb-42-0010.html)。认证入口使用不同的随机初始化密码和最终密码；两份密码均为28个字符，包含96位随机量及大小写字母、数字和特殊字符，符合固定内核默认8～32字符的[密码长度约束](https://support.huaweicloud.com/intl/zh-cn/centralized-ref-v10-gaussdb/gaussdb-38-0999.html)，不得修改密码策略来容纳测试密码。SQL 通过 stdin 传入，启用 `ON_ERROR_STOP=1`，返回码3表示脚本SQL错误时立即失败，不重复改密直到超时；归档前对两份密码统一脱敏；不得禁用密码安全策略或放宽 SHA-256 认证以跳过初始化。Run 39036730173 已证明内核可启动，但因未完成初始用户改密而在就绪阶段失败，清理零残留，未进入协议认证。Run 39040181663 已到达改密SQL，但认证脚本生成的34字符密码超过默认上限，在初始改密阶段失败且清理零残留；该轮也未进入协议认证。

此前固定介质的离线 Go 驱动默认重复注册 `postgres`，且缺少 Darwin 的 `setSocketParameter` 实现，因此未进入 Common 生产依赖。Run 39042984305 已进入真实 x86 协议验证，认证、精确数值、目录及只读事务通过，但取消查询后连接池恢复返回 `driver: bad connection`，T5 失败且清理零残留。该历史证据不构成新候选驱动或 ARM64 支持证明。

openGauss 的准入基线固定为 6.0.6 LTS 官方 Docker tar：Linux x86_64 Release 认证使用 `make test-release RELEASE_SUITE=opengauss-official-media`，Business 和常规 T2 使用同一固定 URL、SHA-256 与镜像。官方 6.0.6 Docker 构建包含 MOT，Docker Desktop for macOS 缺少其所需 NUMA 拓扑，所以 openGauss 实库门禁是显式登记的 hosted-only T2，不进入 macOS `make local-ci`；不得改用第三方镜像、跳过用例或覆盖系统配置制造本地绿色。Release suite 验证原生容器启动、PG 兼容 database、`lib/pq` 参数绑定、COPY、MERGE、复合 watermark、唯一键目录查询和查询取消。正式插件独立注册为 `engine_type=opengauss`，内部复用 PostgreSQL wire protocol 与 SQL 方言实现，但只显式暴露已验证的非空间目录、Facts、查询、批量/游标读取、COPY 写会话、建表、删除、bounded watermark 和幂等 upsert；不声明 PostGIS、CDC、分区变化应用或 PostgreSQL 扩展能力。

TiDB 的准入基线固定为 8.5.8，并且只使用 PingCAP 在 Docker Hub 公开发布的 `pingcap/pd`、`pingcap/tikv`、`pingcap/tidb` 三组件镜像；Business、Linux x86_64 GitHub Hosted T2 与 macOS Docker Desktop T2/T4 必须使用同一版本并按 OCI digest 固定，不使用 TiDB Enterprise 镜像、第三方重打包镜像或需要人工申请的介质。TiDB 源代码与官方镜像路线基于 Apache 2.0，可免 License 文件和激活用于无人值守开发测试；正式插件独立注册为 `engine_type=tidb`，内部复用 MySQL wire protocol、SQL 方言和已认证的 MySQL-compatible Provider，但只声明非空间目录、Facts、查询、BatchRead、bounded watermark、建表、删除、写会话和幂等 upsert。TiDB 普通只读 SQL 固定使用共享语句校验边界，bounded watermark 在 Snapshot Isolation 中只执行内部生成的 `SELECT`；不得启用只有 noop 语义的 `READ ONLY` 兼容开关。普通表 upsert 继续复用共享批处理和唯一键约束，但冲突值引用固定使用 [TiDB 官方支持的 `VALUES(column)`](https://docs.pingcap.com/tidb/stable/miscellaneous-functions/#values)，不得把 MySQL 8 insert row alias 当成全体 MySQL-compatible 引擎的共同语法。官方兼容性文档明确不支持 MySQL replication protocol 与空间类型/索引，因此不得推导 CDC、分区变化应用或空间能力。

KingbaseES 的准入基线固定为 V9R1C10 `V009R001C010B0004` 官方 Docker tar：x86_64 SHA-256 为 `16a436608cc204349e510cb136b8fc1fcbdf6874aee7b204cdac20a3522282da`、官方 MD5 为 `26bb99891becc52f533488aac5fabfb6`，ARM64 SHA-256 为 `3d08f5a99f5659723c34315b71d49f783847cba19c7a0c61c2628bf9f131c8ee`、官方 MD5 为 `7b1661123cf7fc34c6be3553e8b6d24d`。Business 按宿主 CPU 选择同一 build 的介质并统一加载为固定中性本地镜像引用，不建立两套 Compose 服务。只开放 `DB_MODE=pg` 与 `engine_type=kingbase`；正式 T5 必须在 Linux x86_64 证明 `KingbaseES V009R001C010`、`database_mode=pg`、`lib/pq`、参数绑定、COPY、`ON CONFLICT`、复合 watermark、唯一键目录查询和查询取消，并显式证明 openGauss `MERGE` 不在本方言路径。真实目录还必须证明 KingbaseES 系统 schema 以及 `public.sys_stat_statements`、`public.sys_stat_statements_all` 不进入业务枚举、leaf count 或直接路径解析。官方镜像内置 90 天试用 License 只允许一次性准入评估，不得通过新建容器重置试用期；长期 T5/T2/T4 仅在受保护的 owner-managed Linux x86_64 Runner 上使用 x86_64 介质和 owner 正规获得、在有效期内且 SHA-256 明确的 License 文件运行。介质、镜像和 License 都不重分发；GitHub Hosted 与 macOS Docker Desktop 不是正式门禁路径。

引擎选型同时考虑重点行业采用、介质可验证性和长期无人值守授权条件。[PingCAP 微众银行案例](https://www.pingcap.com/case-study/webank-cuts-costs-scaling-tidb-petabytes/)、[达梦官方行业案例](https://eco.dameng.com/cases/)和[人大金仓官方一汽案例](https://www.kingbase.com.cn/explore/tech-blog/%E6%97%B6%E5%BA%8F%E6%95%B0%E6%8D%AE%E5%BA%93%E9%87%8D%E5%A1%91%E6%99%BA%E9%80%A0%E6%9C%AA%E6%9D%A5%EF%BC%9A%E9%87%91%E4%BB%93%E6%95%B0%E6%8D%AE%E5%BA%93%E5%BC%95%E9%A2%86%E5%B7%A5%E4%B8%9A%E4%BA%92/)提供金融、政务及制造业采用依据。[TiDB、PD、TiKV 开源仓库](https://github.com/pingcap)采用 Apache 2.0，官方 Docker Hub 三组件镜像可公开拉取并固定 OCI digest，启动不需要 License 文件、激活或人工申请；[DM8 官方试用说明](https://eco.dameng.com/document/dm/zh-cn/faq/faq-dm-product)和[KingbaseES License 手册](https://help.kingbase.com.cn/v8/install-updata/license-information/license-information-2.html)说明试用／临时授权有明确期限，不能作为长期免人工授权的 GitHub Hosted 基线。不得以第三方镜像绕过授权条件。

KingbaseES V9R1C10 采用受保护的 owner-managed Linux x86_64 Runner 路线：官方精确 build 的 Docker tar 与 MD5、ADDP 独立计算的 SHA-256、有效正规 License 和介质缓存共同构成准入条件。临时授权有严格有效期，商业授权通常绑定稳定主机并由厂商签发；owner 负责到期续授。不公开归档介质或 License，不以 ephemeral Runner 或重建容器重置试用期。KingbaseES 与 DM8 分别遵守各自的介质、架构和授权基线。

达梦 DM8 只保留 `dm8_20260708_HWarm920_kylin10_sp1_64.zip` 一条官方介质路线：ZIP SHA-256 固定为 `d6871147cd4a04e1595d9dedf9d05245c55b17bff2ae738dded37fe568df9824`，其中 ISO SHA-256 固定为 `2a8a4844527e901718a88b4c747d7fd44460bcb2a862956bba98146760b8423e`，服务端版本标识为 `03134284604-20260707-335949-20228`。同介质携带的官方 Go 驱动 ZIP SHA-256 固定为 `96516ecd7c9ec3c405109b81135f2c9d87ed0631632e99cd2de0e06f03b1392e`；驱动只允许内部使用，不进入仓库、镜像仓库或 Artifact，也不修改、派生或重分发。厂商目标环境是鲲鹏 920 与麒麟 10 SP1；macOS ARM64 Docker Desktop 只能在 `linux/arm64` Ubuntu 兼容层中做非认证 ABI 技术验证，不能据此扩展为通用 ARM64 或生产支持声明。Business 只构建本地、不可推送的无卷 disposable 镜像；内置试用授权固定到期日为 2027-07-07，到期后必须停止使用并改为 owner 正规申请、与正式 ARM64 环境匹配且 SHA-256 明确的 License，不得通过重建容器、替换时钟或第三方镜像绕过许可。

DM8 独立登记为 `engine_type=dameng`。插件描述、连接字段、SQLDialect 和能力模板属于可由 macOS System 加载的控制面代码；官方 Go 驱动不作为 Go Module 依赖提交，而由门禁从上述固定官方介质提取、校验后只注入 Linux ARM64 Docker 构建上下文。真实 TestConnection、Engine Catalog、参数化查询、BatchRead、TableReadSession、QueryReadSession、BoundedWatermarkRead、TableWritePrepare、TableWriteSession、Delete 与基于 `MERGE` 的幂等 TableUpsert 必须在该执行边界内完成 T2。macOS System 可以创建 Engine Instance，但宿主进程不得执行连接检测或把实例伪装为 `online`；主要模块技术验证也必须把实际 Provider 调用进程放入同一 Linux ARM64 Docker 边界。Business 只拥有数据库与样例生命周期，不调用 System API；上层模块只消费 capabilities、ResourceLocator、Provider 和 SQLDialect，不增加 `dameng` 类型硬编码。

HighGo 接入使用独立 `engine_type=highgo`，复用 PostgreSQL 方言和关系目录 Provider。开放连接、非空间实时目录与普通字段 Facts、BatchRead、TableReadSession、QueryReadSession 和参数化只读 SQL。写入扩展开放非空间 TableWritePrepare、BatchWrite、COPY TableWriteSession 及可见普通表 Delete，继续复用同一 PostgreSQL Provider 与 `highgo` 驱动；不开放任意写 SQL、upsert、CDC、空间、分析编译或 PostgreSQL 扩展。关系 Facts 复用共享 PG catalog 回调，开放有序列索引、主键／唯一／外键约束和字段分区布局；拥有者通过 `ProtocolIdentity.CatalogFacts` 绑定同一能力声明，其他 PG 协议插件不自动开启。Meta 请求的空间补充仍不执行，普通关系事实正常返回。写入上线须通过隔离原生认证：随机且本次独占的测试 schema、合成数据、COPY 提交／Abort、失败批次原子回滚和精确类型往返；清理只删除本次已创建表及 schema，并核实零残留。

类型展示名称使用 `HighGo`，中文界面显示“瀚高（HighGo）”；V4.5 表达当前已验证的版本范围，不嵌入类型名称，也不按版本建立不同 `engine_type`。引擎实例的数据库实际版本由通用连接探测契约返回，System 以只读 `server_version` 和 `server_version_checked_at` 保存并在详情展示；它与平台登记修订号 `Engine.version` 是不同事实，不能混用。当前名称收敛不改变已有版本准入限制。

连接探测以服务端 `version()` 的厂商标识和 4.5 版本系列为准，接受原生中文 `瀚高数据库管理系统V4.5` 和英文 `HighGo Database Management System 4.5` 表述。版本识别使用同一严格边界规则，不依赖构建架构或显示语言；PostgreSQL、其他厂商、4.50、其他主次版本以及含伪造嵌入标识的字符串均拒绝。

HighGo 普通业务目录不包含安全审计系统表 `hg_t_audit_log`，也不包含运行监控视图 `pg_buffercache`、`pg_stat_statements`。`hg_t_audit_log` 归三权分立的 SYSSAO 管理；SYSDBA 的通用 ACL 探测可返回 SELECT，但原生读取仍被拒绝，不能据此扩大权限或关闭审计。插件通过已有 `ProtocolIdentity.AdditionalSystemTables` 统一隐藏这些保留对象，目录数量、表列举和精确叶子解析／字段 Facts 使用同一规则；旧缓存路径返回 `not_found`。普通业务 schema 和其他用户表保留，Meta 通过标准完整范围刷新收敛此前误发现的技术元数据，不删除源数据库对象。

用户已确认官方 Go 包不可取得时采用第三方路线。驱动主体固定为 ADDP 已使用的 lib/pq v1.10.9，SM3 扩展固定取自 [iwanlebron/pq](https://github.com/iwanlebron/pq) 的 `6ff687fe05e4ef24b5d3abcb9e92d5bab94a958b`，按 MIT 许可保留声明，存放于 `common/engine/internal/highgopq`。本地适配登记唯一 `highgo` 驱动名，复用现有 `github.com/lib/pq/oid`、`scram`、公开错误类型和 `github.com/emmansun/gmsm/sm3`；不引入第三方分支的存储过程改动，不全局替换 lib/pq，不引入认证代理或认证降级。可信的 `ProtocolIdentity.DriverName` 统一绑定连接检测、GORM 连接池和 SQL 会话，默认端口 5866、`sslmode=require`，显式 TLS 设置按连接配置执行。

HighGo 普通表删除扩展沿用 `ResourceDeleteProvider` 与同一 PostgreSQL DROP 实现，先按精确叶子取得当前可见表 Facts，仅接受 `kind=table` 且原生 `relkind=r`，并复用非空间定义检查；目录隐藏或不存在的叶子不执行 DROP，其他目录／权限错误不得吞掉。仅接受 schema/table 路径，不删除 schema、不使用 CASCADE，不扩大账号权限；有依赖对象时由数据库拒绝。须在本次独占 schema 验证普通表删除、重复删除、引号标识符、同级表保留和依赖拒绝，且原生认证通过后才能开放删除能力。写会话的 Replace 沿共享 PostgreSQL 会话验证：事务内 DELETE 与 COPY 一起提交或回滚，覆盖正常提交、空输入、Abort、重复键、值编码及 COPY 准备错误，不另建 HighGo COPY 路径。会话事务不覆盖 Transfer 在此前执行的 DROP／Prepare。

写入回归沿用 `scripts/test/common-postgres-gate.sh`：默认完整入口及 `--test highgo-write` 在 disposable PostgreSQL 上验证 HighGo 驱动绑定与共享 SQL/COPY 契约，现有 Common PostgreSQL CI 自动覆盖；这不等于原生瀚高认证。显式 `--test highgo-write --highgo-native` 只消费调用方注入的 `ADDP_TEST_HIGHGO_HOST/PORT/DATABASE/USER/PASSWORD`，强制 TLS、校验 V4.5，不创建／删除 database，不读取既有业务表数据；仅附加 `public` 系统审计对象的只读目录过滤核验。原生认证不得自动切换驱动或重试未知结果的写入。

驱动实现标准 `driver.DriverContext`，将调用方 Context 传递到建连过程；TCP、SSL 协商、TLS 和 SM3 握手期间取消或超时必须关闭本次连接并保留 Context 错误，握手完成后解除取消监听，避免误关已交付的连接。System 手动与周期检测共用 10 秒预算，真实失败仍更新离线状态，执行授权仍核验引擎可用性。确定性 SM3、三阶段握手取消、成功后取消监听释放和登记测试由 Common T1 自动发现，探测预算一致性由 System T1 自动发现；现有 Platform CI 的 Go workspace／插件登记及 Release/T2 消费者门禁覆盖相关改动，无新增运行服务或 CI 外部依赖。

HighGo 的协议回归、原生 Provider、用户环境消费链与 Hosted T4 必须分别认定。短时连通性和只读扫描不代表长期稳定性、全量字段扫描、写入或完整产品认证；原生 Provider 写入不替代跨模块消费链，x86_64 Hosted 消费链也不替代其他架构的实例版本页面验收。普通字段 Facts 由 Provider 路径验证，不以工作台直接查询受保护系统目录替代。历史源码、执行身份、通过范围和未覆盖边界统一见[测试与验收规范的证据索引](testing/验收证据索引.md#addp-验收证据索引)。

聚合包是内置插件编译期登记的唯一手写清单。`make test-engine-plugin-registration` 会自动扫描调用 `plugin.Register` 的生产包，校验每个包恰好进入与 `EngineOrigin()` 一致的一个聚合入口，并禁止上层生产代码通过 blank import 直接加载具体插件。测试和 System API 不得另行维护全量插件类型清单或固定数量；它们应从已登记插件和 descriptor 动态验证通用契约，具体引擎的端口、能力和字段语义由各插件包自己的测试拥有。

System 的引擎类型列表、注册表单、默认值和校验规则均由 `GET /api/v1/system/engine-types` 返回的插件描述驱动。新增引擎时不得在 `system/frontend` 或 `common-frontend` 增加 `engine_type` 判断；若现有 `ConnectionFieldSpec` 无法表达所需交互，应先扩展通用描述协议和共享渲染器，再实现具体插件。

SQL 引擎必须通过 `SQLDialectProvider.SQLDialect()` 声明稳定方言，`SQLQueryRuntimeProvider` 组合该接口和 SQL 执行能力；通用查询生成、标识符引用、分页和参数占位符只消费该方言。新增 MySQL 协议兼容数据库时仍使用独立 `engine_type`，不得为了复用驱动而登记为 `mysql`，也不得在 common 或上层模块增加新的 `engine_type` 方言分支。

上层模块通过 `common/engine/plugins/builtin/general`、`common/engine/plugins/builtin/extension` 或 `common/engine/plugins/builtin/all` 统一加载内置插件，不应散落 blank import 具体引擎插件包。`common/dbbridge` 只消费聚合后的插件注册表。

---

## 二、接口选择

Oracle 11g 目录扫描与较新实例共用 Oracle Provider，系统模式过滤的字典探测规则见《引擎插件接口规范》的 SQL catalog facts provider 差异矩阵。Common T1 自动发现无 `ORACLE_MAINTAINED` 列和有该列的两类字典回归；既有 `make test-common-oracle-decimal` 的 Oracle Free 23 T2 同时运行只读目录检查，现有 `release-and-t2-gates.yml` 的 Common Oracle job 已按 Common 变更命中，无需新建 workflow。

对用户明确授权只读检查的现有 Oracle 实例，可复用同一标准入口的只读模式：

```bash
# 事先通过环境变量提供 ADDP_TEST_ORACLE_HOST、ADDP_TEST_ORACLE_PORT、
# ADDP_TEST_ORACLE_SERVICE_NAME、ADDP_TEST_ORACLE_USER、ADDP_TEST_ORACLE_PASSWORD。
# ADDP_TEST_ORACLE_SCHEMA 可选，默认为连接用户的大写模式名。
make test-common-oracle-decimal ORACLE_READONLY=1
```

只读模式只选择 `TestIntegrationOracleReadOnlyCatalog`，强制使用原生只读事务，检查模式、表清单以及最多五张普通表的字段、索引、约束和分区事实；不读取业务行值，不创建夹具、容器、表、账号或授权，不运行同包的写入测试。凭据缺失、字典查询失败或测试跳过均为失败。它是现有实例的有界技术验收，不能替代 disposable T2，也不代表完整扫描、分页查询、写入或 CDC 已通过该版本认证。

| 引擎类型 | 必选接口 | 常用可选接口 |
| --- | --- | --- |
| 关系型 / SQL 表格型 | `EnginePlugin`、`ConnectionSpecProvider`、`EngineCatalogModelProvider`、`EngineCatalogProvider`、`EngineCatalogFactsProvider`、`SQLQueryRuntimeProvider` | `ConnectionPoolPlugin` |
| 动态 schema 记录集合型 | `EnginePlugin`、`ConnectionSpecProvider`、`EngineCatalogModelProvider`、`EngineCatalogProvider`、`EngineCatalogFactsProvider`、`QueryRuntimeProvider` | `DynamicSchemaSamplingProvider` |
| 键值存储（Redis） | `EnginePlugin`、`ConnectionSpecProvider`、Catalog Model/Catalog/Facts、`KeyValueReadableProvider` | 原生 key 值不套用表或记录集合读取契约；Meta 身份扫描与实时预览分开。 |
| 图数据库 | `EnginePlugin`、`ConnectionSpecProvider`、`EngineCatalogModelProvider`、`EngineCatalogProvider`、`EngineCatalogFactsProvider`、`QueryRuntimeProvider` | `GraphSampleProvider`、`GraphQueryProvider` |
| 对象存储 | `EnginePlugin`、`ConnectionSpecProvider`、`EngineCatalogModelProvider`、`EngineCatalogProvider`、`EngineCatalogFactsProvider` | `ContentReadableProvider`、`ContentWritableProvider` |
| 文件系统 | `EnginePlugin`、`ConnectionSpecProvider`、`EngineCatalogModelProvider`、`EngineCatalogProvider`、`EngineCatalogFactsProvider` | `ContentReadableProvider`、`ContentWritableProvider` |
| 工作流 | `EnginePlugin` | `WorkflowRuntimeProvider` |
| Notebook / 脚本 | `EnginePlugin` | `ScriptRuntimeProvider` |

旧 `ListSchemas/ListTables/ListColumns/ListBuckets/ListObjects/ListCollections` 不作为上层接口扩展点。若需要，可作为插件内部 helper，再通过 `EngineCatalogProvider` 适配为统一目录。

---

## 三、Capabilities 要求

新增插件必须返回 `engine.capabilities/v1`：

```go
func (p *MyPlugin) Capabilities() plugin.EngineCapabilities {
    return plugin.EngineCapabilities{
        SchemaVersion: plugin.CapabilitiesSchemaVersion,
        EngineType:    p.Type(),
        EngineFamily:  "tabular",
        Storage: &plugin.StorageCapabilities{
            CatalogModel: plugin.TabularCatalogModel("database"),
            Catalog:      &plugin.EngineCatalogCapability{Supported: true, RealTime: true},
            Facts:        &plugin.EngineCatalogFactsCapability{Supported: true, FieldInfo: true},
            Store:        &plugin.StoreCapability{BatchRead: true},
        },
    }
}
```

声明和实现必须一致：

- `storage.catalog.supported=true` 时必须实现 `EngineCatalogProvider`。
- `storage.facts.supported=true` 时必须实现 `EngineCatalogFactsProvider` 或采样 provider。
- `storage.store.stream_read=true` 时必须实现 `ContentReadableProvider`。
- `storage.store.stream_write=true` 时必须实现 `ContentWritableProvider`。
- `storage.store.range_read=true` 时必须实现 `RangeReadableProvider` 或在 `OpenContent` 中明确支持 offset / length。
- `storage.store.range_write=true` 时必须实现 `RangeWritableProvider`。
- `storage.store.delete=true` 时必须实现 `ResourceDeleteProvider`。
- `storage.store.batch_read=true` 时必须实现 `BatchReadableProvider`。
- `storage.store.table_read_session=true` 时必须实现 `TableReadSessionProvider`，用于大表连续批量读取。
- `storage.store.batch_write=true` 时必须实现 `BatchWritableProvider`。
- `storage.store.table_write_session=true` 时必须实现 `TableWriteSessionProvider`，用于跨批次 bulk load / COPY 写入。
- `storage.store.table_write_prepare=true` 时必须实现 `TableWritePreparer`。
- `TableWritePreparer` 对字段定义存在稳定边界时，必须声明对应的类型化 `limits`；decimal 约束使用 `limits.table_write.decimal`，上层不得按 `engine_type` 重复判断或硬编码相同上限。
- `compute.query.supported=true` 时必须实现对应 query runtime provider。

`storage.families`、`store.read`、`store.write`、`store.random_write`、`store.atomic_rename`、`store.transactions`、`store.formats` 不再作为新增插件能力声明字段。

---

## 四、路径和目录

新增存储引擎必须先定义 Engine Catalog Model：

- root 术语是什么：server、service、root 等。
- root 下第一层业务术语是什么：schema、database、bucket、directory 等。
- Engine Catalog leaf 术语是什么：table、collection、graph、object、file 等。
- full_name 如何计算。
- ResourceLocator 的 path segments 如何由 full_name 转换。

路径规则必须写入 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

对象存储和文件系统必须分别建模：

- 对象存储：`service(root) -> bucket -> prefix -> object`，`Levels` 只包含 `bucket -> prefix -> object`。
- 文件系统：`root -> directory -> file`，`Levels` 只包含 `directory -> file`。

二者不得共享 Engine Catalog Model 或 Engine Catalog 拼装实现；最多共享内容流接口、MIME 推断、格式解析等底层 helper。所有存储引擎都必须有显性结构 root；NFS 的 root `name` 使用引擎实例名称，`full_name` 使用空字符串，原生挂载根 `/` 写入 `meta_node.attributes.catalog.native_name`，`.` 不得进入 Engine Catalog path 或元数据路径。

---

## 五、模块自动消费

完成插件与 capabilities 后，各模块按能力自动消费：

- System：注册、连接测试、能力刷新。
- Meta：扫描 catalog，并按需读取 catalog facts 生成元数据快照。
- Manager：展示探查树并预览 item。
- Develop：筛选 query/workflow/script 引擎。
- Service：发布查询服务或空间服务。
- Transfer：任务配置、planner、policy、transform、worker、checkpoint、日志和指标归 Transfer；native table、query、change stream 与 content 数据面按 capability 直接消费 `common/engine` Provider，不在 Transfer 内维护按 `engine_type` 分叉的 Reader/Writer。

---

## 六、验证命令

新增插件及消费者变更使用标准影响选择入口，按 Common 的真实依赖扩散执行 T0–T3；不得拼接一组语言命令替代标准门禁：

```bash
# 当前工作区受影响 owner 的标准门禁
make test-changed

# 内置插件登记专项，不替代完整影响范围验证
make test-engine-plugin-registration
git diff --check
```

涉及前端入口时，对应 owner 门禁自动包含前端测试与构建。数据库、官方介质及跨模块真实验收仍按下述独立准入要求分别执行，不以登记检查或 T1 通过替代。

openGauss 的完成证据必须同时包含上述官方介质认证、常规 T2 disposable Provider 集成门禁和 `opengauss-consumer-flow` 跨模块 T4 验收；T4 固定在 GitHub Hosted Linux x86_64 disposable 部署执行，复用通用关系引擎消费链路断言，不能用协议认证替代模块能力验收。

TiDB 的完成证据必须同时包含固定三组件官方镜像 digest、Linux x86_64 GitHub Hosted 与 macOS Docker Desktop 均可执行的常规 T2 disposable Provider 集成门禁、Business 幂等样例，以及 macOS 专用 Runner 上 `tidb-consumer-flow` 的 Manager / Transfer / Develop / Service 跨模块 T4 与三组件清理零残留证据；T2 与 T4 复用同一个无卷 Compose，不得用 MySQL 门禁或单组件 SQL 连接冒充 TiDB 验收。T4 首次真实成功前只允许手工 `workflow_dispatch`，不得增加 schedule。

KingbaseES 的完成证据必须同时包含固定官方介质 SHA-256、owner 提供 License 文件的 SHA-256 与有效性校验、Linux x86_64 T5 官方介质协议认证、同环境真实 T2 disposable Provider 门禁、Business 幂等样例，以及 `kingbase-consumer-flow` 的 Manager / Transfer / Develop / Service 跨模块 T4 与容器零残留证据。三层门禁只在带 `self-hosted`、`Linux`、`X64`、`addp-kingbase` 标签的受保护 Runner 上执行，首次真实通过前只登记手工 `workflow_dispatch`，不得增加 schedule。

### Redis 键值数据集扫描与原生预览

`redis` 插件复用固定版本 `go-redis/v9@v9.17.2`，只支持单端点 ACL 认证和一个逻辑数据库。ConnectionSpec 使用 `host`、`port`（默认 6379）、`user`（默认 default）、`password`、`database`（从 0 开始，默认 0）、`use_ssl` 和可选 PEM `tls_ca_cert`。TLS 必须验证证书链与连接主机，不能关闭证书验证。连接检测以配置的凭据执行 HELLO（必须为 standalone）、SELECT 和 DBSIZE；此检测不读写业务 key，也不证明账号有 key 读取权限。

目录模型固定 `server -> keyspace`，连接指定的数据库对应唯一 `keyspace` 数据集，`data_type=key_value`。数据库编号属于连接边界，不进入业务路径。key/value 为数据集内容，不逐键登记 Meta；内容 key 用 `k:` 加无填充 Base64URL 编码，空 key 为 `k:`，内容键预算为 64 KiB。浏览使用有预算的原生游标，COUNT 是提示且不得丢弃多返回的键；单键读取保留原生结构和精确字节。

Catalog 不遍历键空间。内容浏览每次执行一次 SCAN，保留全部返回键，过滤 key ACL 拒绝的键；COUNT 只作提示，单批最多 1000 个键、输出最多 1 MiB，操作最长 10 秒；超限、取消或命令权限不足明确失败，不返回半份批次。游标允许空批次及重复键，不承诺稳定分页。Meta 完整重扫成功后清理旧逐 key 元数据，只保留一个键值数据集。

原生 Facts 返回类型、字节长度或元素数与实时 PTTL；读取通过 `KeyValueReadableProvider` 给出 string 字节窗口或 hash/list/set/zset/stream 有限样本，最多 50 项、string 最多 64 KiB、单个 RESP 响应最多 1 MiB。数据读取在 SDK 分配内存前检查 RESP 长度、元素数和嵌套深度；TLS 解密后同样检查。字段和值使用显式 UTF-8/Base64 字节表达，不自动转换数值字符串、JSON 或 stream ID，stream 字段对保留顺序及重复字段名。只读样本不承诺快照、稳定分页或完整导出，key 消失、类型变化和访问拒绝必须明确返回。

System 公开真实能力与 Keyspace Facts；Manager 继续要求 Meta item 身份，并以独立原生键值预览呈现。首期不声明 Cluster、Sentinel、写入、查询、搬运或字段保护适配。

最小验证为 `make test-common-redis-unit`（原生读取、资源路径、预览路由与保护边界）、`make test-common-redis`（独占真实 Redis + Common/System/Meta/Manager 消费契约）、`make test-business-redis`（原生夹具）、`make test-engine-plugin-registration`、`make test-common-frontend test-manager-frontend test-system-frontend test-meta-frontend`；共享聚合依赖由 `make test-go` 验证。现有 Redis T2 job 按 Common/System/Meta/Manager/Business 路径触发，不新增另一套测试生命周期。
