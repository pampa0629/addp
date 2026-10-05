# ADDP 数据血缘能力规范

**状态**：正式规范（数据项级已实现，字段级首期范围见 6.1）
**更新时间**：2026-10-03
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

### 5.1 固定正式表查询写入

Develop Query Execution Supervisor 必须在业务写入事务成功提交后，将 `lineage_facts` 与成功终态在同一次带 lease 校验的执行记录事务中保存，再复用已有通知入口调用 Meta collector。输入采用 Provider 冻结并在写入事务中复核的完整 ReadSet，按 canonical path 稳定排序。命中 relation 参数绑定的来源保留 input.<参数名> 端口，View 展开的其他实际来源使用 read.<序号> 端口；不得仅靠调用方的绑定列表遗漏间接来源；目标采用本次实际写入的 `target_locator`。不得从 Orchestrator 的 `depends_on` 推导资源边，也不得将逻辑表建表任务作为数据来源。

覆盖计算的 `overwrite` 映射为血缘 `replace`；`append` 保留为 `append`。失败、取消、失去 lease 或终态保存失败时不通知采集、不建立成功关系；通知失败保留已落库的成功事实，由 Meta 周期 collector 重试。业务库提交与平台执行记录提交不是跨库事务，二者之间进程中断的执行不能自动冒充成功或补造血缘。

这一路径由 TableResultProvider 证明同 Engine 现有表写入的实际读取集合、源/目标实时结构和字段位置映射；Develop 记录完整字段事实或明确的 unavailable 状态，不在 Owner 复制 SQL 依赖解析，也不扩大为任意写 SQL/DDL 的推断。历史缺少事实的执行不能由 Meta 反向猜测；通过新的真实执行生成证据。

## 六、粒度

第一阶段支持 data item 级别，而不是仅支持关系型 table。字段级模型从一开始预留，但分阶段实现：

1. 先完成 table / file / object / view / collection / graph / whole dataset 的 data item 级闭环。
2. Transfer 显式 field mapping 先产生字段级关系。
3. 再按引擎方言由 Provider 证明 SQL 输出来源，并结合其实时结构事实处理 CTE、别名、`*` 和表达式。

字段不是默认的独立 data item，字段引用使用 `item_id + field_name + schema_snapshot_hash`，完整结构快照保存在执行事实和不可变证据中。无法可靠解析的 SQL 不得保存猜测边，也不使用任意浮点 `confidence` 伪装确定性。

### 6.1 字段级首期契约

首期支持 Transfer bounded native table -> native table，以及同一 PreparedQuery 能证明完整输出映射的 query source -> native table。多来源查询必须通过已有 `source.query.inputs[]` 声明全部输入，并与 Provider 的规范 ReadSet 精确一致；端口按原生路径绑定，不依赖输入声明与 Provider 返回来源的顺序。每个来源分别冻结原始结构，同一结果字段的多个值来源分别记录映射，仅参与行筛选的输入仍保留资源及结构事实，不生成字段值来源边。该扩展不改变 Console 单表基础查询构建器的职责。字段血缘回答目标字段值来自哪些源字段；过滤、JOIN 条件、排序及行数影响不混入值来源关系。查询源只消费 Provider 的 ReadSet / OutputLineage，不在 Transfer 解析 SQL/MQL；MongoDB 透明 aggregate 的嵌套字段别名、`$ifNull: [field, null]` 和 `$unwind.includeArrayIndex` 按 Provider 证据与实际 field mapping 组合，数组索引保留 derived 语义。opaque / unresolved 或缺少精确输出绑定的查询（包括当前接口不能证明的无来源结果列）、continuous/CDC、encoded 输出及空间重投影暂不声明完整字段血缘，继续记录数据项级事实，字段视图明确显示证据不可用。

PostgreSQL Provider 已能在同一 PreparedQuery 内组合非递归 CTE、派生表、JOIN、UNION 与表达式的字段值来源。该证明能力不替代执行 owner 的写入事实：Develop 的同 Engine 现有表写入消费 TableResultProvider 冻结计划，在成功事务返回的实时源/目标结构上记录 schema snapshot 与实际位置映射。CTE、JOIN、UNION 等可证明查询支持多来源字段血缘；不透明查询仍标记 unavailable。此能力必须通过真实执行验收，不能仅凭查询解析测试声明部署后的 DWD 血缘已经可用。

