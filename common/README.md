# Common Module

ADDP 平台的共享代码模块，提供各个微服务模块通用的工具和类型定义。

## 包说明

### 类型感知的表格写入

`engine/plugins/shared.EncodeTableWriteValue` 独占 JSON 值编码。PostgreSQL COPY session、批量 COPY、INSERT（含 upsert），以及 MySQL／TiDB／OceanBase 的 INSERT session 和 upsert，按 `FieldInfo.Type` 使用同一入口；MySQL 空间表中的非空间 JSON 列也使用该入口。`json` 字段中的 Go 数组、对象、数字和布尔值编码为 JSON 文本；`nil`、nil 字节切片和 nil `RawMessage` 为 SQL NULL，非 nil 的空数组／对象分别为 `[]`／`{}`，typed nil 数组／对象编码为 JSON `null`。`string`、`[]byte` 和 `json.RawMessage` 表达已编码 JSON，必须是完整合法的 JSON，不重复编码；JSON 字符串标量应包含 JSON 引号。非法 JSON 或不可 JSON 编码的 Go 值返回带字段定位的错误，不转换为普通字符串兜底。

编码只由声明的字段类型决定，不把普通文本、`bytes` 或原生 `array` 自动解释为 JSON。PostgreSQL 空间 `[]byte` 继续使用十六进制 EWKB，MySQL 空间值继续使用 WKB 与显式 SRID；普通 `bytes` 保持各引擎原生二进制语义。PostgreSQL 协议的 `bytes` 字段在进入 lib/pq 前将 `[]byte(nil)` 规范为 SQL NULL，非 nil 空切片仍为空 BYTEA；不能依赖驱动把 typed nil 切片视作 NULL。Session 的类型来自打开时的字段定义，upsert 类型来自 options，不由后续批次覆盖。验证入口为 `make test-go`、`make test-common-postgres`；真实 JSONB 回归由现有 Common PostgreSQL T2 门禁和 Release/T2 CI 执行；MySQL、OceanBase、TiDB 的 JSON、NULL、二进制及失败回滚回归分别由 `make test-common-mysql-data-protection`、`make test-common-oceanbase`、`make test-common-tidb` 自动发现，MySQL 同时验证空间混合表。

Oracle INSERT session 与分区变更 MERGE 同样使用共享 JSON 编码，保留原生 JSON、BLOB 与 SDO_GEOMETRY 语义。`make test-common-oracle-decimal` 默认使用一次性 Oracle Free 23 Slim 门禁验证 Decimal、两条 JSON 写入路径、SQL NULL／JSON null、非法输入回滚、分区位点原子性及非空间删除；`ORACLE_SPATIAL=1` 使用固定摘要的完整镜像，额外验证原生空间登记、索引与删除清理。两种配置均在同一 owned 容器内以合成源运行 Transfer 的真实 `ResourceDeleteProvider → PrepareTableWrite → TableWriteSession` 组合回归，覆盖成功、空输入、定义拒绝、依赖阻止 DROP、JSON／主键失败及批次后的中止；成功验证 JSON、BLOB、几何类型及 SRID，核对重建后的字段、登记、索引和同级表保留。纯定义拒绝先于删除；DROP 与建表／Spatial 登记不属于后续行写入事务。新行回滚且无成功提交标记，不代表旧表恢复；已重建的空表、登记和索引可能保留。`records_written` 是写入进度，不能据其推断失败后的提交行数或目标无副作用。容器只使用随机回环端口，退出核对测试对象和容器清理；不连接业务实例、不运行已保存的 Transfer 任务，也不代表任务 API、权限或扫描 T4 验收。

