# ADDP 数据血缘能力规范

**状态**：正式规范（数据项级已实现，字段级首期范围见 6.1）
**更新时间**：2026-10-17
**适用范围**：Meta、Transfer、Develop、Manager、Service、Asset、Graph、Orchestrator 及 `common` / `common-frontend`

## 一、定位与边界

数据血缘回答数据项之间的来源、派生和服务依赖关系。它是 Meta 的关系视图，不是新的数据资产实体、任务实体或图数据库业务模块。

血缘能力分为三层：

| 层级 | 事实 | 事实源 | 用途 |
| --- | --- | --- | --- |
| 执行事实 | 一次执行实际读取、写入和产生的资源引用 | 真实读写 owner 的 execution | 证明某次执行发生了什么 |
| 关系证据 | 已解析的资源关系及其执行/发布依据 | Meta collector | 历史追踪和审计 |
| 当前投影 | 当前有效的上游、下游和服务依赖 | Meta 根据证据维护 | 查询、影响分析和展示 |

当前投影不是事实源。任何关系都必须能回溯到执行事实或服务发布事实。

本规范第一阶段不解决：

- 外部系统未被 ADDP 观察时的推断血缘。
- 运行时内存对象作为独立数据资产登记。
- 任意 SQL 方言的字段级自动解析。
- 用户访问行为、调用次数和客户端作为数据血缘边。
- 用 AGE、Neo4j 或其他独立图数据库承载血缘事实。

## 二、核心概念

### 2.1 血缘主体

血缘图允许以下主体类型：

| 主体类型 | 身份 | 说明 |
| --- | --- | --- |
| `data_item` | `meta_item.id`，跨模块使用 ResourceLocator / fingerprint | 表、视图、文件、对象、collection、graph 或 whole dataset |
| `published_service` | Service 的 `service_id + published_revision` | 已发布的数据查询、瓦片或图查询服务版本 |
| `execution` | `common.task_executions.execution_id` | 只作为证据和展示上下文，不是数据资源 |
| `field_ref` | `item_id + field_name + schema_snapshot_hash` | 数据项内部字段，按冻结结构定位 |

`node` 资源树节点不是血缘主体，除非它本身被规范识别为 data item。

### 2.2 关系类型

第一阶段只定义三种关系：

| 关系 | 方向 | 语义 |
| --- | --- | --- |
| `derive` | data item -> data item | 目标内容由源内容复制、转换、聚合或物化产生 |
| `reference` | data item -> data item | 视图或其他逻辑对象依赖源对象，但不表示一次物理写入 |
| `serve` | data item -> published service | 已发布服务读取、发布或暴露该数据项 |

`execution` 不作为普通数据边的源或目标；它通过关系证据关联产生该关系的执行记录。

### 2.3 指纹与资源身份

现有 Item fingerprint 是 `engine_id + full_name` 的 SHA256 资源身份摘要。它满足：

- 用于跨模块事实传递、Meta 解析和幂等去重。
- 内容变化时保持不变，因此不是内容版本指纹。
- 不能单独表达写入时间、数据版本、执行结果或当前关系有效期。

血缘实现遵循以下组合：

1. owner 在执行事实中使用标准 ResourceLocator，已存在的资源同时携带 item fingerprint。
2. Meta collector 解析资源并关联 `meta_item.id`。
3. 血缘关系内部优先使用 `meta_item.id` 维护引用完整性。
4. 历史证据保存 locator、fingerprint 和必要的显示快照，不能依赖当前 `meta_item` 名称重建历史。
5. 新目标使用 `parent_locator + name`，只有完成 Meta scan 并形成真实 item 后才进入资源血缘。

Meta item 的 fingerprint upsert 和软删除恢复规则保证同一资源重扫后可以继续关联；资源重命名产生新的资源身份，不覆盖旧历史关系。

## 三、存储架构

### 3.1 存储选择

血缘使用 Infra PostgreSQL 的普通关系表；当前图查询按固定方向执行有界分层遍历，可在同一关系事实源上优化为递归 CTE：

- 不新增 Infra Neo4j。
- 不依赖 Apache AGE 或其他 PostgreSQL 图扩展。
- 不在 Meta 和 Neo4j 之间双写。
- 所有血缘事实、证据和当前投影位于 Meta schema。

PostgreSQL 19 SQL/PGQ 可以在未来作为关系表上的只读 Property Graph 视图评估，但不得作为第一阶段运行前提。关系表仍是唯一事实源。

### 3.2 逻辑数据模型

正式实现至少应提供以下逻辑结构，具体字段和索引在 Meta migration 设计中落实：

#### `lineage_item_relations`

