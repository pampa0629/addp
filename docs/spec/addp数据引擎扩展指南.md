# ADDP 数据引擎扩展指南

本文只说明新增一种数据引擎时的开发步骤。接口边界见 [addp引擎插件接口规范.md](addp引擎插件接口规范.md)，能力声明结构见 [addp引擎能力声明规范.md](addp引擎能力声明规范.md)，路径规则见 [addp存储引擎路径体系规范.md](addp存储引擎路径体系规范.md)。

---

## 一、扩展步骤

1. 对只有厂商离线介质、或运行环境受 OS/CPU 架构限制的引擎，先通过 T5 官方介质认证确认许可证、介质完整性、原生启动、驱动协议和关键 SQL 能力；认证不得伪装成平台已经支持该引擎。
2. 在 `common/engine/plugins/<engine_type>/` 新建插件包。
3. 实现 `EnginePlugin` 基础接口；可由用户注册的引擎同时实现 `ConnectionSpecProvider`，并让默认端口、必填、敏感和身份字段接口从同一 `ConnectionSpec` 派生。
4. 按引擎能力实现需要的 provider。
5. 返回结构化 `Capabilities()`。
6. 按 `EngineOrigin()` 加入 `common/engine/plugins/builtin/general` 或 `common/engine/plugins/builtin/extension` 聚合包。
7. 补充单元测试和必要的 integration 测试。

官方介质事实由 `scripts/lib/<engine>-official-media.sh` 唯一持有版本、按架构下载地址和完整性校验值；Release owner 脚本持有认证专用启动参数、测试夹具、证据和清理逻辑，协议断言放在 `common/engine/certification/<engine>/`，不能用只有测试文件的插件目录冒充生产实现；workflow 只能调用 `make test-release RELEASE_SUITE=<suite>`。认证通过后仍必须建立独立 `engine_type` 和插件实现，不得因 wire protocol 兼容而复用其他数据库身份。

openGauss 的准入基线固定为 6.0.6 LTS 官方 Docker tar：Linux x86_64 Release 认证使用 `make test-release RELEASE_SUITE=opengauss-official-media`，Business 和常规 T2 使用同一固定 URL、SHA-256 与镜像。官方 6.0.6 Docker 构建包含 MOT，Docker Desktop for macOS 缺少其所需 NUMA 拓扑，所以 openGauss 实库门禁是显式登记的 hosted-only T2，不进入 macOS `make local-ci`；不得改用第三方镜像、跳过用例或覆盖系统配置制造本地绿色。Release suite 验证原生容器启动、PG 兼容 database、`lib/pq` 参数绑定、COPY、MERGE、复合 watermark、唯一键目录查询和查询取消。正式插件独立注册为 `engine_type=opengauss`，内部复用 PostgreSQL wire protocol 与 SQL 方言实现，但只显式暴露已验证的非空间目录、Facts、查询、批量/游标读取、COPY 写会话、建表、删除、bounded watermark 和幂等 upsert；不声明 PostGIS、CDC、分区变化应用或 PostgreSQL 扩展能力。