Doris session 与批量 INSERT 使用共享 JSON 编码，JSON 字段沿用现有 `STRING` 建表映射，普通文本与 bytes 不解释为 JSON。声明为 `bytes` 的值要求 `[]byte`，在 Provider 内绑定十六进制文本并通过 [UNHEX](https://doris.apache.org/docs/2.1/sql-manual/sql-functions/scalar-functions/string-functions/unhex/) 还原原始字节，避免 MySQL 驱动生成 Doris 不支持的 `_binary` 字面量；NULL、空 bytes 与非 UTF-8 字节分别保留。当前批次在第一条 SQL 前完成全部编码，避免后续 chunk 的非法 JSON 导致前半批先提交；chunk 仍自动提交，`Abort` 不撤销已有写入，数据库执行失败不保证整批原子性。编码失败不新增当前批次数据，也不代表此前建表或 replace 已回滚。`make test-common-doris-decimal` 沿用已有一次性 Doris 门禁与 CI 登记，验证 Decimal、两条 JSON 写入入口、NULL、已编码内容与 bytes 保真、字段类型固定及跨 chunk 的编码失败无新增行。

ClickHouse session 与批量 INSERT 使用同一共享 JSON 编码，按打开时的字段类型写入现有 `String`／`Nullable(String)` 列；这不声明原生 JSON 列写入。bytes 的 `[]byte` 按原始二进制字符串绑定，避免驱动把字节切片解释为 SQL 数组；NULL、空 bytes 与非 UTF-8 字节分别保留。整个当前批次在第一条 SQL 前完成编码校验，非法 JSON 不新增本批数据；chunk 自动提交，数据库执行错误或 `Abort` 不保证撤销已有写入，也不撤销建表／replace。`make test-common-clickhouse-decimal` 沿用已有 Hosted 一次性容器与 CI 登记，覆盖 Decimal、session／batch 的 JSON 和 bytes 保真、既有目标结构复核、字段类型固定及跨 chunk 编码失败无新增行。本地 T1 的 SQLite 夹具只观测参数绑定，真实 ClickHouse 验证由该 Hosted T2 入口完成。

达梦的 `json` 表格写入沿用 `CLOB`，INSERT session 与 MERGE upsert 共用 `plugins/shared` 的 JSON 编码契约，在当前批次第一条写入 SQL 前校验全部行；编码后的 SQL NULL 遵守非空约束。原生 array／空间类型仍拒绝，bytes 保留 BLOB 绑定。`make test-common-dameng` 在固定官方介质与驱动的 Linux ARM64 Docker 进程中验证真实 CLOB/BLOB、幂等 MERGE 和 replace Abort 回滚，合成表逐个清理并核验零残留；宿主无官方驱动时拒绝数据面操作。该入口不登记 Hosted，也不证明生产 License 或正式部署认证。

openGauss 的独立原生 MERGE 与 PostgreSQL COPY／INSERT 共用 `postgresql.ProtocolTableWriteValue`，不再原样绑定容器。字段类型只来自 upsert options；JSON、SQL NULL／JSON null、一维 TEXT[] 及 BYTEA 沿用同一协议编码，整批编码与非空校验先于目标连接，错误含行和字段。批次元数据不补全或覆盖目标类型，空间目标仍拒绝。`make test-common-opengauss` 的既有 Hosted-only Linux x86_64 官方介质门禁自动发现 COPY、INSERT session 与 MERGE 的真实 JSONB／TEXT[]／BYTEA 回归，覆盖大数、64 KiB 文本、NULL、幂等 MERGE 和失败批次无新增行；本地只用标准 PostgreSQL 类型写入门禁复核被复用编码，不将其计为真实 openGauss 验证。

MySQL 可通过相同测试连接条件运行 `bash scripts/test/common-mysql-data-protection-gate.sh --test typed-write`，仅验证类型感知写入；默认入口仍运行完整门禁。

PostgreSQL 原生 `array` 保持 `TEXT[]`，支持一维字符串／NULL 元素，nil 容器为 SQL NULL、非 nil 空容器为空数组。Go 切片／数组及 PG 读取返回的数组文本在 Provider 内统一校验和编码；拒绝对象、嵌套数组、数字、布尔、非法 UTF-8／NUL，不隐式字符串化或改成 JSON。明确的非字符串 `ElementType` 在 DDL 前拒绝；未声明时逐值检查，不能据此证明源结构已知。PG 原生读取保留声明的数组元素类型，包含带长度／精度的数组，防止数组文本丢失类型证据；已有目标列严格匹配 `text[]`。

`TableWriteDefinitionChecker` 提供不接收连接或业务行的纯定义检查；openGauss 与 KingbaseES 转发同一 PostgreSQL 协议物理检查并拒绝 geometry／空间声明，Transfer 的既有只读配置预检与删除前校验自动消费，无引擎名单或权限变更。PostgreSQL 将物理类型映射、数组／decimal 声明、已知常量编码及既有目标列兼容规则用于同一报告。源字符串不保证是 JSON 文档，原生数组文本也不会因目标标注 `json` 自动转换；这些情况明确返回 `unknown`。真实 Prepare 复用该检查，Transfer 在 native target 的删除／准备之前拒绝确定不支持的目标定义。此检查不证明数据库权限、实时结构、扩展安装或逐行业务值合法，不能保证 replace 全流程回滚。`make test-transfer-postgres` 使用合成数组表及哨兵目标验证拒绝发生于删除之前，既有 Common PostgreSQL 门禁继续覆盖 JSONB／TEXT[] 编码。

PostgreSQL 协议与 MySQL-compatible 的纯定义检查共享有界常量审查：编码前递归排除自定义 JSON／Text 编码、driver.Valuer 及不透明对象，返回 `unknown` 且不执行回调；其他定义约束继续检查，真实写入值编码保持原有行为。回归由 `make test-go` 自动发现。

TiDB 与 OceanBase 通过同一 MySQL 非空间写入核心实现 `TableWriteDefinitionChecker`，Transfer 的现有配置预检和 replace 删除前校验直接消费。检查 JSON 常量、原生类型、decimal 上限和已有列兼容规则；拒绝原生 `array`，保留字符串／数组转 JSON 及缺少结构事实的 `unknown`，不自动改类型。Prepare、session 和 upsert 连接前复用同一确定性定义检查。T1 由 `make test-go` 自动发现，真实 JSON／bytes 和写入语法继续由已有 MySQL、TiDB、OceanBase T2 入口及 Release/T2 Job 验证，不新增测试入口。

只复验 PostgreSQL 类型感知写入时，在按 `scripts/infra/README.md` 核实并设置测试连接后运行 `bash scripts/test/common-postgres-gate.sh --test typed-write`，同时验证真实 JSONB 与 TEXT[] 的 COPY session、批量 COPY、INSERT、upsert。该选择仍使用同一门禁的测试库保护和清理，不能计为完整 Common PostgreSQL 门禁通过。批量 COPY 仍按 chunk 提交；失败 chunk 回滚不撤销此前已提交的 chunk，写入失败也不代表建表或 replace 已回滚。

MySQL 自身的纯定义检查覆盖空间混合表：普通字段复用共享核心，geometry 使用插件自身规则；声明冲突、原生数组、非法 decimal 与不匹配的空间子类型在目标操作前拒绝。目标快照缺少独立列 SRID 事实时返回 `unknown`。回归由 `make test-module MODULE=common GO_ONLY=1`、`make test-module MODULE=transfer GO_ONLY=1` 以及已有 `make test-common-mysql-data-protection` 自动发现。

### client
提供与其他服务交互的客户端：
- `SystemClient`: 与 System 模块交互的客户端，用于获取资源配置、用户认证等

`client` 只表达跨服务 HTTP/API 调用边界，不作为 infra PostgreSQL `common` schema 的读写入口。

`dataprotection/projectionstore` 是所有数据出口 Owner 的本地保护投影存储唯一实现。它在 Owner 自身 schema 内执行同一套版本化迁移和结构校验；新 Owner 只能复用该构造入口，不得复制投影表 DDL，也不在 `common` schema 建立中央保护表。

### jsonmap
decoded JSON map 的通用读取工具，用于读取嵌套 section、字符串、数字、时间等基础值。

`jsonmap` 不承载 `meta_item.attributes` 业务规范；attributes 标准分区、normalizer 和落库构造属于 Meta 模块。

### contentio
基于 Go `io` 之上的内容定位和读写抽象，提供 `Ref`、`Reader`、`Writer`、`Lister`、`RangeReader` 和 `Stat`。

`contentio` 不依赖 engine，也不解析格式；多 content 的组织规则属于 `common/format`、`common/dataitem` 或调用编排层，engine 到 contentio 的适配放在 `common/engine/contentadapter`。

### dataitem/authorizationfacts
授权资源事实的轻量 wire 契约、严格校验与边界摘要。独立于 DataItem 识别器和格式实现，供 HTTP Client、Meta、System 与 Catalog 使用；不访问源资源、不持有 Grant，也不执行权限裁决。

### resourcetree
把 Meta 已落库的 Engine Catalog / item 事实投影为跨模块资源树视图，并提供 `ResourceLocator` / provider `EngineCatalogPath` 的纯转换能力。

`resourcetree` 不持有 System / Meta client，不主动读取远程服务，不处理租户权限、token、降级策略、扫描或内容读取；上层模块自行获取 System / Meta 数据后，再调用资源树构建和路径转换能力。

`resourcetree` 中 attributes helper 只服务 `TreeNode.Metadata` 展示摘要，不作为通用 attributes 规范 API，也不写入持久 attributes。

### query
查询文本通用能力，包括 SQL / Cypher / MQL 参数绑定、SQL 副作用分析，以及跨 SQL 引擎的标识符引用、基础 SELECT / COUNT 和分页 SQL 生成。

参数引用分析识别 `es_dsl` 为无参数查询语言，返回空引用集；统一定义校验拒绝非空参数定义，ES Provider 继续负责 JSON、只读子集和执行选项校验，不新增 ES 参数绑定能力。

`query` 不承载 Engine Catalog facts 探测或 PostGIS 等特定引擎扩展函数；PostGIS 空间表达式属于 `spatial`。

### spatial
空间数据通用能力，包括 CRS、MVT、WKB、坐标转换和 PostGIS 空间 SQL 表达式。

### format
文件格式、类型信息、格式信息、字段类型映射、parser / extractor / analyzer 等通用能力。

`format` 不直接决定 meta item 如何归并，也不绕过 Meta normalizer 写最终 attributes。

### execution
统一执行记录能力，负责 `common.task_executions` 的模型、仓储、统计查询和 SQL 迁移入口。

所有模块写入或查询统一执行记录时应复用 `common/execution`，不得在模块内新增私有执行历史主表或绕过仓储直接写 `common.task_executions`。

后续如果确实新增 `common` schema 共享表，应按领域新增 `common/<domain>` 包，并在领域包内提供模型、仓储和 `EnsureStore`；只有当 `common` schema 独立成远程 Common 服务时，才新增对应 `common/client`。

### taskprovider
TaskProvider 标准契约的纯解析和校验能力，包括 `task.capabilities/v2`、具体任务 `execution_contract`、执行参数实例校验与标准任务列表响应 `{items,total,page,page_size}`。

`taskprovider` 不访问 System 注册表，不调用 owner 模块，不处理执行调度；System、Monitor、Orchestrator 等模块只复用它判断跨模块契约是否符合规范。

### models
共享的数据模型：
- `Resource`: 资源信息结构体
- `Engine`: 引擎配置模型，`ConnectionInfo` 是连接信息事实源

## 使用方法

在其他模块的 `go.mod` 中引用：

```go
require (
    github.com/yourusername/addp/common v0.0.0
)

replace github.com/yourusername/addp/common => ../common
```

在代码中导入：

```go
import (
    "github.com/yourusername/addp/common/client"
)

// 使用 SystemClient
sysClient := client.NewSystemClient("http://localhost:8180", token)
engine, err := sysClient.GetEngine(1)
```

## 设计原则

1. **单一职责**: 只包含真正通用的代码
2. **边界清晰**: 通用概念和工具可进入 common，Meta item 识别、claims / exclusive、`meta_item.full_name` 决策和 attributes 落库构造属于 Meta
3. **零依赖**: 尽量减少外部依赖，只使用 Go 标准库
4. **无需旧兼容**: 开发阶段不为旧包名、旧数据或旧逻辑保留兼容层
5. **文档完善**: 所有公开函数和类型都有文档注释