- `lineage_facts` 保持 `addp.lineage-facts/v1`；资源引用的 `schema_snapshot` 使用 `{hash, fields}`，fields 来自同一次真实读取和目标写入结构，hash 复用 Common 的 `TableSchemaSnapshotHash`。不回查当前结构补造执行快照。
- 查询源冻结 Provider 的原始来源结构及精确路径绑定，查询结果列不能充当原始来源快照。嵌套字段只按快照中的唯一字段名引用，完整 path 参与结构哈希；点号不会被 collector 或查询 API 再次拆分。查询保护准备同时返回实际受转换影响的结果字段名，与同次保护规则绑定；这些字段经过别名及后续 field mapping 后仍须记为 derived，抑制列不产生映射。
- operation 增加 `field_lineage_status=complete|unavailable` 和 `field_mappings`。每条映射明确 `input_port`、`output_port`、`source_field`、`target_field` 和 `transformation=direct|derived|generated`。generated 没有输入端口或源字段，只证明常量/默认值生成，不生成虚假的字段来源边。非空默认值会改变源值语义，记为 derived；数据保护准备阶段同时返回实际受转换影响的字段名，脱敏字段记为 derived，受抑制字段不产生目标映射；按运行时顺序组合多步映射与覆盖，透传字段来自实际源结构。
- 成功执行才采集；字段快照与映射必须校验哈希、端口和字段存在性。无法证明时记录 unavailable，不按同名字段猜测。缺少字段契约的其他 owner 事实也属于 unavailable，而不是 complete 或无依赖。
- `lineage_item_relations` 统一存储 item 和 field 两种粒度，field 行保存两端字段名与结构快照 hash；唯一键包含两端字段身份。observation 保存对应快照和转换语义。字段不是新增 DataItem，不新增第二套 collector。
- replace 在一次目标更新中关闭旧字段入边，即使两端仍是同两张表、字段映射已经改变；append/upsert 合并。未知写入模式只保存证据。迟到执行与重复采集不能恢复已关闭关系。字段快照不匹配时禁止跨版本串接；当前结构变化后旧快照关系标记 stale，重新扫描不重新激活。
- 统一 `GET /lineage/graph` 增加 `subject_kind=field_ref`、`field_name` 和可选 `schema_snapshot_hash`；省略 hash 时依据 Meta 当前结构定位，指定 hash 时使用对应执行证据。字段图按字段身份沿固定方向遍历，保留租户、深度、数量和端点完整性约束；as_of 沿冻结快照查询已观察的写入事实，stale 标记仍保留在历史关系上，不因当前结构变化删除历史来源，也不反推未观察的外部变更时间；首期字段视图不接受数据项 ID 的局部展开参数。字段视图返回 `field_lineage_status=complete|unavailable`，complete 且无入边可表示已证明的 generated 字段；unavailable 不等于没有来源。
- 数据项主体可使用 `granularity=field` 一次查询表内字段图。Meta 按根结构顺序播种全部字段（受统一 limit 限制），共用字段引用的有向遍历与批量取证；不逐字段查询或合并多张独立图。根字段分别返回 `field_lineage_status=complete|unavailable`；完整但没有来源边的字段仍保留。历史结构 hash 必须由执行证据证明。响应 subject 仍是根数据项，nodes 是字段引用，根结构 hash 随 subject 返回。
- `common-frontend/graph` 统一管理数据项级/字段级切换，字段视图按所属数据项和结构快照分组为表卡片，字段行之间连线。点击字段在已加载图中聚焦上下游关系，清除聚焦恢复全图，不重新请求每个字段。宿主只传入是否支持字段视图及粒度，并请求同一 API。字段名称作为精确标识传递，不拆分点号。节点详情提供字段名、所属数据项及结构快照 hash，关系详情提供转换语义和执行证据。删除原逐字段下拉查询交互。字段表卡采用紧凑的左右布局，尺寸、字段行锚点和绘制区域保持一致；适应窗口必须完整显示已加载图，字段文字按可用宽度截断，悬停和详情显示精确全名，不用最小缩放裁掉上游来代替全图。典型四表三跳图在 820px 画布适应窗口后，字段文字至少为 11px。默认进入优先保留满足这一字号的全图；更大图才聚焦当前表，并允许缩放和平移。
- 字段图的搜索、拖拽和自动布局统一由 `common-frontend/graph` 提供：搜索只筛选已加载根字段的入口，不裁掉图中字段或关系，字段名仍按精确标识选中；点击入口定位该字段并聚焦其上下游。血缘连线统一采用水平三次贝塞尔曲线，控制点仅由当前两端锚点计算，同高度端点自然呈直线；不保留布局生成的折线路由，不增加样式切换配置。方向箭头、交叉隔离、悬停及选中高亮、精确关系证据保持一致。表卡可拖拽，字段行锚点与曲线随表卡移动；自动布局复用共享 DAG 能力，按表卡实际宽高重新排列、重算曲线并适应窗口，保留当前字段或关系选择。布局坐标仅属于当前浏览会话，不写入血缘事实或后端。适应窗口只改变视口，不撤销拖拽；图数据重新加载时重新布局，主题或语言切换保留坐标、视口、搜索和当前选择。四表以内优先维持阅读字号，更多表时扩大层间距。多分支图为连线保留层间和同层空间。