当前有效的 data item -> data item 关系投影，包含：

- tenant、source item、target item。
- relation kind、granularity、当前有效状态。
- 首次发现、最近发现和关闭依据。
- 当前写入语义（replace / append / upsert 等）。

source/target 使用 `meta_item.id` 外键；不能复制一套 `lineage_nodes` 作为 Meta Item 的第二身份表。

#### `lineage_service_dependencies`

当前有效的 data item -> published service 依赖投影，包含：

- tenant、source item。
- `service_id`、发布 revision、dependency hash。
- dependency kind（table / SQL / federation / tile / graph）。
- 依赖字段和快照时间（字段级能力启用后）。

Service 仍由 Service 模块拥有，Meta 只保存用于血缘查询的关系投影和服务版本软引用。

#### `lineage_observations`

不可变关系证据，至少包含：

- tenant、relation kind、source/target 快照。
- execution id 或服务发布事实引用。
- capture method（declared / runtime / parsed）。
- observed_at、producer module、task type、operator/step 摘要。
- 资源 locator、fingerprint 和必要的 schema/字段快照。

执行记录被清理后，Meta 中的 observation 仍必须可查询；execution id 只作为可回查的软引用。

### 3.3 当前投影的写入语义

- `replace`：关闭目标当前已有的 derive 入边，再激活本次成功执行产生的关系。
- `append` / `upsert` / CDC：保留并合并本次成功执行的有效来源。
- `reference`：按当前发布或 DDL 定义替换旧的逻辑依赖。
- 未知写入语义：只能保存 observation，不得伪造 current projection。
- Meta 发现目标在没有对应 ADDP 执行时发生外部变化：标记关系为 `unverified` 或 `stale`，不得猜测新的来源。
- `meta_item` 从有效状态转为软删除时，所有以该 item 为端点的非关闭 item relation 以及以该 item 为来源的 service dependency 必须标记为 `stale`；不可变 observation 继续保留。该 item 后续恢复只恢复资源身份，不自动重新激活关系，只有新的执行或发布事实可以把对应投影重新置为 `active`。
- Meta 物理清理不得删除仍被当前投影或不可变 observation 引用的 `meta_item`。这类软删除 item 是血缘身份锚点，不属于可释放候选；清理执行必须保留并报告跳过，不能级联删除关系或证据，也不能把外键冲突降级成删除成功。只有不再被 `lineage_item_relations`、`lineage_service_dependencies`、`lineage_observations` 引用的 item 才能物理删除，其所属 node 仅在不再包含任何 item 后才能物理删除。

关系查询必须支持当前投影和基于 observed_at / execution 的历史视图，不能只保留一条无时态边。

## 四、统一执行事实

### 4.1 `lineage_facts`

真正读写数据的 owner 必须在统一 execution 结果中写入版本化的 `lineage_facts`，而不是让 Meta 解析各模块私有 metadata。统一结构如下：

```json
{
  "schema_version": "addp.lineage-facts/v1",
  "inputs": [
    {
      "port": "source",
      "locator": "addp://...",
      "item_id": 21,
      "item_fingerprint": "..."
    }
  ],
  "outputs": [
    {
      "port": "target",
      "locator": "addp://...",
      "item_id": 22,
      "item_fingerprint": "...",
      "write_mode": "replace"
    }
  ],
  "operations": [
    {
      "kind": "derive",
      "operator": "optional-public-operator-id",
      "input_ports": ["source"],
      "output_ports": ["target"]
    }
  ],
  "runtime_execution_id": "runtime-local-id",
  "meta_scan_refs": ["scan-execution-id"]
}
```

约束：

- Runtime 只提供节点和端口执行事实，不构造 ADDP 资源身份。
- owner 负责把 ResourceLocator、`produced_targets` 和实际绑定写入长期 execution 结果。
- 成功执行的目标写入模式 `create` 属于已知持久化效果，与 `replace/append/upsert/cdc` 一样参与正式派生关系投影。`create` 表示首次创建目标，不关闭其他目标的输入关系；目标尚未完成 Meta scan 时，立即采集保留执行事实，由既有周期 collector 在 DataItem 可解析后复用同一采集方法生成 observation 和当前关系。不得把 `create` 当作未知模式，也不得要求再次覆盖写入才能显示首次创建的血缘。
- 不保存连接凭据、Token、临时挂载路径或完整大对象。
- 只有真实读写 owner 可以产生资源级 lineage facts。
- Orchestrator 只通过 `parent_execution_id` 提供父执行上下文，不重复生成资源边。