TiDB 的准入基线固定为 8.5.8，并且只使用 PingCAP 在 Docker Hub 公开发布的 `pingcap/pd`、`pingcap/tikv`、`pingcap/tidb` 三组件镜像；Business、Linux x86_64 GitHub Hosted T2 与 macOS Docker Desktop T2/T4 必须使用同一版本并按 OCI digest 固定，不使用 TiDB Enterprise 镜像、第三方重打包镜像或需要人工申请的介质。TiDB 源代码与官方镜像路线基于 Apache 2.0，可免 License 文件和激活用于无人值守开发测试；正式插件独立注册为 `engine_type=tidb`，内部复用 MySQL wire protocol、SQL 方言和已认证的 MySQL-compatible Provider，但只声明非空间目录、Facts、查询、BatchRead、bounded watermark、建表、删除、写会话和幂等 upsert。TiDB 普通只读 SQL 固定使用共享语句校验边界，bounded watermark 在 Snapshot Isolation 中只执行内部生成的 `SELECT`；不得启用只有 noop 语义的 `READ ONLY` 兼容开关。普通表 upsert 继续复用共享批处理和唯一键约束，但冲突值引用固定使用 [TiDB 官方支持的 `VALUES(column)`](https://docs.pingcap.com/tidb/stable/miscellaneous-functions/#values)，不得把 MySQL 8 insert row alias 当成全体 MySQL-compatible 引擎的共同语法。官方兼容性文档明确不支持 MySQL replication protocol 与空间类型/索引，因此不得推导 CDC、分区变化应用或空间能力。

KingbaseES 的准入基线固定为 V9R1C10 `V009R001C010B0004` Linux x86_64 官方 Docker tar，仓库内固定 SHA-256 `16a436608cc204349e510cb136b8fc1fcbdf6874aee7b204cdac20a3522282da`，并以官方 MD5 `26bb99891becc52f533488aac5fabfb6` 交叉校验来源。只开放 `DB_MODE=pg` 与 `engine_type=kingbase`；T5 必须证明 `KingbaseES V009R001C010`、`database_mode=pg`、`lib/pq`、参数绑定、COPY、`ON CONFLICT`、复合 watermark、唯一键目录查询和查询取消，并显式证明 openGauss `MERGE` 不在本方言路径。真实目录还必须证明 KingbaseES 系统 schema 以及 `public.sys_stat_statements`、`public.sys_stat_statements_all` 不进入业务枚举、leaf count 或直接路径解析。官方镜像内置 90 天试用 License 只允许一次性准入评估，不得通过新建容器重置试用期；长期 T5/T2/T4 仅在受保护的 owner-managed Linux x86_64 Runner 上使用 owner 正规获得、在有效期内且 SHA-256 明确的 License 文件运行。介质、镜像和 License 都不重分发；GitHub Hosted 与 macOS Docker Desktop 不是正式门禁路径。

第三个国产数据库的 T0 取舍固定为 TiDB：[PingCAP 微众银行案例](https://www.pingcap.com/case-study/webank-cuts-costs-scaling-tidb-petabytes/)公开了自 2019 年采用 TiDB、运行 80 余集群和 PB 级数据的金融生产实践，足以证明国内重点行业采用；[TiDB、PD、TiKV 开源仓库](https://github.com/pingcap)采用 Apache 2.0，官方 Docker Hub 三组件镜像可公开拉取并固定 OCI digest，且启动不需要 License 文件、激活或人工申请。[达梦官方行业案例](https://eco.dameng.com/cases/)覆盖银行、政务、能源等大量信创核心场景，市场采用更强，但 [DM8 官方试用说明](https://eco.dameng.com/document/dm/zh-cn/faq/faq-dm-product)明确默认试用期一年、到期后必须购买 License，否则数据库停止服务；[人大金仓官方一汽案例](https://www.kingbase.com.cn/explore/tech-blog/%E6%97%B6%E5%BA%8F%E6%95%B0%E6%8D%AE%E5%BA%93%E9%87%8D%E5%A1%91%E6%99%BA%E9%80%A0%E6%9C%AA%E6%9D%A5%EF%BC%9A%E9%87%91%E4%BB%93%E6%95%B0%E6%8D%AE%E5%BA%93%E5%BC%95%E9%A2%86%E5%B7%A5%E4%B8%9A%E4%BA%92/)说明其已在大型制造业生产环境规模化落地，但 [KingbaseES License 手册](https://help.kingbase.com.cn/v8/install-updata/license-information/license-information-2.html)明确非商用 License 为严格期限的临时授权，开发版也只有一年试用期。后两者不能满足长期、无人值守、免人工申请 License 的 GitHub Actions 基线，因此不进入本阶段实现，也不得以第三方镜像绕过授权条件。

第四个国产数据库的 T0 取舍固定为 KingbaseES V9R1C10：官方下载中心可公开获取精确 build 的 Linux x86_64 Docker tar 与 MD5，ADDP 已独立计算 SHA-256；官方 License 规则允许非商用或商用授权，但临时授权有严格有效期，商业授权通常绑定稳定主机并由厂商签发。这一路线的信创与重点行业代表性高于可全自动 Hosted 运行的开源候选，代价是 owner 必须维护一台受保护 Linux x86_64 Runner、正规 License、到期续授和介质缓存。不使用来历不明的第三方镜像，不公开归档介质或 License，不以 ephemeral Runner 或重建容器重置试用期。达梦 DM8 市场代表性同样强，但官方 Go 驱动、试用到期停服和自动化介质管理成本更高，本阶段不建立双轨实现。

聚合包是内置插件编译期登记的唯一手写清单。`make test-engine-plugin-registration` 会自动扫描调用 `plugin.Register` 的生产包，校验每个包恰好进入与 `EngineOrigin()` 一致的一个聚合入口，并禁止上层生产代码通过 blank import 直接加载具体插件。测试和 System API 不得另行维护全量插件类型清单或固定数量；它们应从已登记插件和 descriptor 动态验证通用契约，具体引擎的端口、能力和字段语义由各插件包自己的测试拥有。

System 的引擎类型列表、注册表单、默认值和校验规则均由 `GET /api/v1/system/engine-types` 返回的插件描述驱动。新增引擎时不得在 `system/frontend` 或 `common-frontend` 增加 `engine_type` 判断；若现有 `ConnectionFieldSpec` 无法表达所需交互，应先扩展通用描述协议和共享渲染器，再实现具体插件。

SQL 引擎必须通过 `SQLDialectProvider.SQLDialect()` 声明稳定方言，`SQLQueryRuntimeProvider` 组合该接口和 SQL 执行能力；通用查询生成、标识符引用、分页和参数占位符只消费该方言。新增 MySQL 协议兼容数据库时仍使用独立 `engine_type`，不得为了复用驱动而登记为 `mysql`，也不得在 common 或上层模块增加新的 `engine_type` 方言分支。

上层模块通过 `common/engine/plugins/builtin/general`、`common/engine/plugins/builtin/extension` 或 `common/engine/plugins/builtin/all` 统一加载内置插件，不应散落 blank import 具体引擎插件包。`common/dbbridge` 只消费聚合后的插件注册表。

---

## 二、接口选择

| 引擎类型 | 必选接口 | 常用可选接口 |
| --- | --- | --- |
| 关系型 / SQL 表格型 | `EnginePlugin`、`ConnectionSpecProvider`、`EngineCatalogModelProvider`、`EngineCatalogProvider`、`EngineCatalogFactsProvider`、`SQLQueryRuntimeProvider` | `ConnectionPoolPlugin` |
| 动态 schema 记录集合型 | `EnginePlugin`、`ConnectionSpecProvider`、`EngineCatalogModelProvider`、`EngineCatalogProvider`、`EngineCatalogFactsProvider`、`QueryRuntimeProvider` | `DynamicSchemaSamplingProvider` |
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

建议至少执行：

```bash
make test-engine-plugin-registration
go test ./common/engine/plugin ./common/engine/plugins/...
go test -tags integration ./common/engine/plugin/integration -run '^TestPluginInterfaceImplementation'
go test ./system/backend/internal/service ./meta/backend/internal/service ./manager/backend/internal/service
git diff --check
```

涉及前端入口时补跑对应模块构建。

openGauss 的完成证据必须同时包含上述官方介质认证、常规 T2 disposable Provider 集成门禁和 `opengauss-consumer-flow` 跨模块 T4 验收；T4 固定在 GitHub Hosted Linux x86_64 disposable 部署执行，复用通用关系引擎消费链路断言，不能用协议认证替代模块能力验收。

TiDB 的完成证据必须同时包含固定三组件官方镜像 digest、Linux x86_64 GitHub Hosted 与 macOS Docker Desktop 均可执行的常规 T2 disposable Provider 集成门禁、Business 幂等样例，以及 macOS 专用 Runner 上 `tidb-consumer-flow` 的 Manager / Transfer / Develop / Service 跨模块 T4 与三组件清理零残留证据；T2 与 T4 复用同一个无卷 Compose，不得用 MySQL 门禁或单组件 SQL 连接冒充 TiDB 验收。T4 首次真实成功前只允许手工 `workflow_dispatch`，不得增加 schedule。

KingbaseES 的完成证据必须同时包含固定官方介质 SHA-256、owner 提供 License 文件的 SHA-256 与有效性校验、Linux x86_64 T5 官方介质协议认证、同环境真实 T2 disposable Provider 门禁、Business 幂等样例，以及 `kingbase-consumer-flow` 的 Manager / Transfer / Develop / Service 跨模块 T4 与容器零残留证据。三层门禁只在带 `self-hosted`、`Linux`、`X64`、`addp-kingbase` 标签的受保护 Runner 上执行，首次真实通过前只登记手工 `workflow_dispatch`，不得增加 schedule。