- 字段表卡支持单表及全部表的收起/展开。收起时显示字段数量，字段身份和关系数量保持不变；该表的字段连线临时对齐摘要行，不聚合或删除字段事实，点击关系仍可核对精确两端及执行证据。展开恢复字段行锚点。手动折叠仅调整表卡高度，保持左上角、手动坐标、缩放及当前选择，相关连线重算；需要重新排列时显式使用自动布局。通过字段入口选择字段时，自动展开该字段上下游链路所属表，其他表保持原折叠状态；入口触发展开时复用共享 DAG 能力，按展开后的实际表卡宽高重新排列，再保持当前缩放定位所选字段，避免紧凑布局中的长表展开后互相覆盖。未触发展开的字段选择不重新排列。主题、语言及全屏切换保留折叠状态；重查图数据后恢复全部展开。此交互仅由共享 LineageViewer 拥有，不增加 API、后端事实或持久化偏好。

- 血缘查看支持浏览器原生全屏，包含工具栏、字段入口、画布及详情；Console 沿用现有 iframe 全屏授权。进入、退出和 Esc 退出不重建图，不重查接口，不重新布局，保留节点坐标、缩放、搜索及当前选择；画布随容器尺寸变化。适应窗口由用户显式触发。全屏生命周期由 Basic 共享 composable 统一管理，Office 预览复用同一实现；下拉菜单和提示框留在全屏元素内。

首期跨模块验收复用 `transfer-relational-sql-etl` Online suite，唯一运行于 GitHub Hosted Ubuntu x86_64 临时部署，覆盖真实 Transfer 原生表字段映射、replace 与两跳自动采集，以及 Console 中 Manager 共享字段视图；原生写出后等待目标 DataItem 和当次字段证据收敛，不手工补扫目标。自动扫描持有命名空间锁时，额外手动扫描可能跳过该范围并结束，不能用这种执行的成功状态证明目标已经入库。同一 suite 验证 Provider 能证明的单来源 SQL 查询字段映射。每轮创建隔离身份与 PostgreSQL Engine Instance，退出时随整套部署销毁，不依赖永久账号或自托管 Runner。确定性脚本通过不等于真实 T4 通过，具体身份、夹具、报告和清理契约见《ADDP 测试与验收规范》5.2。