Develop 算子工作流按本次 execution 冻结并应用执行参数后的工作流生成资源血缘。每个读取节点使用独立的 `input.<节点ID>` 端口，每个实际产出节点使用独立的 `output.<节点ID>` 端口；同一节点的批量产物共用该节点输出端口并逐项保留真实 ResourceLocator 和写入模式。每个产出节点单独声明 `derive` operation，只关联该节点直接读取的资源，以及沿参数中的标准 `$ref` 数据引用追溯到的读取节点。数组与嵌套对象中的引用同样参与追溯，重复来源去重；未参与该产物计算的读取节点不产生该产物的来源边。`depends_on` 只约束执行顺序，不证明资源来源；不得把整个工作流的所有输入和输出做交叉配对。Meta 继续只消费既有 `lineage_facts` 端口和 operations，不解析 Develop 私有 DAG，不登记运行时内存对象。

### 4.2 服务发布事实

Service 发布或变更一个版本时，必须向 Meta collector 提供等价的发布事实：

- `published_service` 身份、发布 revision 和 dependency hash。
- 表模式、SQL、联邦 SQL、瓦片或图服务的来源依赖。
- 已解析的 source locator、item id、item fingerprint。
- 输出字段和依赖快照摘要。

表模式直接使用已有 source snapshot。SQL 模式如果不能得到明确的源 item 列表，只能标记依赖不完整，不能伪造血缘边。

## 五、采集边界

| 模块 | 第一阶段处理 |
| --- | --- |
| Transfer | `sync` 的成功读写和写入模式 |
| Develop | 持久化 Workflow 的资源输入输出，以及通过 relation 参数契约向固定正式表写入的查询；任意写 SQL/DDL 不自动推断 |
| Manager | 只有产物成为业务 data item 时才接入 |
| Service | 发布版本的 source dependency，关系为 `serve` |
| Graph | 输出形成稳定 graph item 时接入 |
| Orchestrator | 只提供 parent/child execution 上下文 |
| Meta | 扫描和 collector，不产生数据血缘 |
| Quality | 质量结果不是数据派生关系 |
| Model | 逻辑/物理设计关系作为后续独立图层 |
| Asset / Portal / Standard | 消费或治理关系，不产生数据派生关系 |
| Notebook | 受控 I/O 契约形成前不自动推断 |

采集顺序固定为：

```text
owner execution/publication fact
  -> owner immediately notifies Meta collector
  -> locator/fingerprint resolve
  -> Meta scan completion (if target is new)
  -> observation
  -> current projection
```

正式关系只在成功完成的持久化效果可确认后建立。部分提交、失败和取消必须由 owner 提供明确提交事实，否则只记录诊断，不建立成功关系。

执行事实的采集触发遵循单一处理路径：

1. owner 必须先把成功状态和 `lineage_facts` 原子地持久化到统一 execution，再通知 Meta 采集指定 execution。
2. Develop 通过 `POST /api/v1/meta/lineage/executions/{execution_id}/collect` 通知；该入口只接受 `addp-develop` Service Principal 和 `meta.lineage.create` Permission。该 Permission 必须在内置 `tenant.develop_runtime` Role 的发布清单及数据库迁移中同时声明；不能依赖管理员身份或周期补采掩盖即时通知权限缺失。
3. 立即通知与周期 collector 必须共同调用 `LineageService.CollectExecution`，不得分别实现两套解析或投影逻辑。
4. 通知失败不得把已成功的数据执行改为失败；Meta 周期 collector 负责漏采和失败重试。
5. Item 元数据刷新只更新资源元数据，不触发血缘采集；血缘事实的触发依据始终是 owner execution，而不是用户刷新行为。

周期 collector 的批量参数只限制单次数据库分页大小，不限制一轮采集总量。每轮先冻结成功且声明 `lineage_facts`、尚无 observation 的 execution ID 上界，再按 `created_at ASC, id ASC` 使用键集游标遍历该上界内的待采集执行；禁止反复只取最早一页，也不使用 OFFSET 分页。单个执行的全部 observation 与投影在同一事务内保存，因此周期查询按 Tenant 和 execution ID 排除已有 observation 的执行，不逐轮重放已完成历史，也不新增完成标记或第二套采集状态。未能解析 DataItem 时不写 observation，下一轮从头重试，确保目标延迟扫描后能够补采；轮内新增执行或已越过游标后才成功的执行由下一轮覆盖。单条执行采集失败应汇总报告并继续处理后续执行，数据库分页失败或上下文取消则终止本轮。立即通知的重复调用仍通过同一幂等路径处理，保持旧关系关闭及字段快照时态，不得重新激活已被后续写入替代的关系。

