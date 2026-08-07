# ADDP 数据血缘能力规范

**状态**：正式规范（阶段 1 实现依据）  
**更新时间**：2026-08-07  
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
| `field_ref` | `data_item + field_name + schema_snapshot` | 数据项内部字段，第一阶段只预留模型 |

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

第一阶段使用 Infra PostgreSQL 的普通关系表和递归 CTE：

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
| Develop | 持久化 Workflow 的资源输入输出；写 SQL/DDL 后续接入 |
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
  -> Meta collector claim
  -> locator/fingerprint resolve
  -> Meta scan completion (if target is new)
  -> observation
  -> current projection
```

正式关系只在成功完成的持久化效果可确认后建立。部分提交、失败和取消必须由 owner 提供明确提交事实，否则只记录诊断，不建立成功关系。

## 六、粒度

第一阶段支持 data item 级别，而不是仅支持关系型 table。字段级模型从一开始预留，但分阶段实现：

1. 先完成 table / file / object / view / collection / graph / whole dataset 的 data item 级闭环。
2. Transfer 显式 field mapping 先产生字段级关系。
3. 再按引擎方言实现 SQL 字段解析，并结合 Meta 字段事实处理 CTE、别名、`*` 和表达式。

字段不是默认的独立 data item，字段引用使用 `item_id + field_name + schema_snapshot`。无法可靠解析的 SQL 不得保存猜测边，也不使用任意浮点 `confidence` 伪装确定性。

UDBX Dataset、GeoPackage layer 等容器内部对象只有在数据项体系为其确定稳定可寻址身份后，才能进入正式资源血缘。

## 七、统一查询 API

Meta 提供唯一的图查询入口：

```http
GET /api/v1/meta/lineage/graph
```

查询参数：

| 参数 | 说明 |
| --- | --- |
| `subject_kind` | `data_item` 或 `published_service` |
| `item_id` | `subject_kind=data_item` 时使用 |
| `service_id` | `subject_kind=published_service` 时使用 |
| `revision` | 服务发布版本，服务根节点必须明确版本 |
| `direction` | `upstream` / `downstream` / `both`，默认 `both` |
| `depth` | 展开深度，服务端限制最大值 |
| `limit` | 节点和边上限，超过时返回 `truncated=true` |
| `as_of` | 可选历史观察时间 |

响应使用直接图结构，不新增 `{code,message,data}` 包装：

```json
{
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

该 API 必须执行 Tenant、Meta lineage read Permission 和 owner 资源可见性校验。不得因为用户能看到某个服务，就自动泄露该服务无权访问的上游数据项名称。

本规范不保留旧计划中的 `/upstream`、`/downstream`、`/path`、`/impact` 多套并行入口；这些能力由 `direction`、`depth`、`as_of` 和统一图结构表达。

## 八、共享前端边界

血缘 API 和关系事实归 Meta；血缘查看器归 `common-frontend/graph`，不是 Manager 私有组件。

共享组件至少包括：

- `LineageViewer.vue`。
- lineage DTO 类型和标准化函数。
- 注入宿主认证 API client 的 `createLineageApi` 或 composable。
- 中英文 i18n 消息。

共享组件只负责展示、交互和节点事件，不负责权限、Token、业务路由、Service/Asset DTO 解析或 Meta 数据刷新。Manager、Service、Asset、Portal 通过宿主页面传入根主体和导航回调，集成同一个查看器。

组件放在 `common-frontend/graph`，不放入 `basic`；消费模块按需声明 G6 依赖，保持 Vue 单实例和共享前端无自有 `node_modules`。

## 九、实施顺序与验收

1. 先完成本规范、术语表、任务体系、数据项体系和数据服务体系同步。
2. 定义 `lineage_facts`，完成 Transfer -> Meta 的资源级闭环。
3. 完成统一图查询 API、租户/权限测试和历史投影测试。
4. 在 `common-frontend/graph` 实现查看器，先集成 Manager，再集成 Service 和 Asset。
5. 接入 Develop Workflow、Service SQL 依赖和其他业务产物。
6. 最后实现字段级血缘及 Model/Quality 等独立关系图层。

验收必须覆盖：幂等、重试、软删除恢复、replace/append/upsert 时态、延迟扫描、失败/取消、执行清理后的证据保留、多租户权限和深度查询性能。

## 十、相关文档

- [ADDP 数据项体系图](../concepts/addp数据项体系图.md)
- [ADDP 任务体系规范](addp任务体系规范.md)
- [ADDP 数据服务体系图](../concepts/addp数据服务体系图.md)
- [ADDP 路径统一和指纹计算](addp路径统一和指纹计算.md)
- [工作流运行时结果产物与血缘专题](../next/工作流运行时结果产物与血缘专题.md)
- [Meta 模块血缘扩展设计（历史方案）](../plan/meta模块血缘扩展设计.md)
