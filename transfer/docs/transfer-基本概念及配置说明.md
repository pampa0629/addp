# Transfer 模块基本概念及配置说明

更新时间：2026-11-02

本文档定义 Transfer 稳定任务配置、执行状态、bounded snapshot 主链路、watermark bounded incremental 规则，以及 continuous/Kafka 契约与当前实现边界。旧版顶层 `mode`、`target.policy.write_mode`、`connector_type`、`source_config`、`target_config`、`output_format`、`file_type`、旧 endpoint `engine_id` 等字段不再兼容。

阅读导航：[配置维度与 Worker 分工](#21-顶层字段)、[table Transfer](#三table-transfer-支持范围)、[raw copy](#四raw-copy-支持范围)、[Continuous/Kafka](#八continuouskafka-v1-契约与当前实现边界)、[数据库 CDC](#九数据库-cdc-v1-已冻结边界)、[恢复分级](#恢复分级)、[测试与验收](#测试与验收导航)。

## 测试与验收导航

测试分层与标准入口遵循[测试与验收规范](../../docs/spec/addp测试与验收规范.md#三标准入口)，本文不维护另一套命令清单。

| 阅读目的 | 权威入口 |
| --- | --- |
| 模块、Go 定位、PostgreSQL 与 Hosted runner 回归 | [README 标准门禁与环境前提](../README.md#常规模块与专项门禁) |
| 格式集成与 Oracle CDC 专项 | [ArcGIS 门禁](../README.md#arcgis-开放格式专项)、[Oracle CDC 入口缺口与故障窗口](../README.md#oracle-cdc-专项入口缺口) |
| 固定版本、有效证据、失败与修复、清理及未覆盖项 | [分版本证据索引](../CLAUDE.md#专项约束与分版本证据) |
| 操作与结果核对 | [快速开始](快速开始.md#五开发验证) |

本文的配置、能力、快照或接口返回不作为当前 HEAD 的真实运行通过记录；未运行、失败、跳过和未覆盖项不计为通过。各章保留自身的权限与独立验收条件，历史结果不扩大为完整身份矩阵、数据面或公开入口已经验收。

## 一、核心对象

### 1.1 传输任务

任务表：`transfer.transfer_tasks`。

| 字段 | 说明 |
|---|---|
| `id` | 任务 ID。 |
| `tenant_id` | 租户 ID。 |
| `name` | 任务名称。 |
| `description` | 任务描述。 |
| `task_type` | 当前固定为 `sync`；执行主链路统一由 `config` 决定。 |
| `config` | source / target endpoint 任务配置。 |
| `schedule` | Cron 表达式；为空表示手动任务，授权与控制见[调度说明](#调度授权与控制)。 |
| `batch_size` | 批大小；config 内未声明时作为 planner 默认值。 |
| `enabled` | 定时任务是否启用。 |
| `auto_scan_metadata` | 成功后是否触发 Meta deep scan，默认 true。 |
| `status` | 任务实际状态：`idle`、`running`、`blocked`；`blocked` 当前用于数据库 CDC schema drift，审批成功后收敛为 `idle`。 |
| `progress` | 任务进度百分比。 |

任务状态只表达当前任务是否正在运行；成功或失败等结果在统一执行记录中查看。

### 1.2 执行记录

Transfer 执行记录使用统一表 `common.task_executions`。Transfer API 会将统一执行记录投影为模块 DTO。

可复用同步配置使用 `transfer.transfer_tasks`；Manager 数据导出、Develop 查询完整结果导出等一次性动作通过 `POST /api/v1/transfer/executions` 直接创建 bounded `sync` ad-hoc execution，`source_task_id` 为空。该入口不创建临时任务定义，调用方只持有统一 `execution_id`。模块 Runtime 使用 `GET /api/v1/transfer/executions/{execution_id}/result` 回查自己创建的一次性 execution；Transfer 从 Service Client 身份再次推导来源模块并与 execution `source` 精确匹配，其他来源一律按不存在处理。请求使用强类型 source / target / fields 契约，Transfer 在内部生成唯一 planner 配置并继续使用相同 worker、checkpoint、保护和输出主链路。查询 source 可在 `query.inputs[]` 提交本次执行已经解析完成的关系输入 `{name, locator}`；Transfer 只接受与 source locator 同 Engine 的标准 ResourceLocator，并使用这些输入生成完整执行血缘，不解析查询文本反推资源。Develop 的 CSV 查询导出必须按参数名稳定排序提交全部有效关系输入，并把冻结结果的有序列名提交为同名 `field_mapping` 投影；`target_type=unknown` 表示保留 Provider 返回值且不做类型转换，不能用有界预览样本猜测类型。

下表区分保存的执行事实与专业 HTTP 响应；公开字段以[Transfer 专业读取与响应](../../docs/spec/tasks/执行监控.md#transfer-专业执行读取与响应)为准，原始 checkpoint 和旧 `logs` 不作为专业响应字段返回。

| 字段或事实 | 口径与正文归属 |
|---|---|
| `records_read` / `records_written` | table Transfer 的行数指标；raw copy 第一版固定为 `1/1`。 |
| `bytes_read` / `bytes_written` | 字节指标；table Transfer 当前通常不作为主指标，raw copy 第一版会写入。 |
| `checkpoint_offset` / `checkpoint_state` | execution metadata 中的观测与 marker 持久事实；字段口径、生成与解释职责统一见[Checkpoint、进度和重试](#十checkpoint进度和重试)，不据此扩大 HTTP DTO。 |
| `error_details` / 安全 `metadata` | 专业响应仅保留已声明的安全错误与诊断投影，边界沿上述专业读取契约。 |

snapshot 重跑与 watermark incremental 的恢复分工见[恢复分级](#恢复分级)和[增量同步契约](#七watermark-bounded-incremental)；返回执行记录、字段统计或保存 marker 均不代表真实恢复已验收。

### 1.3 租户阶段的任务授权

#### 租户可见性与动作权限

Transfer 任务定义与执行记录在当前 Tenant 内共享。`transfer.task.read` 可以读取当前 Tenant 的任务列表、统计、详情和相应执行记录；创建者身份不额外赋予功能 Permission，也不限制有读取权限的其他租户成员。各动作的精确 Permission、用户任务定义与 TaskProvider 的身份分工统一见[任务表 API](tables/tasks表.md#五api)，各权限彼此不隐式包含。跨 Tenant 的任务 ID 按不存在处理。Department 与 Project Group 工作区及任务级分享在本阶段不生效，组织 Scope 不能扩大为 Tenant 权限；要启用组织 Scope，必须先完成任务归属、同范围列表裁剪及 owner 授权设计。

#### 资源选择与页面入口

创建向导的格式和 CDC 支持矩阵不包含任务或引擎实例，已认证 Tenant 用户可读取 `GET /capabilities`，不依赖 `transfer.task.read`。源与目标引擎选择使用 Meta 的引擎目录和资源树，调用者另需 `meta.catalog.read`；目录可见性不表示用户具有独立的数据资源授权。任务管理访问按 Tenant 归属和任务功能权限核验；个人、部门、项目组或逐资源的任务授权需在 owner 契约明确后另行设计。创建与启动是两个动作：没有 `transfer.task.execute` 时创建成功后不得自动启动；没有 `transfer.task.read` 时不得跳转到任务列表。内置角色和自定义角色应显式组合创建向导需要的跨模块权限。

Console 和 Transfer 页面按入口及动作权限显示：无任务权限时不提供任务入口，直达地址显示无权限状态；仅有创建权限的账号可进入创建向导；只读者可进入租户任务列表与详情但看不到写操作。API 对每次请求独立执行功能权限与 Tenant 校验，不能以隐藏按钮代替授权。

#### 调度授权与控制

带 Cron 的 bounded 任务在保存定义或恢复调度时，必须由同 Tenant 的本次 User 授权：除相应的创建／更新权限外，还要求 `transfer.task.execute` 和 `system.execution_authorization.create`。暂停清除本地调度授权，恢复重新授权，不按创建者补齐或沿用暂停前的主体，也不以 Service 身份代替。主体字段、定义摘要、原子更新、触发时重核及失败语义统一见[Transfer 定时任务授权](../../docs/spec/tasks/任务定义与授权.md#transfer-定时任务授权)；授权主体不是数据访问权，源读、目标写与运行期保护仍须独立核验。

bounded pause/resume 只控制未来调度，不中断已有 execution，也不执行 checkpoint 续写；无 Cron 的手动任务不支持这两个操作。用户控制与 continuous 运行控制的入口分工见[任务表执行与恢复边界](tables/tasks表.md#执行恢复与验收边界)，操作示例见[暂停与恢复定时调度](快速开始.md#暂停与恢复定时调度)。execution 的重跑与增量续读另按[恢复分级](#恢复分级)认定。

## 二、任务 Config JSON

`config` 必须包含 `source` 和 `target` endpoint；以下为 bounded snapshot 示例：

```json
{
  "runtime": {"boundary": "bounded"},
  "load": {"mode": "snapshot"},
  "source": {
    "locator": "addp://engine/1/path/public/roads?type=table",
    "data_type": "table",
    "representation": "native"
  },
  "target": {
    "parent_locator": "addp://engine/2/path/exports?type=directory",
    "name": "roads.csv",
    "data_type": "table",
    "representation": "encoded",
    "format": "csv",
    "options": {"header": true},
    "policy": {"apply_mode": "replace"}
  },
  "transforms": [],
  "batch_size": 10000
}
```

### 2.1 顶层字段

| 字段 | 必填 | 说明 |
|---|---:|---|
| `runtime` | 是 | 执行边界为 `boundary=bounded` 或 `boundary=continuous`；两类 Worker 的分工遵循[同步执行规范](../../docs/spec/tasks/Transfer同步执行.md#transfer-continuous-sync-v1-契约)。 |
| `load` | 是 | 装载方式为 `mode=snapshot` 或 `mode=incremental`；与执行边界、变化识别及源／目标能力的有效组合见[当前支持矩阵](transfer-任务语义与同步模式.md#三当前支持矩阵)。 |
| `source` | 是 | 源 endpoint。 |
| `target` | 是 | 目标 endpoint。 |
| `transforms` | 否 | table batch transform 列表。 |
| `batch_size` | 否 | 本任务批大小；为空时使用任务表 `batch_size`。 |

bounded execution 由 `transfer-bounded-worker` 通过 PostgreSQL claim 领取；continuous execution 由独立 continuous worker/supervisor 承担长期 session，不进入 bounded claim，也不使用 Asynq。进程、领取和租约职责分别沿[正式执行契约](../../docs/spec/tasks/Transfer同步执行.md#transfer-continuous-sync-v1-契约)与[Continuous 运行时](transfer-任务语义与同步模式.md#六continuous-运行时)。

上述维度不能任意组合，源／目标与非空间限制仍按支持矩阵及各专题校验。Kafka poll 分批读取不改变 continuous 边界，CDC initial snapshot 属于 continuous CDC 的 bootstrap，定义见[执行边界](transfer-任务语义与同步模式.md#21-执行边界)；`manifest` 尚未进入支持范围，见[变化识别方式](transfer-任务语义与同步模式.md#23-变化识别方式)。配置与进程分工不作为运行通过证据，标准入口和未覆盖项统一见[测试与验收导航](#测试与验收导航)。

### 2.2 Endpoint 字段

| 字段 | 必填 | 说明 |
|---|---:|---|
| `locator` | source 必填 | source 使用的 ResourceLocator URI，指向已存在资源。 |
| `parent_locator` | target 必填 | target 父 node 的 ResourceLocator URI，指向已存在 schema / directory / bucket / prefix 等父节点。 |
| `name` | target 必填 | 父 node 下待创建或待覆盖的目标资源名。 |
| `data_type` | 按任务形态 | table Transfer 使用 `table`；动态 schema collection 原始记录导出使用 `unknown`；raw copy 第一版支持 `document`、`media`、`cad`、`unknown`，其 target 省略规则见[目标继承与一致性](#raw-copy-目标继承与一致性)。 |
| `representation` | 是 | `native` 或 `encoded`。 |
| `format` | 按任务形态 | encoded endpoint 的格式；table 源格式与目标声明见[format 声明与来源](#format-声明与来源)，raw copy 的源端必填与目标继承见[目标继承与一致性](#raw-copy-目标继承与一致性)。 |
| `options` | 否 | 格式或读取写入选项。 |
| `policy` | target 必填 | 目标应用策略；必须声明 `apply_mode`，upsert 还必须声明 `keys`。 |
| `override_policy` | target 可选 | `existing_table_append` 表示允许 Orchestrator 用可选 `target_locator` 覆盖单次执行目标；只允许 bounded snapshot、已存在原生表和 append。 |

`locator` 示例：

| 类型 | 示例 |
|---|---|
| `table` | `addp://engine/1/path/public/roads?type=table` |
| `file` | `addp://engine/2/path/exports/roads.csv?type=file` |
| `object` | `addp://engine/3/path/bucket/exports/roads.csv?type=object` |
| 已入库 source item | `addp://engine/3/path/bucket/roads.shp?type=object&item_id=12` |

CSV／TSV 的表头选项见[CSV 与 TSV 表头](#csv-与-tsv-表头)；单资源 encoded source 的字段身份、Meta／Describe 比对及失败边界见[单资源结构核对](#单资源结构核对)。

### 2.3 Bounded query source 与轻量 ETL 构造

`bounded + native table` source 可以声明 `source.query`，由源引擎的 `QueryReadSessionProvider` 执行只读查询，Transfer 仍通过统一 table reader / writer 主链路分批搬运查询结果：

```json
{
  "source": {
    "locator": "addp://engine/11/path/sales/orders?type=table",
    "data_type": "table",
    "representation": "native",
    "query": {
      "language": "mql",
      "statement": "{\"aggregate\":\"orders\",\"pipeline\":[{\"$unwind\":{\"path\":\"$items\",\"includeArrayIndex\":\"items__index\",\"preserveNullAndEmptyArrays\":false}},{\"$project\":{\"_id\":\"$_id\",\"items__sku\":{\"$ifNull\":[\"$items.sku\",null]},\"items__index\":1}}]}"
    }
  }
}
```

MongoDB 控制台提供一个通用结构整形构建器，当前覆盖两类基数模型：

- 一条源文档生成一行：自动携带 Meta 识别出的文档标识，并选择需要进入关系行的文档字段。
- 一个数组元素生成一行：只能选择一个 Meta 识别出的数组字段，可以选择该数组下的多个元素叶子字段，也可以选择多个不位于任何数组下的父文档叶子字段随每个元素行重复携带；父文档标识自动携带，并可选输出数组序号。空数组和缺失数组固定生成零行。

构建器只是一种 MQL 编写方式，不是新的任务 DSL 或执行路径。保存到任务中的唯一事实仍是 `source.query.language=mql` 和标准 MQL command object；基础构建器只生成 `可选单次 $unwind -> $project`，不生成筛选和排序。编辑已有任务时，只有严格属于该子集、使用系统确定性查询输出名，且 collection、数组与投影路径仍与当前 Meta 事实一致的语句才反向显示为结构化表单；其他已有 MQL 统一只读展示，不提供高级 MQL 编辑或保存入口。字段候选来自 Meta 已扫描的字段路径和数组类型，数组字段禁止手工输入；构建器不得内置业务库、collection、字段名或目标 schema。

基础构建器不暴露 MQL 的 `$match`、`$sort`、`$ifNull`、投影别名和 `preserveNullAndEmptyArrays` 等实现细节，不提供递归自动摊平，不猜测多个数组之间的业务粒度，也不开放 `$group`、`$lookup`、`$unionWith` 等业务计算。父文档随行字段只是同一文档 `$project` 的上下文复制，不是关联、聚合或跨 Collection 读取。需要其他只读 MQL 能力时转到 Develop；指标、标准化、维度和事实加工仍属于 Develop。

查询执行前，Transfer 必须从将要打开读会话的同一 `PreparedQuery` 无条件取得 Provider `ReadSet`。上游声明 `source.query.inputs[]` 时，其 Locator 集合必须与 ReadSet 精确相等；Console 单源构建器不声明 `inputs` 时，ReadSet 必须精确只包含所选 source leaf。输入缺失、多出或错配都必须在打开游标前失败，不得仅在数据保护命中时才解析；执行血缘只能使用已通过该校验的输入。

原生表的 bounded 保护准备必须在打开读会话和任何目标变更前，同时核验计划携带的 Meta 字段快照与同一源 Provider 实时返回的完整 `FieldInfo`。先按精确 Locator 用本地投影校验计划字段，再由 Common 编译结果绑定的结构校验闭包核验实时字段；该闭包与输出保护使用同一份投影，只准备一次保护视图。未纳管源不绑定该闭包、不增加 Catalog 读取要求；不能把旧 Meta 字段或选中的输出字段当作受保护源的实时全表结构。受保护源的实时结构缺失或无法获取时拒绝执行，结构与投影不符时使用既有 `protection_schema_mismatch` 准备阶段原因；既有字段映射、空间事实和查询 PreparedQuery 路线保持各自契约。本检查不承诺结构描述与读取之间的并发 DDL 原子性。

结构整形与 PostgreSQL 字段映射的职责必须分离：结构整形只决定“一行代表什么”和“哪些 MongoDB 源字段进入查询结果”；编译器为查询结果自动生成确定性、无点号的内部字段名。下一步 `field_mapping` 只展示实际查询输出，不展示其余 MongoDB 原始字段，并负责 PostgreSQL 目标字段名、类型、可空性等目标定义。数组展开的父文档标识属于关系行必需来源，基础模式自动携带且不得从映射中删除；`activity_id` 等业务目标名只在 `field_mapping` 中声明。

关系型 native table 的 Console 基础 SQL 构造器只扩展 Transfer 的轻量 ETL 能力，不是第二个 SQL IDE：

- 源关系固定为用户已选择的单个 native table，基础界面只允许选择输出字段和配置一层“满足全部 / 满足任一”行过滤。
- 过滤操作符按 Meta 字段类型和 Engine capability 的 `parameters.types` 共同开放；基础集合为等于、不等于、为空、不为空、属于列表，可比较标量另支持大于、大于等于、小于和小于等于。不对 geometry、JSON 结构或其他无稳定跨方言谓词的类型开放值过滤，也不得为未声明的参数类型提供值过滤。
- 所有过滤值必须生成稳定命名参数 `:p1`、`:p2` 并写入 `source.query.parameters`，不得将值拼入 SQL。`int` 使用 `integer`（声明 `number` 也可承载整数值），`float/double` 使用 `number`，布尔值使用 `boolean`，普通文本及日期时间使用 `string`；`bigint/decimal` 必须使用十进制字符串通过 `string` 无损传递，禁止经过 JavaScript Number。schema、table 和 column 的引号必须消费 Engine capability 的 `identifier_quotes.sql`，不按 `engine_type` 猜测。
- 基础构造器不提供 Join、多表输入、聚合、计算字段、输出别名、排序、Limit 或方言私有函数。目标字段名和类型继续由后续 `field_mapping` 配置；复杂 SQL 加工归 Develop。
- 基础界面直接以已选字段生成确定输出字段，下一步 `field_mapping` 只消费该输出。仅当 SQL 严格属于上述可逆子集时才能反向显示并编辑为基础表单；其他合法只读 SQL 在 Transfer Console 中只读展示，不提供高级 SQL 编辑或保存入口。

查询入口必须由所选 Engine Instance 的 `compute.query` 能力驱动：只有 `supported=true`、`read_session=true` 且 `languages` 非空时才开放。单语言引擎直接使用 `default_language` 或唯一语言，不显示无意义的语言下拉框；仅当引擎真实声明多种语言时才允许在声明集合内切换。查询包含参数时，Planner 还必须失败关闭地校验 `parameters.supported`、`parameters.languages` 与 `parameters.types`，不能只依赖前端。切换 Engine 或源资源必须清理不兼容的查询语句、参数和基础构造状态，不得把 MQL 带入 SQL 引擎或反之。

### 2.4 单次执行目标覆盖

目标覆盖与 `source.query` 完全正交。任务必须先选择并保存一个已存在原生表作为默认目标，并使用 `policy.apply_mode=append`；随后才可以声明 `override_policy=existing_table_append`。该任务即使不经过 Orchestrator 也必须能直接执行。

允许目标覆盖的 TaskProvider 契约将 `target_locator` 声明为可选 ResourceLocator 输入，并在 `input_defaults` 返回默认目标。执行时未提供该参数则使用保存的默认目标；只有带父编排 execution 的 Orchestrator 调用可以提交覆盖值。覆盖只修改冻结到本 execution 的有效目标，不修改任务定义。默认目标和覆盖目标都通过 existing-table write session 写入，不执行 prepare、DDL、replace、truncate 或 delete；continuous、watermark 和 encoded target 不开放该能力。

## 三、table Transfer 支持范围

阅读导航：[Endpoint 组合](#31-endpoint-组合)、[动态记录导出](#32-动态记录原始导出)、[格式](#33-格式)、[原生目标](#34-native-table-target)、[空间字段与 CRS](#35-空间字段与-crs-边界)。

### 3.1 Endpoint 组合

| source | target | 状态 |
|---|---|---|
| native table | native table | 已接入统一 table reader / writer 链路。 |
| native table | encoded file/object | 已接入。 |
| encoded file/object | native table | 已接入。 |
| encoded file/object | encoded file/object | 已接入。 |
| encoded whole scope | encoded / native table target | Parquet dataset 已接入。 |
| encoded multi refs | encoded / native table target | Shapefile 已接入。 |
| native dynamic records | encoded file/object | 已接入 `mongodb_extended_jsonl` 原始记录导出；不经过 table pipeline。 |

Transfer 不为 PostgreSQL -> PostgreSQL、NFS -> MinIO、MinIO -> NFS 等具体引擎组合维护专用链路。

### 3.2 动态记录原始导出

动态 schema collection 的原始记录导出使用 `source representation=native + data_type=unknown` 到 `target representation=encoded + format=mongodb_extended_jsonl` 的唯一任务形态。Transfer 消费源引擎 `EncodedRecordReadSessionProvider` 返回的类型保真批次，再通过目标 `ContentWritableProvider` 流式写出；不得复用 `raw copy`，因为源 collection 没有可直接复制的单一内容字节流。

MongoDB 第一版固定输出 Canonical Extended JSON v2、UTF-8、每行一个紧凑文档，目标扩展名为 `.ejsonl`。该格式用于记录交换，不是 BSON archive 或数据库备份。任务不接受 transforms、query、筛选、排序或用户可选编码模式；表格化导出后续复用 query source 到 encoded table target 的独立主路径。

### 3.3 格式

[format 声明与来源](#format-声明与来源) · [读取布局与入口](#读取布局与入口) · [CSV／TSV 表头](#csv-与-tsv-表头) · [单资源结构核对](#单资源结构核对) · [Parquet 压缩](#parquet-列式压缩) · [空间字段与 CRS](#35-空间字段与-crs-边界) · [写后扫描](#十一写后-meta-扫描)

| 格式 | 支持形态 |
|---|---|
| CSV / TSV | single table read / write。 |
| JSON / JSONL | single table read / write；JSONL 是 `json` 的用户侧编码变体。 |
| GeoJSON | single table read / write；独立 `format=geojson`，空间事实由解析结果表达为 `capabilities.spatial`。 |
| Parquet | single table read / write；whole scope dataset read；支持 field_selection 下推；普通 Parquet 与 GeoParquet 共用列式压缩配置。 |
| Shapefile | multi table read / write；完整 refs 由 format specs 或 Meta attributes 提供。 |

#### format 声明与来源

encoded table source 使用已解析 Meta item 的格式事实；显式 `source.format` 与该事实冲突时拒绝。没有 Meta 格式事实时，必须声明 `source.format`，不能只凭路径扩展名补齐。已入库 item 的事实与消费边界见[再次消费规范](../../docs/spec/addp数据项探测器规范.md#已入库-item-的再次消费)。

encoded table target 必须显式声明 `target.format`，并具备对应格式的写入能力，不从源格式继承。动态记录原始导出的固定目标格式见[对应契约](#32-动态记录原始导出)；raw copy 的省略继承与一致性单独见[目标继承与一致性](#raw-copy-目标继承与一致性)，不能推广到 table 目标。

#### 读取布局与入口

已入库 encoded table source 使用 Meta 确定的 item layout；身份、primary content、whole scope 和 refs 的权威定义见[已入库 item 的再次消费](../../docs/spec/addp数据项探测器规范.md#已入库-item-的再次消费)，持久字段归属见[attributes 写入规则](../../docs/spec/addp元数据attributes规范.md#写入规则)。

| 布局 | 本节典型来源与消费范围 | 读取入口 |
| --- | --- | --- |
| `single` | 单个文件／对象，例如单个 Parquet 文件。 | `TableReaderProvider`。 |
| `whole` | Meta 已确定的 Parquet 目录数据集，使用 whole scope 根范围。 | `ScopeTableReaderProvider`。 |
| `multi` | Shapefile primary content 与完整 `attributes.item.refs`；按格式声明核验必需成员。 | `MultiTableReaderProvider`。 |

不能仅因格式注册了 scope reader 就把单文件当目录读取。PGeo 等 container child 没有普通 content table reader，仍消费运行时工厂已绑定的 scope reader；不把父容器的 single layout 改写为 whole。

multi 的 primary 路径不能替代完整 refs；缺失或不完整时回到节点重扫，由 detector 重新裁决，不能在格式 reader 内枚举兄弟文件补齐。whole 的 refs 只保存 manifest／关键入口，不用于反推全部数据集成员；消费边界沿上述探测器规范，不扩大为普通父目录。

#### CSV 与 TSV 表头

CSV / TSV encoded source 沿用既有 `source.options.header` 布尔选项，不新增 `has_header` 任务参数。有效的显式任务选项优先；未显式设置时，继承 Meta `type_info.table.native.has_header`，两者均无声明才默认 `true`。Meta 表头声明必须是布尔值，显式 `false` 不得丢失。任务读取选项只影响本次读取，不回写 Meta，也不按首行内容猜测表头。

#### 单资源结构核对

单资源 encoded source 打开真实格式 reader 后，必须在目标准备、删除、创建或写入前核对实际有序字段名与本次 Meta／Describe 声明。reader 在打开时已有字段的，直接核对该字段结构；JSON／GeoJSON 等动态 reader 尚未提供字段时，使用同一格式 InfoProvider 对当前源执行 Describe 后核对，本次打开流程已经取得的 Describe 结果直接复用，不能将“尚无字段”当作空表或跳过校验。已有 Meta 声明的动态格式因此多读取一次源文件；这不提供不可变源快照。启用字段选择时，两边按同一选择投影后比较；字段数量、名称或顺序冲突即拒绝并关闭源资源，不能用缓存声明掩盖当前源字段，也不能依靠字段映射默认值补空后继续写。该检查只核对字段身份；共享 CSV reader 的字符串字段描述不覆盖 Meta 已推断的 bigint、精度等丰富类型事实。没有 Meta 声明时继续沿既有 Describe／reader 取得结构，不另建推断路径；reader 尚无字段且无 InfoProvider 可核验时明确拒绝。

#### Parquet 列式压缩

Parquet writer 使用 `target.options.compression`；默认值、允许 codec、整列统一配置、压缩级别及非 canonical 别名的拒绝规则，统一见[列式压缩能力规范](../../docs/spec/addp数据类型与格式能力规范.md#列式压缩能力)。该选项不是文件封装压缩或 Parquet value encoding。

普通 Parquet 和 GeoParquet 使用同一份契约；当源数据含空间事实时，压缩后的 WKB 几何列和 `geo` metadata 一起写出。

`GET /api/v1/transfer/capabilities` 的 `table_formats[].columnar_compression` 是唯一能力事实源，包含 `default` 和 canonical `codecs`。控制台必须消费该声明，不按格式名硬编码列表。

```json
{
  "target": {
    "format": "parquet",
    "options": {
      "compression": "zstd"
    }
  }
}
```

布局、格式能力声明和 refs 完整性不授予读取或写入权限；任务权限、目录选择和当前 Tenant 范围见[租户阶段的任务授权](#13-租户阶段的任务授权)，源读、目标写及运行期保护仍独立核验。格式表、静态映射或配置预检不等于真实数据与完整链路验收通过。

### 3.4 Native table target

[运行期定义检查](#运行期定义检查) · [请求与权限](#预检请求权限与读取范围) · [报告与三态](#报告与三态结果) · [快照与未检查项](#快照覆盖与未检查项) · [目标能力](#目标能力与写入策略) · [摘要与输出](#摘要绑定与输出保护) · [Agent 复核](#agent-tool-与创建复核) · [写侧范围](#已接入的原生目标写侧)

#### 运行期定义检查

执行器在删除／准备目标之前调用 Provider 可选的纯定义检查，拒绝确定不支持的字段声明。PostgreSQL 已复用实际类型映射和结构兼容规则；缺少事实或需要读取实际值才能判断的情况为 `unknown`。这不是行值预检或执行授权，不能据此证明整个 replace 无副作用，仍须通过运行期授权、Security、实时结构及逐值编码检查。

#### 预检请求、权限与读取范围

`POST /api/v1/transfer/task-config-checks` 接收与创建任务相同的**完整请求**，供 Console 与 Agent 共用。首期支持 bounded + snapshot、native table、replace/append；不检查 upsert。当前租户用户必须同时具备 `transfer.task.create`、`meta.catalog.read`、`system.engine.read`，委托还须使用精确 transfer audience 和唯一 `transfer.task.config.check` Scope。Service Principal 和 Resource Access Ticket 不能调用此用户入口。此接口不会保存任务、安装调度、扫描、执行查询、读取业务行、建表或删除目标；原创建／更新接口的权限和行为不因此改变。

Agent Tool 与 SDK 只接入上述 HTTP 契约；`transfer.task.config.check` 的输入 Schema、SDK 映射与权限声明统一见 [Tool 契约](../../docs/spec/addp智能体Tool开放规范.md#34-当前-tool-集合)。System 签发与 Transfer 委托路由均按上述三项 Permission 执行 all-of 校验；完成当前主体校验后，由 Transfer 使用已有 Tenant Service 客户端读取租户绑定的控制面事实，不转发委托 token 给 Meta/System，不增加权限。

#### 报告与三态结果

HTTP 200 返回 `addp.transfer_task_config_check/v1` 报告，`status` 为 `supported / unsupported / unknown`；字段诊断含 `code`、`check`、`config_path`、`source_field / target_field` 及 Provider 物理类型。`checked` 记录实际检查，`not_checked` 与 `runtime_checks_required` 列出剩余门禁。

HTTP 200 只表示成功取得报告，不代表 `supported`；三态对创建的影响见[Agent Tool 与创建复核](#agent-tool-与创建复核)，依赖错误仍按下节失败语义处理。

#### 快照覆盖与未检查项

`facts[].schema_coverage` 是完整／采样／未知的**已落库快照**覆盖度，不是实时结构保证。

查询不执行；可选 Common `QueryDefinitionChecker` 只用已保存快照分析部分输出。MongoDB 首期单集合、单个简单 `$project` 支持包含、排除和字段别名，覆盖度仍为 sampled／unknown；复杂查询、采样、缺失快照、无 Provider Checker 均保留 unknown。查询前原表结构不能充当输出结构，未观察字段不能证明缺失或确定 NULL／默认值。

即使源未知，仍由目标 Provider 检查显式目标约束及常量；unsupported 优先于 unknown。缺少 Meta 快照或 Meta 404 均不能证明物理目标不存在。已知数字默认值经过运行期 float64 配置解析的精度未证明，明确返回 unknown。

依赖服务的权限、网络或无效响应返回 502，不作为 unknown 吞掉。

#### 目标能力与写入策略

原生目标能力按 `apply_mode` 校验：append 只要求已登记的写入能力；replace 还要求实例已登记 `storage.store.delete`，且当前插件声明该能力并实现 `ResourceDeleteProvider`。已知缺少删除能力即返回 unsupported，诊断路径为 `/config/target/policy/apply_mode`；缺少事实而无确定反证时返回 unknown。源读取不要求删除能力。目标应用策略统一见[写入策略](#六写入策略)；实时目标状态与覆盖副作用仍须按下节要求重新验证。

#### 摘要绑定与输出保护

规范 JSON 使用对象键递归排序、保留数组顺序和数字 token、不增加空白的 UTF-8 JSON（与 Go encoding/json 的字符转义一致）。`config_hash` 是完整提交请求的规范 JSON SHA-256；`facts` 摘要绑定引擎版本、已登记能力和完整 Meta 结构（含字段顺序、原生／元素类型、精度、默认表达式、空间事实、身份与扫描时刻）；`binding_hash` 是将自身置空后对完整报告规范 JSON 求 SHA-256。它们是内容绑定，**不是签名或执行授权凭据**。报告不回传配置、SQL、默认值正文、凭据或样本，响应禁止缓存。结构快照时效、实时连接／结构、数据库权限、Security、源读取 Grant 和逐值转换均须运行时重新验证；replace 的目标副作用也不由此报告证明安全。

#### Agent Tool 与创建复核

Agent Runtime 在最终配置复核及创建前调用正式预检；unsupported 阻止创建，unknown 展示未检查项与运行要求后知情确认，只保存未运行定义。supported 不保证执行成功，报告也不改变创建与执行接口自身的权限和配置校验。

创建复核的原参数、稳定报告绑定、重检及确认失效规则统一见[Transfer 创建条件与复核](../../docs/spec/addp本体定义与发布规范.md#九transfer-创建条件与复核)。上述接入与预检报告不代表真实运行已验收，历史结果及未覆盖项见[预检验收记录](../CLAUDE.md#bounded-失败诊断与配置预检)。

#### 已接入的原生目标写侧

| 引擎 | 当前写侧 |
|---|---|
| PostgreSQL | create / ensure table、schema evolution、COPY session、batch insert、空间字段写入。 |
| MySQL | create database/table、安全缺失字段追加、事务内批量 insert。 |
| Doris | DUPLICATE KEY 明细模型、MySQL 协议批量 insert；空间字段暂拒绝。 |
| ClickHouse | MergeTree 普通表、批量 insert；生成列写入跳过，空间字段暂拒绝。 |

### 3.5 空间字段与 CRS 边界

Transfer 在 table 链路中只传递空间事实，不提供通用 CRS transform 能力。

规则：

- 表结构中的空间字段由 `datatype.FieldInfo` 表达。
- 空间字段、几何类型、SRID / CRS、dimension、extent 等横切事实由 `datatype.SpatialInfo` 表达。
- encoded Shapefile / GeoJSON 等空间源写入 native table target 时，Transfer 将 `SpatialInfo` 传给目标 writer / preparer，用于创建 geometry column、typmod、SRID 等目标结构事实。
- `ewkb` 行值可以携带 SRID，但不能替代 `SpatialInfo` 中的 schema 级 CRS 事实。
- Transfer 不在普通 table copy / import / export 链路中隐式执行 CRS transform。
- 批量 CRS transform 属于计算 / ETL 能力，应由 PostGIS、Python/Spark 工作流或后续明确的空间转换算子承担。

Shapefile 写出 `.prj` 时，`WriteOptions.ExtraParams["crs_definition"]` 必须是 CRS 定义文本，例如 WKT、ESRI WKT 或 proj4 文本；不得传入裸 `EPSG:<code>`。CRS ID 应进入 `SpatialInfo` / `capabilities.spatial.crs_ref`，定义文本应进入 `crs_definitions[].definition`。

## 四、raw copy 支持范围

raw copy 是 non-table encoded single content 的原始字节复制。它不调用 `common/format` 的 table reader / writer，不解析文档、不抽取媒体元数据，也不做格式转换。

第一版只支持以下 endpoint：

| 维度 | 支持范围 |
|---|---|
| source / target `data_type` | `document`、`media`、`cad`、`unknown` |
| source / target `representation` | `encoded` |
| source locator `type` | `file`、`object` |
| target `parent_locator` type | `directory`、`root`、`bucket`、`prefix`、`service` |
| source format layout | `single` |
| target `data_type` / `format` | 省略继承与显式声明统一见[目标继承与一致性](#raw-copy-目标继承与一致性)。 |
| target path | 必须是完整 file / object 路径 |
| target `policy.apply_mode` | 只支持 `replace` |

### raw copy 目标继承与一致性

源端必须声明 `source.data_type` 和 `source.format`。目标的 `data_type`、`format` 可分别省略，由 planner 从源端补齐后校验；显式声明的值必须与源一致，冲突即拒绝。继承只适用于这两个身份字段；目标 `representation`、`parent_locator`、`name` 和写入策略不从源端自动补齐，仍须按 Endpoint 与本节要求校验。

继承规则不扩大上表的路径、布局、类型和写入策略支持范围。继承成功或规划通过不等于真实读写、目标内容与权限已经验收，标准入口及未覆盖项沿[测试与验收导航](#测试与验收导航)。

配置示例（目标显式声明与源一致的类型和格式）：

```json
{
  "runtime": {"boundary": "bounded"},
  "load": {"mode": "snapshot"},
  "source": {
    "locator": "addp://engine/1/path/raw/docs/report.pdf?type=object",
    "data_type": "document",
    "representation": "encoded",
    "format": "pdf"
  },
  "target": {
    "parent_locator": "addp://engine/2/path/archive?type=directory",
    "name": "report.pdf",
    "data_type": "document",
    "representation": "encoded",
    "format": "pdf",
    "policy": {"apply_mode": "replace"}
  },
  "transforms": [],
  "batch_size": 1
}
```

## 五、field_mapping transform

字段映射写入 `config.transforms[]`：

```json
{
  "type": "field_mapping",
  "version": "v1",
  "mode": "project",
  "fields": [
    {"source": "amount", "target": "amount", "target_type": "decimal", "precision": 18, "scale": 4},
    {"source": "name", "target": "road_name", "target_type": "string"},
    {"source": "geom", "target": "geometry", "target_type": "geometry", "nullable": false},
    {"target": "created_by", "target_type": "string", "default": "transfer"}
  ]
}
```

| 字段 | 必填 | 说明 |
|---|---:|---|
| `source` | 否 | 源字段名；为空表示常量 / 默认字段。 |
| `target` | 是 | 目标字段名。 |
| `target_type` | 否 | 目标字段类型。 |
| `precision` | 否 | decimal 目标的总有效位数；必须为正整数。 |
| `scale` | 否 | decimal 目标的小数位数；必须为非负整数且不大于 `precision`。 |
| `nullable` | 否 | 是否可空；默认 true。 |
| `default` | 否 | 源字段缺失或值为 nil 时使用的默认值。 |
| `format` | 否 | 日期、时间、数字等简单解析 / 格式化提示。 |

`precision` / `scale` 只属于 decimal 目标字段，两者必须同时出现或同时省略。源字段已声明有限精度时，向导默认继承该事实；无界 decimal 写入声明 `limits.table_write.decimal.requires_explicit_precision_scale=true` 的目标时，必须在映射中显式填写。MySQL、MySQL 模式 OceanBase 与 TiDB 声明 `max_precision=65`、`max_scale=30`，Oracle、Doris 与 ClickHouse 当前 Provider 声明 `max_precision=38`、`max_scale=38`，并统一要求 `scale <= precision`；向导校验与字段定义推荐统一消费目标 Engine Instance 的 capability，不按数据库名称建立分支，推荐 API 只接收所选 `target_engine_id`，不接受调用方另报引擎类型。系统不会把无界 decimal 静默收缩为固定默认精度；只有用户触发字段定义推荐时，Transfer 才会全量扫描所选源字段的实际值并展示结果供确认。

`mode`：

| mode | 语义 |
|---|---|
| `project` | 只输出 fields 声明的目标字段；可推导 source `field_selection`。 |
| `passthrough` | 保留源 row 全字段，再应用字段映射覆盖 / 新增目标字段；不下推 `field_selection`。 |

旧任务外层 `mappings` / `field_mappings` 不作为新执行主链路输入；相关表和独立 mappings API 已删除，不能成为新的配置来源。

## 六、写入策略

`policy.apply_mode`：

| 值 | 说明 |
|---|---|
| `replace` | Transfer 写入前清理目标资源或让 prepare 重建目标。 |
| `append` | 追加写入；失败 retry 当前拒绝 append，避免重复写入。 |
| `upsert` | 按稳定键幂等新增或更新；当前支持声明幂等 upsert 能力的 PostgreSQL、MySQL、OceanBase、openGauss 与 TiDB native table 目标；OceanBase/openGauss/TiDB 限非空间表。 |
| `upsert_delete` | 数据库 CDC v1 按稳定键新增、更新和物理删除；与目标 partition ledger 原子提交。 |

apply mode 是 Transfer policy；真实 upsert/delete 能力必须由目标 engine Provider 和 capability 声明。raw copy 第一版只支持 `replace`，并要求目标 engine 提供删除资源能力。

## 七、Watermark bounded incremental

当前支持组合为：

```text
PostgreSQL/MySQL/OceanBase/openGauss/TiDB native table -> PostgreSQL/MySQL/OceanBase/openGauss/TiDB native table
bounded + incremental + watermark + upsert
```

配置必须声明 `load.change_detection.field`、`start=committed`、`end=execution_upper_bound`，并在 `target.policy.keys` 声明稳定目标键。`tie_breaker=[]` 表示仅同步新增：watermark field 必须自身精确匹配非空主键或唯一约束，并由用户确认可靠单调递增且不可变，目标 keys 必须是该字段映射后的目标字段。非空 `tie_breaker` 表示同步新增和更新：tie breaker 必须精确匹配非空主键或唯一约束，并且稳定、不可变，目标 keys 必须与其字段映射一一对应。每次 execution 在源数据库的一致性快照内冻结游标上界，只读取 `(committed_position, execution_upper_bound]` 并稳定排序；MySQL、OceanBase 与 TiDB 源必须是目录报告为 InnoDB-compatible 的基表，OceanBase、openGauss 与 TiDB 当前不得包含空间字段。

同步主状态存储在 `transfer.sync_states`。position 使用 `type=watermark`、`version=v1` 的 JSON；目标批次提交成功后才允许携带 `state_version` 和本次 fencing token 做 CAS 更新。重复应用必须由目标 `TableUpsertProvider` 幂等吸收：PostgreSQL 使用 `ON CONFLICT ... DO UPDATE`，openGauss 使用配置 keys 作为 `ON` 匹配条件且不更新匹配键的原生 `MERGE INTO`，MySQL 及 MySQL 模式 OceanBase 使用 InnoDB 事务内的 `ON DUPLICATE KEY UPDATE`，TiDB 使用事务内的 `ON DUPLICATE KEY UPDATE` 并固定以 `VALUES(column)` 引用冲突值。MySQL-compatible 目标的配置 keys 必须精确匹配非空主键或唯一约束，且目标表不得存在与配置 keys 不同的唯一约束；OceanBase、openGauss 与 TiDB 当前只支持非空间目标。

第一版只支持 resume：新 execution 从 committed position 继续并在成功后推进主状态。不提供 replay，不发现物理删除，也不支持只读副本 lookback。单字段模式不保证已有记录更新；复合模式要求源表所有 insert/update 都可靠更新 watermark。时间回拨或未更新 watermark 的变化不在保证范围内。

## 八、Continuous/Kafka v1 契约与当前实现边界

阅读导航：[来源与映射](#业务-kafka-来源与映射)、[目标提交](#目标应用与提交顺序)、[能力与界面](#当前能力与界面)、[DLQ/replay](#dlq-与-bounded-replay)、[恢复与取消](#位置恢复与取消)、[诊断与投影](#诊断与安全投影)、[恢复策略](#自动恢复与运行策略)、[Continuous 验收边界](#continuous-验收边界)。

### 业务 Kafka 来源与映射

业务 Kafka continuous 路径为 keyed JSON record -> PostgreSQL/MySQL native table upsert。业务 Kafka 作为 System Engine 暴露 `service -> topic` catalog；用户选择 `type=topic` locator，partition 不进入资源树或 locator。Infra Kafka 是数据库 CDC 的内部实现，不进入公开任务配置。

Kafka Provider 通过 `ChangeStreamReaderProvider` 返回原始 ChangeRecord 和 per-partition provider position；Transfer adapter 只接受 JSON object，从 value 的显式非空字段提取稳定 key，并归一化为 `operation=upsert` ChangeEvent。任务必须提供完整 `field_mapping` 固定目标 schema，source key 映射后必须与目标 keys 一一对应，并显式提交 `runtime.record_failure.mode=block|dead_letter`，不得依赖字段省略猜默认值。

任务创建或编辑时可以显式请求 Manager 对 Topic 当前保留范围尾部做一次有界预览，并从返回的 JSON object 样本生成顶层字段、目标类型和可空性的候选建议。样本建议不是 Topic schema，不写入 Meta attributes，也不能自动进入任务配置；用户必须在确认对话框中检查建议，确认后才合并为正式 `field_mapping`。已有手工映射始终保留，样本未覆盖的字段不能据此删除。运行时仍只认已确认的完整映射，并继续按未知字段、缺失必填字段和类型不匹配的严格规则处理记录。

### 目标应用与提交顺序

目标必须声明原子、单调且覆盖任务所需 operation 的 `PartitionedTableChangeApplyProvider`；各路线的目标范围见[支持矩阵](transfer-任务语义与同步模式.md#三当前支持矩阵)。每个 task 的 `apply_identity` 由服务端生成且不可修改。目标变更与 partition `next_offset` 的原子提交、分区拆批及同键保留最高 offset 的规则统一见[continuous 正式契约](../../docs/spec/tasks/Transfer同步执行.md#transfer-continuous-sync-v1-契约)与[交付保证](transfer-任务语义与同步模式.md#61-交付保证)，普通 `TableUpsertProvider`、Infra state CAS 或 runtime lease 不能替代目标侧原子应用。

各目标 ledger 的表名与归属以[CDC 正式契约](../../docs/spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)为准：PostgreSQL ledger 位于业务目标库；MySQL 使用同一目标数据库的 InnoDB 私有表，不跨库；Oracle CDC 目标 schema 必须等于连接用户，ledger 通过 ownership comment 从 Catalog 隐藏。同名 ledger 结构不符合唯一规范时直接失败，不保留兼容路径。

每个 partition 的主状态保存在 `transfer.sync_states`，position 固定为 `type=kafka_offset`、`version=v1`、`next_offset`；consumer auto commit 禁用。先提交目标业务行与 ledger，再以 runtime fencing token + state version 做 Infra CAS；两步之间崩溃会重放，由目标 ledger 吸收重复并阻止旧 worker 回写，不宣称分布式 exactly-once。首次无状态 partition 必须显式选择 `earliest|latest`。

### 当前能力与界面

业务 Kafka JSON upsert + DLQ 与 PostgreSQL/MySQL/Oracle CDC snapshot/upsert/delete 已实现，并共用同一个 consumer/apply/CAS 主循环。Worker 分工统一见[顶层字段](#21-顶层字段)，状态控制与租约归属见[Continuous 运行时](transfer-任务语义与同步模式.md#六continuous-运行时)，交付与恢复约束以[continuous 正式契约](../../docs/spec/tasks/Transfer同步执行.md#transfer-continuous-sync-v1-契约)为准。

Console Wizard 已支持业务 Kafka 以及 PostgreSQL/MySQL/Oracle CDC 配置。业务 Kafka 默认显式发送 `block`，公开 API 可显式选择 `dead_letter`，完整策略见[Kafka 与 DLQ 契约](../../docs/spec/tasks/Transfer-Kafka与DLQ.md#transfer-业务-kafka-dlq-与-bounded-replay-v1-契约)。execution 详情展示 owner、heartbeat、lease、fencing token、每个 partition 的 committed next offset 与最近提交时间。

当前仍不支持无 key append、Schema Registry、Avro、Protobuf、Kafka target、数据库 CDC replay/DLQ、普通 Oracle LOB/RAC、ArcGIS SDE 或自动 schema evolution。

### DLQ 与 bounded replay

`dead_letter` 的错误分类、幂等身份和处理协议统一见[Kafka 与 DLQ 契约](../../docs/spec/tasks/Transfer-Kafka与DLQ.md#transfer-业务-kafka-dlq-与-bounded-replay-v1-契约)。顺序为 DLQ payload → `transfer.dead_letters` 控制索引 → 目标 `skip` ledger → `transfer.sync_states` CAS，任一步失败不得推进后续 position。仅业务 Kafka 确定性记录错误可以进入 DLQ；source/poll、目标、fencing、retention 和 Infra 故障仍严格阻塞。

业务 Kafka bounded replay 的唯一入口为 `POST /task-definitions/:id/replay`，请求只提交显式 per-partition `[start_offset,end_offset)` 和不存在的新 PostgreSQL 目标 `parent_locator + name`。source、mapping、key、policy 与原目标事实从 owner task 冻结继承，不得覆盖；提交前校验 retention 并拒绝原目标或已有目标，bounded worker 执行时再次核验 retention 和目标不存在。

独立 execution、execution-scoped apply identity 与主状态隔离统一见[Checkpoint 与 Replay](transfer-任务语义与同步模式.md#五状态checkpoint-与-replay)，完整失败约束以上述 Kafka 与 DLQ 契约为准。replay 从原业务 topic 读取，DLQ topic 不是 replay source；它写独立隔离表，不读写主水位、lease、apply identity、目标及任务状态，可与主 continuous runtime 并行，失败不改变主任务状态或触发 continuous 恢复 circuit。DLQ 查询的 Tenant/task 隔离与 payload 安全见[公开查询与来源隔离](数据库架构.md#dlq-公开查询与来源隔离)。

### 位置恢复与取消

resume 的新 execution 与位置核验见[用户运行控制](transfer-任务语义与同步模式.md#用户运行控制)；完整恢复条件以[continuous 正式契约](../../docs/spec/tasks/Transfer同步执行.md#transfer-continuous-sync-v1-契约)为准。committed `next_offset` 必须落在当前 partition 的 `[earliest_offset,latest_offset]` 内；retention 已清除该位置或位置高于 latest 时均明确失败，不能自动重置到 earliest/latest，须人工处理。

PostgreSQL/MySQL/Oracle 目标锁等待必须响应 context 取消，并回滚未完成的业务行与目标 ledger 事务；Oracle 使用 `FOR UPDATE NOWAIT` 有界重试，约束见[CDC 正式契约](../../docs/spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)。

### 诊断与安全投影

diagnostics 采样间隔、degraded/critical retention 阈值和 checkpoint 停滞阈值由 Transfer 配置管理页面“连续任务策略”统一维护，不进入任务 JSON。指标计算、unknown 条件与真实 position commit 口径见[分区位置与 Kafka 恢复窗口](transfer-任务语义与同步模式.md#分区位置与-kafka-恢复窗口)，捕获健康与源日志窗口见[诊断专题](transfer-任务语义与同步模式.md#捕获健康与源日志窗口)。

诊断结果写入 `common.task_executions.metadata.continuous.diagnostics`；Monitor 只读取 execution metadata，不直连业务 Kafka 或 Transfer 私有状态表。允许公开的字段与敏感信息排除统一见[安全投影与读取边界](transfer-任务语义与同步模式.md#安全投影与读取边界)。

### 自动恢复与运行策略

continuous worker 的重启恢复、领取条件与 circuit 状态统一见[自动恢复与退避](transfer-任务语义与同步模式.md#自动恢复与退避)，不可变审计与可变状态见[恢复审计与运行状态](transfer-任务语义与同步模式.md#恢复审计与运行状态)；唯一 pending execution、租约及恢复约束以[continuous 正式契约](../../docs/spec/tasks/Transfer同步执行.md#transfer-continuous-sync-v1-契约)为准。

指数退避、最大连续失败次数、circuit 冷却时间和稳定运行窗口由“连续任务策略”配置页统一维护，属于 Transfer-owned 平台运行配置，不进入任务 JSON。

### Continuous 验收边界

本章能力摘要、execution 详情和诊断展示不作为当前版本的运行通过记录；事件与扫描的分版本范围分别见[过程事件记录](../CLAUDE.md#continuous-过程事件与浏览器验收)及[精确扫描与审批记录](../CLAUDE.md#continuous-精确扫描与审批归属)。

目标事务与 Infra CAS 的提交顺序须实际核对；DLQ 处理或查询成功不替代 replay、Tenant/task 来源隔离与完整链路验收；位置诊断或取消请求返回不证明实际恢复、停止或回滚通过；策略参数保存或 recovery execution 创建不等于自动恢复成功。各项须按对应场景分别验证。

## 九、数据库 CDC v1 已冻结边界

阅读导航：[链路与配置](#cdc-链路与公开配置)、[目标与位点](#cdc-目标应用与位点)、[源类型与空间捕获](#源类型与空间捕获)、[Oracle 既有验证](#oracle-既有验证与验收边界)、[漂移与审批恢复](#schema-drift-与审批恢复)、[生命周期与清理](#cdc-生命周期与清理)、[未支持项与运行事实](#cdc-未支持项与运行事实)。

### CDC 链路与公开配置

当前数据库 CDC 主链路如下，语义与来源／目标范围见[数据库 CDC 专题](transfer-任务语义与同步模式.md#63-数据库-cdc)：

```text
PostgreSQL/MySQL/Oracle 单表
  -> 对应 Debezium Connector
  -> Infra Kafka
  -> Transfer Continuous Worker
  -> PostgreSQL/MySQL/Oracle 新目标表
```

公开任务只保存 `runtime.boundary=continuous`、`load.mode=incremental`、`load.change_detection.type=cdc`、`bootstrap=initial_snapshot`、源表 locator、目标表和 field mapping。source provider 只能由 locator 对应的 System Engine 解析结果决定，不进入任务 JSON。connector、provider 专属捕获资源、Infra Kafka topic 和 consumer group 都由服务端生成，不进入 System Engine 或 Meta 资源树。

### CDC 目标应用与位点

第一版只支持有稳定主键的单表。Debezium `op=r` 作为 snapshot upsert，`op=c|u` 作为 upsert，`op=d` 使用 record key 做物理 delete；目标固定为 `apply_mode=upsert_delete`，可选择声明完整原子应用能力的 PostgreSQL、MySQL 或 Oracle。目标必须是不存在的新表，由 bootstrap 创建；不清空或接管已有目标表。Oracle CDC 目标的字段、精度、空间维度与原子应用约束以[CDC 正式契约](../../docs/spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)为准，不保留兼容映射或旁路。捕获位点由 Kafka Connect 管理，Transfer 只在 `transfer.sync_states` 保存目标已应用的 Infra Kafka `next_offset`，两类位点不能互相代替。

### 源类型与空间捕获

源类型导航：[PostgreSQL](#postgresql-cdc)、[MySQL](#mysql-cdc)、[Oracle](#oracle-cdc)。

#### PostgreSQL CDC

PostgreSQL CDC 在创建 capture 前核对完整源表字段和真实类型，开放 `string|bool|int|bigint|float|double|decimal|date|time|timestamp|json|uuid|geometry`。PostGIS geometry 必须固定 OGC type、正 SRID和 XY/XYZ 维度；Transfer 将 Debezium `{wkb,srid}` 解码为 EWKB，按源空间事实创建目标 geometry 列，不做坐标转换，geometry 也不能作为主键。connector 固定 Decimal 字符串和 Connect 毫秒时间编码；`time` 和无时区 `timestamp` 仅允许精度 `0..3`。`bytea`、数组、geography、未约束 geometry、M/ZM geometry、interval、枚举及其他用户定义类型明确拒绝。

Debezium 的 geometry 属性名是 `wkb`，真实 PostGIS connector 可能在该 base64 属性中发送带 SRID 的 EWKB。Transfer 使用统一 WKB/EWKB 解析器，并校验内嵌 SRID、旁路 `srid` 与 generation 冻结事实一致，再输出 ADDP 标准 EWKB 行值。

#### MySQL CDC

MySQL CDC 固定支持 MySQL 8.0、有稳定非空主键的单表、`log_bin=ON`、`binlog_format=ROW`、`binlog_row_image=FULL` 和非零 server id。v1 接受有符号整数、字符/文本、Decimal、浮点、毫秒精度日期时间、JSON 和 binary/BLOB；拒绝 unsigned、`TINYINT(1)`/BOOL、BIT、ENUM/SET、YEAR、空间类型、超过毫秒的时间精度和 zero date。每个 generation 拥有唯一 connector server id、data topic 和 `cleanup.policy=delete + retention.ms=-1` 的 schema-history topic，Stop 时统一清理。

#### Oracle CDC

Oracle CDC 固定支持有稳定非空主键的普通单表，并要求独立 `cdc_database_name`、`cdc_user`、`cdc_password`、ARCHIVELOG、FORCE LOGGING、minimal supplemental logging 以及源表 `SUPPLEMENTAL LOG DATA (ALL) COLUMNS`。源类型与编码以[CDC 正式契约](../../docs/spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)为准，源端拒绝条件见[Oracle 源限制](transfer-任务语义与同步模式.md#oracle-源限制)。Oracle 原生 `BOOLEAN` 虽可被 Debezium 初始快照读取，但 Debezium 3.6 LogMiner 不能稳定交付包含该列的流式 DML，因此和普通 LOB/binary、JSON/XML、超过毫秒精度的 timestamp 一样在创建 capture 前拒绝，不能以快照成功推断增量可用。

Transfer 使用源 schema owner 账号创建 generation-owned 镜像表、行级同步触发器和 DDL guard：XY 转为 WKB BLOB，XYZ 转为保留 Z 值的 GeoJSON CLOB，再由同一 connector/consumer 主路径捕获；运行期间对逻辑源表的 ALTER/DROP/RENAME 会被明确拒绝，Stop 后才允许 DDL。空间事实从 Oracle Catalog Facts 冻结，adapter 严格校验 base64 WKB 或 GeoJSON 的 SRID、拓扑和 XY/XYZ 维度，统一输出标准 EWKB；内部镜像列不等于开放用户 LOB CDC。表级 ALL COLUMN LOGGING 是共享 source readiness，Stop 不删除，Spatial 镜像表、行级触发器和 DDL guard 则在 Stop 核对身份后删除。

### Oracle 既有验证与验收边界

原有数据面验证摘要记载：真实 E2E 已覆盖 PostgreSQL/MySQL/Oracle 三类源到 Oracle 目标的 snapshot/insert/update/delete 与 offset/ledger 收敛；Oracle 源还覆盖跨 consumer batch 的 128 行事务提交、未提交不可见和回滚隔离。Oracle Spatial 的 XY Polygon/MultiPolygon 已覆盖 PostgreSQL/MySQL/Oracle 目标，XYZ 当前只覆盖 PostgreSQL，MySQL 与 Oracle 目标仍明确只支持二维 geometry。

上述摘要不作为当前 HEAD 的运行通过记录。[Oracle 专项入口说明](../README.md#oracle-cdc-专项入口缺口)区分独立用例的覆盖范围：目标生命周期用例当前仅覆盖 PostgreSQL/MySQL 源，不能与此处三类源的既有摘要混同；缺少标准调用与隔离清理门禁、未运行或跳过的项目均不计为通过。

### Schema drift 与审批恢复

schema drift 的阻塞流程统一见[Schema 变化](transfer-任务语义与同步模式.md#七schema-变化)：execution 以 `schema_change_blocked` 结束并记录 missing/unexpected/incompatible diff，当前消息和 committed offset 不推进，任务进入 `status=blocked`，禁止直接启动、恢复或重试。connector 仍会继续采集，因此 backlog、Kafka retention 和磁盘风险继续存在。

只有 PostgreSQL/MySQL 原 capture generation 中，当前阻塞消息实际包含、源表仍存在、允许 NULL、非主键且非 geometry 的新增字段可以人工审批。使用 `GET /task-definitions/:id/schema-change` 读取当前请求和复查建议；`POST /task-definitions/:id/schema-change/approve` 必须逐字段提交 `source`、`target`、`target_type` 和 `nullable=true`，完整覆盖本次新增字段。实时源复核、Provider 幂等加列及 mapping/revision 的事务规则统一见[CDC 正式契约](../../docs/spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)。审批后为 `status=idle, desired_state=paused`，不隐式恢复；用户显式 Resume 从原 committed offset 继续。Oracle 的任何 schema drift 及其他不可审批变化只能永久 Stop 后创建新任务和新目标表，具体范围见上述 Schema 变化专题。

审批后的 request 级 Meta 补扫 claim、CAS 接管和旧 token 拒绝统一见[审批补扫 claim 与失败边界](数据库架构.md#审批补扫-claim-与失败边界)及 CDC 正式契约；仅相同重复审批 POST 可接管过期 `running`，GET 始终只读。真实 Meta 调用失败为 `failed`，不由重复审批自动重试，可在 Meta 手动扫描；补扫失败不回滚已提交的 DDL/mapping revision。`TRANSFER_META_SCAN_CLAIM_TTL` 为部署配置，默认 2 分钟且必须大于 Meta client 的 HTTP 超时，不进入任务 JSON。扫描提交成功也不等于扫描完成或恢复成功，实际通过范围见[验证范围](transfer-任务语义与同步模式.md#十一验证范围)。

### CDC 生命周期与清理

CDC pause/resume 的捕获与位置边界统一见[用户运行控制](transfer-任务语义与同步模式.md#用户运行控制)和[暂停与保留窗口](../../docs/spec/tasks/Transfer数据库CDC.md#暂停与保留窗口)，不保证无限期暂停或无条件恢复。

CDC stop 不可逆；服务端校验 `confirmed=true` 和与当前任务名称完全一致的 `confirmation_text`，Console 使用 danger 二次确认并要求输入任务名称。接口与普通业务 Kafka Stop 的区别、资源所有权、身份核验及清理顺序统一见[停止确认与清理顺序](../../docs/spec/tasks/Transfer数据库CDC.md#停止确认与清理顺序)。

`desired_state=stopped` 也用于新任务；永久终态、首次启动与 `cleanup_failed` 的重试限制见[恢复资格与终态判定](../../docs/spec/tasks/Transfer数据库CDC.md#恢复资格与终态判定)，保留对象与实际停止、资源清理的认定见[清理验收边界](../../docs/spec/tasks/Transfer数据库CDC.md#保留对象与清理验收)。状态保存或清理请求返回不代替实际验收。

### CDC 未支持项与运行事实

完整配置、envelope、schema drift、目标原子应用和资源 owner 约束以[CDC 正式契约](../../docs/spec/tasks/Transfer数据库CDC.md#transfer-数据库-cdc-v1-契约postgresqlmysqloracle)为准，运行语义见[数据库 CDC 专题](transfer-任务语义与同步模式.md#63-数据库-cdc)。普通 Oracle LOB/RAC、ArcGIS SDE、多表、无主键、数据库 CDC replay/DLQ、Schema Registry、Avro、Protobuf、自动 schema evolution 和 truncate 事件均未进入当前实现；业务 Kafka 的 DLQ 与 bounded replay已公开。

capture supervisor 通过 Kafka Connect REST 和 Infra Kafka admin API 管理任务级 generation，状态登记在 `transfer.capture_resources`；continuous worker 从登记的内部 topic/group 消费 Debezium 3.6 schemaless JSON。人工 additive request 和 revision 审计保存在 `transfer.schema_change_requests`，状态与公共观测分工见[Schema Change Request](数据库架构.md#七cdc-schema-change-request)；公开任务配置不出现这些内部名称或 schema evolution 开关。

## 十、Checkpoint、进度和重试

本节维护 execution metadata 中的 checkpoint 口径；持久事实不自动成为公开字段，专业 HTTP 读取范围见[执行记录](#12-执行记录)及其引用的安全 DTO 契约。

当前 checkpoint 语义：

- 每个成功写入 batch 后更新执行进度。
- `checkpoint_offset` 等于累计 `records_read`；raw copy 第一版完成后为 `1`。
- `checkpoint_state` 可包含 `batch_index`、`source_offset`、`records_read`、`records_written`、`resume_marker`、`commit_marker` 等。
- marker 由 provider 生成并解释；Transfer 只保存和展示，不解析 marker 内部字段。

### 恢复分级

| 等级 | 当前状态 |
|---|---|
| observable | 已支持，用于进度展示和故障定位。 |
| restartable | 失败 snapshot retry 创建新 execution 并从头执行；append 拒绝。 |
| resumable | watermark incremental 通过 `transfer.sync_states` 支持 execution 间 resume，目标按幂等 `table_upsert` capability 选择；支持引擎、非空间限制与稳定键条件统一见[增量同步契约](#七watermark-bounded-incremental)。snapshot checkpoint 仍仅可观测。 |

本表只区分 execution 的观测、重跑与增量续读；任务的调度暂停／恢复另见[调度授权与控制](#调度授权与控制)。恢复操作及新旧 execution UUID 的使用见[快速开始](快速开始.md#四checkpoint-和重试)。调度控制、重跑与续读须分别验收。静态配置、授权或调度更新成功均不等于实际触发、Worker 执行和目标数据验收通过。

## 十一、写后 Meta 扫描

`auto_scan_metadata=true` 时，Transfer 按 runtime boundary 触发 Meta deep scan：

- bounded execution 在成功写入后扫描一次。
- 普通业务 Kafka continuous execution 不等待任务结束；目标 Provider 首次成功建立目标结构后，按 task `apply_identity` 提交一次目标表的精确扫描。
- 数据库 CDC connector 把 Debezium `Initial Snapshot` 通知写入当前 generation-owned 单分区 data topic；所有合法通知作为目标 `skip` 推进，只有严格匹配当前 connector 的 `COMPLETED` 所在 offset 在目标 ledger 和 Transfer position 提交后才触发扫描。空源表同样触发，不依赖数据事件或 `source.snapshot=last`。
- continuous 首次扫描使用持久化 claim 防止 worker recovery、resume 或并发实例重复提交；Meta 提交失败不阻断数据面，失败或过期 claim 由后续 runtime session 重新领取。
- 目标 schema 经正式审批发生变化后再次扫描；普通 DML、单条事件和单个 batch 不扫描。持续变化的行数等统计由低频计划扫描或手动刷新维护。

Transfer 不直接推导目标文件 attributes。GeoJSON 导出目标使用独立 `format=geojson` 写出；写后扫描由 Meta 按统一格式探测和 GeoJSON provider 解析目标内容，负责写入 `type_info.table`、`format_info.geojson` 和实际存在的 `capabilities.spatial`。

bounded 写后、continuous 首次建表／CDC 初始快照完成及结构变更审批后的扫描都遵守同一范围约束：缺少精确目标时不得提交父范围或全引擎扫描。

Transfer 只提交本次实际写出的目标边界；encoded/raw content 目标使用 `ref_groups`，不扩大为父目录扫描：

| 目标类型 | 扫描目标 |
|---|---|
| native table | 完整 `targets` 表 locator，保留 schema／database 和表名；不扫描父范围。 |
| NFS encoded/raw file | 单文件 `ref_groups`。 |
| MinIO / S3 encoded/raw object | 单对象 `ref_groups`。 |
| Shapefile refs | 本次实际生成的 refs group，不补不存在的 sidecar refs。 |

Transfer 不直接写目标 Meta attributes。