上述契约由 `make test-meta-postgres` 覆盖超过 500 条执行、同创建时间的分页边界、目标延迟扫描、重复采集幂等性、单条错误隔离及轮内新增执行的下一轮采集；沿用已有 Release/T2 的 Meta PostgreSQL 门禁，不新增部署依赖或公开 API。

采集器的回归场景、标准门禁和历史 T1 / T2 证据见[测试与验收规范](addp测试与验收规范.md#血缘采集器回归)，不作为新的部署后 T4 结果。

### 5.1 固定正式表查询写入

Develop Query Execution Supervisor 必须在业务写入事务成功提交后，将 `lineage_facts` 与成功终态在同一次带 lease 校验的执行记录事务中保存，再复用已有通知入口调用 Meta collector。输入采用 Provider 冻结并在写入事务中复核的完整 ReadSet，按 canonical path 稳定排序。命中 relation 参数绑定的来源保留 input.<参数名> 端口，View 展开的其他实际来源使用 read.<序号> 端口；不得仅靠调用方的绑定列表遗漏间接来源；目标采用本次实际写入的 `target_locator`。不得从 Orchestrator 的 `depends_on` 推导资源边，也不得将逻辑表建表任务作为数据来源。

覆盖计算的 `overwrite` 映射为血缘 `replace`；`append` 保留为 `append`。失败、取消、失去 lease 或终态保存失败时不通知采集、不建立成功关系；通知失败保留已落库的成功事实，由 Meta 周期 collector 重试。业务库提交与平台执行记录提交不是跨库事务，二者之间进程中断的执行不能自动冒充成功或补造血缘。

这一路径由 TableResultProvider 证明同 Engine 现有表写入的实际读取集合、源/目标实时结构和字段位置映射；Develop 记录完整字段事实或明确的 unavailable 状态，不在 Owner 复制 SQL 依赖解析，也不扩大为任意写 SQL/DDL 的推断。历史缺少事实的执行不能由 Meta 反向猜测；通过新的真实执行生成证据。

## 六、粒度

当前支持 table / file / object / view / collection / graph / whole dataset 的数据项级血缘。字段级能力覆盖 Transfer 显式 field mapping，以及由 Provider 证明并由执行 owner 写入的 SQL 输出来源；CTE、别名、`*` 和表达式均须以对应 Provider 的事实为依据，具体支持范围见 6.1。

字段不是默认的独立 data item，字段引用使用 `item_id + field_name + schema_snapshot_hash`，完整结构快照保存在执行事实和不可变证据中。无法可靠解析的 SQL 不得保存猜测边，也不使用任意浮点 `confidence` 伪装确定性。

### 6.1 字段级首期契约

首期支持 Transfer bounded native table -> native table，以及同一 PreparedQuery 能证明完整输出映射的 query source -> native table。多来源查询必须通过已有 `source.query.inputs[]` 声明全部输入，并与 Provider 的规范 ReadSet 精确一致；端口按原生路径绑定，不依赖输入声明与 Provider 返回来源的顺序。每个来源分别冻结原始结构，同一结果字段的多个值来源分别记录映射，仅参与行筛选的输入仍保留资源及结构事实，不生成字段值来源边。该扩展不改变 Console 单表基础查询构建器的职责。字段血缘回答目标字段值来自哪些源字段；过滤、JOIN 条件、排序及行数影响不混入值来源关系。查询源只消费 Provider 的 ReadSet / OutputLineage，不在 Transfer 解析 SQL/MQL；MongoDB 透明 aggregate 的嵌套字段别名、`$ifNull: [field, null]` 和 `$unwind.includeArrayIndex` 按 Provider 证据与实际 field mapping 组合，数组索引保留 derived 语义。opaque / unresolved 或缺少精确输出绑定的查询（包括当前接口不能证明的无来源结果列）、continuous/CDC、encoded 输出及空间重投影暂不声明完整字段血缘，继续记录数据项级事实，字段视图明确显示证据不可用。

PostgreSQL Provider 已能在同一 PreparedQuery 内组合非递归 CTE、派生表、JOIN、UNION 与表达式的字段值来源。该证明能力不替代执行 owner 的写入事实：Develop 的同 Engine 现有表写入消费 TableResultProvider 冻结计划，在成功事务返回的实时源/目标结构上记录 schema snapshot 与实际位置映射。CTE、JOIN、UNION 等可证明查询支持多来源字段血缘；不透明查询仍标记 unavailable。此能力必须通过真实执行验收，不能仅凭查询解析测试声明部署后的 DWD 血缘已经可用。

- `lineage_facts` 保持 `addp.lineage-facts/v1`；资源引用的 `schema_snapshot` 使用 `{hash, fields}`，fields 来自同一次真实读取和目标写入结构，hash 复用 Common 的 `TableSchemaSnapshotHash`。不回查当前结构补造执行快照。
- 查询源冻结 Provider 的原始来源结构及精确路径绑定，查询结果列不能充当原始来源快照。普通关系列的可选 path 按 Provider 原样冻结，省略时不补造单段路径；嵌套字段只按快照中的唯一字段名引用，完整 path 参与结构哈希，点号不会被 collector 或查询 API 再次拆分。查询保护准备同时返回实际受转换影响的结果字段名，与同次保护规则绑定；这些字段经过别名及后续 field mapping 后仍须记为 derived，抑制列不产生映射。
- operation 增加 `field_lineage_status=complete|unavailable` 和 `field_mappings`。每条映射明确 `input_port`、`output_port`、`source_field`、`target_field` 和 `transformation=direct|derived|generated`。generated 没有输入端口或源字段，只证明常量/默认值生成，不生成虚假的字段来源边。非空默认值会改变源值语义，记为 derived；数据保护准备阶段同时返回实际受转换影响的字段名，脱敏字段记为 derived，受抑制字段不产生目标映射；按运行时顺序组合多步映射与覆盖，透传字段来自实际源结构。
- 成功执行才采集；字段快照与映射必须校验哈希、端口和字段存在性。无法证明时记录 unavailable，不按同名字段猜测。缺少字段契约的其他 owner 事实也属于 unavailable，而不是 complete 或无依赖。
- `lineage_item_relations` 统一存储 item 和 field 两种粒度，field 行保存两端字段名与结构快照 hash；唯一键包含两端字段身份。observation 保存对应快照和转换语义。字段不是新增 DataItem，不新增第二套 collector。
- replace 在一次目标更新中关闭旧字段入边，即使两端仍是同两张表、字段映射已经改变；append/upsert 合并。未知写入模式只保存证据。迟到执行与重复采集不能恢复已关闭关系。字段快照不匹配时禁止跨版本串接；当前结构变化后旧快照关系标记 stale，重新扫描不重新激活。
- 统一 `GET /lineage/graph` 增加 `subject_kind=field_ref`、`field_name` 和可选 `schema_snapshot_hash`；省略 hash 时依据 Meta 当前结构定位，指定 hash 时使用对应执行证据。字段图按字段身份沿固定方向遍历，保留租户、深度、数量和端点完整性约束；as_of 沿冻结快照查询已观察的写入事实，stale 标记仍保留在历史关系上，不因当前结构变化删除历史来源，也不反推未观察的外部变更时间；首期字段视图不接受数据项 ID 的局部展开参数。字段视图返回 `field_lineage_status=complete|unavailable`，complete 且无入边可表示已证明的 generated 字段；unavailable 不等于没有来源。
- 数据项主体可使用 `granularity=field` 一次查询表内字段图。Meta 按根结构顺序播种全部字段（受统一 limit 限制），共用字段引用的有向遍历与批量取证；字段边批量取证只读取匹配关系的最新证据摘要，不为每条边重复传输完整源、目标结构快照，根结构仍由不可变执行证据校验；不逐字段查询或合并多张独立图。根字段分别返回 `field_lineage_status=complete|unavailable`；完整但没有来源边的字段仍保留。历史结构 hash 必须由执行证据证明。响应 subject 仍是根数据项，nodes 是字段引用，根结构 hash 随 subject 返回。
- Manager 数据探查直接进入血缘页只读取 Meta 元数据与血缘事实，不隐式请求数据预览；切换到预览页才读取数据，并执行原有数据读取授权检查。资源树的深度刷新只更新元数据；刷新完成后仅当前预览页重读数据，血缘页不要求数据读取 Grant。浏览器历史恢复遵循同一按需加载规则。
- `common-frontend/graph` 统一管理数据项级/字段级切换，字段视图按所属数据项和结构快照分组为表卡片，字段行之间连线。点击字段在已加载图中聚焦上下游关系，清除聚焦恢复全图，不重新请求每个字段。宿主只传入是否支持字段视图及粒度，并请求同一 API。字段名称作为精确标识传递，不拆分点号。节点详情提供字段名、所属数据项及结构快照 hash，关系详情提供转换语义和执行证据。删除原逐字段下拉查询交互。字段表卡采用紧凑的左右布局，尺寸、字段行锚点和绘制区域保持一致；适应窗口必须完整显示已加载图，字段文字按可用宽度截断，悬停和详情显示精确全名，不用最小缩放裁掉上游来代替全图。典型四表三跳图在 820px 画布适应窗口后，字段文字至少为 11px。默认进入优先保留满足这一字号的全图；更大图才聚焦当前表，并允许缩放和平移。
- 字段图的搜索、拖拽和自动布局统一由 `common-frontend/graph` 提供：搜索只筛选已加载根字段的入口，不裁掉图中字段或关系，字段名仍按精确标识选中；点击入口定位该字段并聚焦其上下游，字段文字低于 11px 时恢复到这一可读字号，已有更大缩放保持不变。此要求同样适用于自动布局或适应窗口之后的字段定位。血缘连线统一采用水平三次贝塞尔曲线，控制点仅由当前两端锚点计算，同高度端点自然呈直线；不保留布局生成的折线路由，不增加样式切换配置。方向箭头、交叉隔离、悬停及选中高亮、精确关系证据保持一致。表卡可拖拽，字段行锚点与曲线随表卡移动；自动布局复用共享 DAG 能力，按表卡实际宽高重新排列、重算曲线并适应窗口，保留当前字段或关系选择。布局坐标仅属于当前浏览会话，不写入血缘事实或后端。适应窗口只改变视口，不撤销拖拽；图数据重新加载时重新布局，主题或语言切换保留坐标、视口、搜索和当前选择。四表以内优先维持阅读字号，更多表时扩大层间距。多分支图为连线保留层间和同层空间。
- 图形模型直接由当前粒度的投影生成。字段级只为表卡生成节点展示模型，不能先为全部字段生成独立节点模型再丢弃；每条字段边保留精确原始关系和锚点，单次映射生成展示样式，不逐边扫描完整边列表。该优化不减少字段事实、关系数量或执行证据，也不建立另一套缓存或渲染路径。

- 字段表卡支持单表及全部表的收起/展开。收起时显示字段数量，字段身份和关系数量保持不变；该表的字段连线临时对齐摘要行，不聚合或删除字段事实，点击关系仍可核对精确两端及执行证据。展开恢复字段行锚点。手动折叠仅调整表卡高度，保持左上角、手动坐标、缩放及当前选择，相关连线重算；需要重新排列时显式使用自动布局。通过字段入口选择字段时，自动展开该字段上下游链路所属表，其他表保持原折叠状态；入口触发展开时复用共享 DAG 能力，按展开后的实际表卡宽高重新排列，再按上述可读字号要求定位所选字段，避免紧凑布局中的长表展开后互相覆盖。未触发展开的字段选择不重新排列。主题、语言及全屏切换保留折叠状态；重查图数据后恢复全部展开。此交互仅由共享 LineageViewer 拥有，不增加 API、后端事实或持久化偏好。

- 血缘查看支持浏览器原生全屏，包含工具栏、字段入口、画布及详情；Console 沿用现有 iframe 全屏授权。进入、退出和 Esc 退出不重建图，不重查接口，不重新布局，保留节点坐标、缩放、搜索及当前选择；画布随容器尺寸变化。适应窗口由用户显式触发。全屏生命周期由 Basic 共享 composable 统一管理，Office 预览复用同一实现；下拉菜单和提示框留在全屏元素内。

#### 验收与证据

字段级首期复用 `transfer-relational-sql-etl` Hosted T4。现行场景、身份与清理边界、性能采样口径及历史通过证据统一见[测试与验收规范](addp测试与验收规范.md#字段级血缘验收)。确定性测试与本地只读复核不能替代真实 T4，历史验收不扩大本节已声明的 opaque / unresolved、continuous / CDC 等支持边界。

### 6.2 图数据库评估边界

字段级粒度本身不构成引入 Neo4j / FalkorDB 的理由。PostgreSQL 继续唯一拥有血缘证据和当前投影。现有有界上下游查询先优化方向索引、批量取证与查询计划；不能用图数据库掩盖缺失或错误的字段事实。

字段图在每批遍历中统一读取最新关系证据，不逐边查询。证据必须按租户、关系类型、两端字段及结构快照精确匹配，并按 `observed_at`、证据 ID 降序确定唯一记录；历史查询先限定 `as_of` 再选取最新证据，不得引用时间点之后的观察。

只有实际负载已需要高并发深层遍历、复杂路径模式或全图算法，并且 PostgreSQL 优化后仍无法达到已确定的延迟、吞吐和内存目标，才进行同一真实数据集的对照评测。评测同时计入快照/历史语义、租户可见性、一致性恢复和运维成本，不以固定边数作为迁移阈值。若评测确认有收益，须另行确认唯一查询路线与可重建投影方案，禁止向 PostgreSQL 和图数据库双写事实或并行保留两套正式查询实现。

UDBX Dataset、GeoPackage layer 等容器内部对象只有在数据项体系为其确定稳定可寻址身份后，才能进入正式资源血缘。

## 七、统一查询 API

Meta 提供唯一的图查询入口：

```http
GET /api/v1/meta/lineage/graph
```

查询参数：

| 参数 | 说明 |
| --- | --- |
| `subject_kind` | `data_item`、`field_ref` 或 `published_service` |
| `granularity` | `item` / `field`；data_item 默认 item，field_ref 默认且仅支持 field，published_service 仅支持 item。data_item + field 返回根表全部字段的有界图。响应带同名粒度。 |
| `item_id` | `subject_kind=data_item` 或 `field_ref` 时必须提供 |
| `field_name` | `subject_kind=field_ref` 时必须提供精确字段名；其他主体不接受此参数，字段名中的点号不拆分 |
| `schema_snapshot_hash` | 仅字段粒度使用；省略时依据 Meta 当前结构定位，提供时必须有对应执行结构证据 |
| `service_id` | `subject_kind=published_service` 时使用 |
| `revision` | 服务发布版本，服务根节点必须明确版本 |
| `direction` | `upstream` / `downstream` / `both`，默认 `both` |
| `depth` | 展开深度，服务端限制最大值 |
| `expand_upstream` / `expand_downstream` | 逗号分隔的 data item ID；在根主体对应方向已可达的节点处额外展开一层，最多各 100 个。不作为新的根，不能借此进入旁系。字段粒度不接受这两个参数。 |
| `limit` | 节点和边分别计数的上限，根字段与关联字段都计入节点额度。item 粒度默认 100、范围 1–500；field 粒度默认 5000、范围 1–5000。消费方省略时由 Meta 按粒度确定默认值，不固定沿用数据项图的 100。超过时返回 `truncated=true`，截断不能解释为字段没有来源。 |
| `as_of` | 可选历史观察时间 |

响应使用直接图结构，不新增 `{code,message,data}` 包装：

```json
{
  "granularity": "item",
  "subject": {
    "kind": "data_item",
    "item_id": 22,
    "item_fingerprint": "...",
    "engine_id": 21,
    "engine_name": "SuperMap SDX+ for PostgreSQL",
    "full_name": "sdx.farmland"
  },
  "nodes": [],
  "edges": [],
  "truncated": false,
  "as_of": "2026-08-07T00:00:00Z"
}
```

节点可以包含 `data_item`、`published_service`、`execution` 和 `field_ref`，但资源身份和执行身份必须保持不同。`data_item` 节点必须返回所属 `engine_id` 和 System 当前的 `engine_name`，用于在同名 schema / table、跨引擎派生等场景中明确资源边界；共享前端不得解析 locator 或调用其他模块补猜引擎名称。边必须返回 relation kind、granularity、evidence summary 和时间状态。

字段图的每条边只投影对应观察中的 `execution_id`、`write_mode`、`field_lineage_status` 和 `transformation` 证据摘要；缺失值不补造。整份 operation 的 `field_mappings` 与结构快照仍由不可变执行／观察事实保留，不重复放入每条字段边的响应。批量取证在数据库查询中只读取这些摘要和观察时间，避免宽表按字段数平方重复传输和解码。500 列、两跳、1500 节点与 1000 条边的标准宽表夹具完整 JSON 响应应小于 4 MiB；不得通过丢弃节点、关系或证据身份满足该预算。

当前图响应必须保持结构闭合：每条 edge 的 source 和 target 都必须存在于同一响应的 nodes 中。当前已软删除的 data item 不进入 nodes，其相关 `stale` 投影也不进入当前 edges；历史证据通过 observation 和后续历史视图查询，不得以缺失端点的边混入当前图。

图查询以当前主体为根，`upstream` 只沿输入方向追溯，`downstream` 只沿输出方向展开，`both` 为这两种有向遍历的并集；遍历中不能改变方向进入共同上游的其他产物或共同下游的其他输入。其他主体之间真实存在的事实继续保留，不能因为不在当前视图中而删除。每条资源派生或服务依赖边计一层，`depth=0` 只返回主体（数据项主体的字段粒度返回根结构字段）；服务是下游终点，以服务为主体时可沿依赖继续追溯数据项。租户和未删除端点约束在每一层遍历时执行。超出节点或边上限时保留根及已连通部分，并返回 `truncated=true`。

该 API 必须执行 Tenant、Meta lineage read Permission 和 owner 资源可见性校验。不得因为用户能看到某个服务，就自动泄露该服务无权访问的上游数据项名称。

### 7.1 执行事实采集 API

真实读写 owner 在成功 execution 和 `lineage_facts` 持久化后，可通知 Meta 立即采集指定 execution：

```http
POST /api/v1/meta/lineage/executions/{execution_id}/collect
```

该接口是模块间内部 API，不是前端查询入口。当前 Develop 使用 `addp-develop` Service Principal，并要求 `meta.lineage.create` Permission。成功响应直接返回：

```json
{
  "observed": 1,
  "skipped": 0
}
```

立即采集与周期 collector 必须共同调用 `LineageService.CollectExecution`；通知失败不得改变成功 execution，周期 collector 负责漏采和失败重试。Item refresh 不调用该接口。

本规范不保留旧计划中的 `/upstream`、`/downstream`、`/path`、`/impact` 多套并行入口；这些能力由 `direction`、`depth`、`as_of` 和统一图结构表达。

## 八、共享前端边界

血缘 API 和关系事实归 Meta；血缘查看器归 `common-frontend/graph`，不是 Manager 私有组件。

共享组件至少包括：

- `LineageViewer.vue`。
- lineage DTO 类型和标准化函数。
- 注入宿主认证 API client 的 `createLineageApi` 或 composable。
- 中英文 i18n 消息。

共享组件只负责展示、交互和节点事件，不负责权限、Token、业务路由、Service/Asset DTO 解析或 Meta 数据刷新。Manager、Catalog、Service、Asset、Portal 通过宿主页面传入根主体和导航回调，集成同一个查看器。Catalog 必须使用当前 User Access Token 直接查询 Meta，不得使用 Catalog Service Principal 扩权代查。

组件放在 `common-frontend/graph`，不放入 `basic`；消费模块按需声明 G6 依赖，保持 Vue 单实例和共享前端无自有 `node_modules`。

Manager 血缘画布填满标签页可用空间，并随容器宽高变化调整。默认显示上下游各两层，工具栏提供 1、2、3、5、10、20 层的有界选择，层数变化由宿主请求同一图 API 并清空局部展开。节点的 `hidden_upstream_count` / `hidden_downstream_count` 只统计当前根有向血缘范围内尚未显示的直接邻居；点击节点查看详情，点击其方向提示追加一层。累计展开仍受最大 20 层及节点、边数量上限约束，保留视图缩放和被展开节点的位置。连线交叉使用背景隔离描边，选中或悬停连线时突出该连线及端点；截断状态必须可见，详情支持完整名称和证据查看。存在 execution 证据时由宿主使用统一 Monitor 导航打开来源执行。

Service 发布事实同时传递人类可读的 `service_name` 和单调递增的 owner 更新时间 `service_updated_at`；Meta 在依赖投影保存该名称和时间，不从版本哈希推断名称。较新的发布事实关闭该服务其他版本的当前依赖，较旧的通知不得覆盖新版本，历史 observation 保留。Service 定期重放自身现有服务发布事实，以补齐名称并重试漏发；非 active 服务重放空依赖。当前图服务卡片以名称为标题，ID 和版本只在详情展示。

## 九、当前状态与后续边界

阶段 1 已完成并验证：

1. 关系表、关系证据和当前投影位于 Meta schema，PostgreSQL 是唯一事实源。
2. Transfer / Develop 的资源级 `lineage_facts`、Meta collector、幂等投影和历史证据闭环已落地。
3. Develop 成功事实落库后立即通知 Meta；周期 collector 负责漏采和失败重试。
4. Meta 统一图查询 API、租户 / 权限校验和 `common-frontend/graph` 查看器已具备。
5. Service 发布版本的 `serve` 依赖事实已纳入模型和 API 契约。
6. 字段级首期的执行证据、时态投影、整表字段图、500 列宽表及 Provider 可证明的单来源和多来源查询已有对应 [Hosted T4 历史证据](addp测试与验收规范.md#历史证据索引)；支持边界以 6.1 为准。

后续能力必须先更新本规范和术语表，再单一路线实现：

- SQL 方言的字段级自动解析；Transfer 显式 field mapping 的首期范围见 6.1。
- SQL 方言解析及无法可靠解析时的“不完整依赖”表达。
- UDBX Dataset 等容器内部对象的稳定 data item 身份。
- Model / Quality 独立关系图层。

验收持续覆盖：幂等、重试、软删除恢复、replace/append/upsert 时态、延迟扫描、失败/取消、执行清理后的证据保留、多租户权限和深度查询性能。

## 十、相关文档

- [ADDP 数据项体系图](../concepts/addp数据项体系图.md)
- [ADDP 任务体系规范](addp任务体系规范.md)
- [ADDP 数据服务体系图](../concepts/addp数据服务体系图.md)
- [ADDP 路径统一和指纹计算](addp路径统一和指纹计算.md)