2026-10-04，首期 Hosted T4 [运行 37206323194](https://github.com/pampa0629/addp/actions/runs/37206323194) 首次完整通过，验证源码为 `8c0a1cc142f2bbbd8880745527c224fae7bd2b2c`。归档报告确认同一非管理员 User / Tenant 下的 direct、derived、generated 字段证据，replace 后旧入边关闭、结构快照一致的两跳查询，以及 Console 的整表字段图、字段聚焦和可证明单来源 SQL 查询字段血缘。四个临时任务全部删除并确认 404，物理夹具与隔离部署清理通过，Infra 零残留。该验收不扩大为任意 SQL/MQL、多来源 Transfer 查询或 opaque 输出的来源推断。

MongoDB → PostgreSQL ODS 的扩展验收沿用同一 suite 和权限边界：原生集合经透明 MQL `$project` 投影后显式映射六个字段（含嵌套点号字段），冻结源结构保留原始字段而非查询别名，连续执行同一个任务两次，自动扫描目标并自动采集字段血缘，核对当次冻结结构与最新执行证据，验证 Manager 整表字段图和本地字段聚焦。Business Fixture 在 Hosted 临时部署中拥有两个独占 tmpfs 容器与 ODS 完整行集合验证，退出统一检查零残留。该扩展须有新的真实 Hosted 通过证据，不能由前述 PostgreSQL 验收结果代替。

2026-10-05，MongoDB → PostgreSQL ODS 扩展 Hosted T4 [运行 37214989939](https://github.com/pampa0629/addp/actions/runs/37214989939) 完整通过，验证源码为 `e9d030996f664a7b9e31cd37d76e93f15dfaddaf`。同一最小权限 User / Tenant 下，同一个透明 MQL 投影任务连续执行两次，每次读写 3 条；六条活跃字段关系全部引用第二次成功执行，源结构保留原始嵌套字段名和 path，并与目标冻结结构、Meta 字段图一致。目标 DataItem 由 Transfer 自动扫描产生，字段事实由 Meta 自动采集，测试未手工补扫 ODS 或调用 collect。Console 的 Manager 整表字段图包含 12 个节点、6 条关系，日期与昵称字段在本地切换聚焦且只请求一次整表字段图，元数据刷新后关系仍有效。v3 报告与独立物理夹具验证通过，五个临时任务全部删除并确认 404，两个业务容器、平台 Infra 和凭据目录清理通过，零残留。

编排重跑验收继续扩展同一 suite：Orchestrator 调用真实 Transfer ODS 任务和两个 Develop relation 查询任务，依次完成 ODS → DIM 日期转换 → DWD JOIN 写入；同一个编排连续执行两次。每个子执行通过既有 Monitor 安全投影核对当次父 UUID、模块与任务身份，字段及资源边只引用实际 Transfer / Develop 子执行，不得由编排依赖生成。Develop 冻结 ReadSet 采用不带目录选择器 item_id 的规范资源路径，按同一 Engine 与原生路径核对 relation 参数绑定。Develop 专业执行详情复用安全执行投影，不暴露 operations 和 schema_snapshot；验收从该接口核对资源引用、顶层 outputs 与行数，从 Meta 字段图核对映射及冻结结构哈希，跨字段、跨 owner 和重跑必须保持结构身份一致。分别核对精确字段来源、direct/derived、所有冻结结构、三跳日期追溯、最新活跃入边和稳定 DataItem 身份；Manager 必须展示重跑后的整表字段图，Business Fixture 独立验证 DIM、DWD 完整行集合。此扩展的确定性测试不能代替新的真实 Hosted T4 证据。

2026-10-05，编排整链路扩展 Hosted T4 [运行 37225393122](https://github.com/pampa0629/addp/actions/runs/37225393122) 完整通过，验证源码为 `194e0d0ef4f74f534452a81298e47ee5706f6ae6`。同一最小权限 User / Tenant 下，同一个 Orchestrator 编排连续执行两次 MongoDB → PostgreSQL ODS → DIM → DWD；两个父执行与六个实际子执行 UUID 全部不同，每轮 DIM 写入 3 行、DWD 写入 2 行。六条 ODS、两条 DIM 和四条 DWD 字段映射均引用当次真实 Transfer / Develop 子执行，第二轮仅保留最新活跃证据；日期字段可三跳追溯到原始 `title.date`，昵称改名为 direct、强度表达式为 derived，中间表结构哈希与 DataItem 身份保持一致，编排父执行不生成资源或字段边。两轮 API 检查无需手工补扫 ODS 或调用 collect。Console 的 Manager 整表字段图、日期与昵称本地聚焦、恢复全部字段及适应窗口均通过；ODS 整表图包含 18 个字段节点、12 条关系，DWD 三层图包含 13 个字段节点、9 条关系。v4 报告与独立物理夹具验证确认完整行集合及真实 date 列；八个临时任务和编排定义全部删除并确认 404，两个业务容器、平台 Infra 与凭据目录清理通过，零残留。

结构演进验收继续沿用同一编排和任务定义：Business Fixture 在 Hosted 独占 PostgreSQL 容器中将 DWD 的 `person_nickname` 改为 `person_display_name`，将 `activity_date` 从 date 改为 timestamp without time zone，执行正式 Meta 扫描。新写入之前，当前字段图必须声明 unavailable 且无旧关系，旧字段名不再作为当前字段存在；冻结旧 hash 与第二轮父执行完成时间的历史图仍引用第二轮子执行，并保留 stale 状态。随后第三次执行原编排，Provider 按实际目标列位置写入，昵称映射使用新物理列名，日期赋值转换变为 derived；新 DWD 结构 hash 必须变化，源集合、ODS、DIM 的 hash 和所有 DataItem 身份保持不变。第三轮当前图只引用当次子执行，旧结构历史图仍保留原字段、原转换语义和已关闭关系。Manager 展示新结构的整表图，夹具独立核对完整行集合、改名列和真实 timestamp 类型。该验收不更新任务定义、不扩大消费 User 权限，不向 Meta 手工写入结构或血缘事实。

2026-10-05，结构演进扩展 Hosted T4 [运行 37247522460](https://github.com/pampa0629/addp/actions/runs/37247522460) 完整通过，验证源码为 `2b5f1c1437a362e2b7f513c41dd6cad1f180d2b0`。同一最小权限 User / Tenant、同一个编排和任务定义完成三轮写入，共三个父执行与九个实际子执行 UUID，每轮 DIM 三行、DWD 两行。第二轮后，夹具真实改名 DWD 昵称列并将 date 改为 timestamp without time zone，正式扫描后四张当前字段图均声明 unavailable、各仅一个根节点且无来源边，旧字段名查询返回 404；冻结旧 hash 与第二轮父完成时间的四张历史图保留原字段、原 direct 日期关系及第二轮子执行证据，DWD 入边为 stale。第三轮自动采集的新字段关系使用新列名，日期赋值转换为 derived，DWD hash 变化，源集合、ODS、DIM 的 hash 与全部 DataItem 身份保持稳定；原历史图的 DWD 入边转为 closed，观察时间仍不晚于查询时间，不混入第三轮证据。v5 报告保存写入前的当前图及写入前后的历史图，独立归档核验、完整物理行集合、新列与 timestamp 类型、Manager 三层整表字段图及本地聚焦全部通过。八个定义删除并确认 404，两个业务容器、平台 Infra 与凭据目录清理通过，零残留。相同源码的 Platform CI 30/30、Release/T2 的 34 个实际执行任务与 Quality 前端门禁通过；另 4 项 CLI 发布及 Keychain / 官方介质认证任务按 workflow 条件跳过，不计为通过。

2026-10-05，字段图紧凑布局的 Hosted 复验[运行 37252213496](https://github.com/pampa0629/addp/actions/runs/37252213496) 已通过，验证源码为 `680ce20325a7a20cddeadd6889dafbacb8768e6b`。Console 中真实 MongoDB → PostgreSQL ODS → DIM → DWD 三跳整表图包含 13 个字段节点、9 条关系；适应窗口后所有字段行都在画布内，Canvas 实际记录的最小字段字号为 14.22px，画布为 820×667px。截图与 `transfer-field-overview-layout.json` 同时归档。既有字段映射、重跑、结构演进和冻结历史取证回归同时通过；八个临时定义删除并确认 404，业务夹具和部署清理通过、Infra 零残留。同源码 Platform CI 30 项和 Release/T2 的 32 个实际执行任务通过；另 6 项按 workflow 条件跳过，不计为通过。

2026-10-05，字段曲线、拖拽与全屏 Hosted 复验[运行 37276985688](https://github.com/pampa0629/addp/actions/runs/37276985688) 已通过，验证源码为 `7665c0b612d83d31a9a418d94af070d321e1bd79`。真实 MongoDB → PostgreSQL ODS → DIM → DWD 整表图绘制 13 个字段、9 条水平三次贝塞尔连线，820×667px 画布内最小字段字号为 13.96px。目标表向左、向下各拖拽 30px 后，昵称字段的曲线端点偏差为 0.85px，满足 2px 精度要求；自动布局、收起/展开、搜索定位及 Console iframe 原生全屏进出均通过。Canvas 观察工具等待绘制帧后统一读取文字和连线，保留原有位移及端点断言，修正适应窗口后读取旧帧坐标的验收时序。三轮编排、九个实际子执行、结构演进与冻结历史取证、独立物理行集合及列类型检查同时通过；八个临时定义删除，部署清理通过、Infra 零残留。同源码 [Platform CI 37276976663](https://github.com/pampa0629/addp/actions/runs/37276976663) 的 24 个门禁任务实际执行通过，另 6 个按路径条件跳过；Manager 在 Linux Runner 上通过 279 项单元测试、96 项浏览器测试及构建。

Transfer 多来源查询扩展沿用 `transfer-relational-sql-etl` 手工 Hosted T4 与既有最小权限身份。在原生源及已完成 replace 的映射表上声明两个 query.inputs，由真实 CTE、JOIN 与 UNION ALL 生成四行结果；冻结每个原始来源结构，验证一个结果字段同时来自两张表的精确 derived 映射、仅用于 JOIN 的字段不产生值来源边、Meta 自动采集及三层冻结结构一致性。Business Fixture 独立核对目标完整行集合与列顺序；整套物理结果验证由 Hosted 生命周期在浏览器与多来源场景全部完成后统一执行，浏览器不提前调用同一验证入口。新增任务进入既有本轮定义清理，报告使用 v6。Transfer PostgreSQL T2 同时覆盖三个输入（含只参与行筛选的来源）、查询改名、类型转换和完整物理行集合。扩展须有新的真实 Hosted 通过证据，不能由此前单来源或 Develop 多来源结果代替。

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
| `limit` | 节点和边上限，超过时返回 `truncated=true` |
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
6. 字段级首期的执行证据、时态投影、整表字段图与 Provider 可证明的单来源查询已通过上述 Hosted T4；支持边界以 6.1 为准。

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
