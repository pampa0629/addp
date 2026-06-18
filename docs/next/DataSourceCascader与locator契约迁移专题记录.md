# DataSourceCascader 退场与资源树选择器统一专题记录

> 状态：方向已确认，核心迁移已落地，稳定契约已同步到正式文档。本文记录 `common-frontend` 数据源级联选择器原契约、已暴露问题、统一到资源树选择器的设计判断和迁移结果。
>
> 正式规则入口：
> - `docs/spec/addp路径统一和指纹计算.md`：ResourceLocator、Meta resource-tree API、ancestors 回显、新资源 `parent_locator + name` 边界。
> - `docs/concepts/addp共享模块介绍.md`：共享资源树、locator 和 ResourceTreePicker 分工。
> - `common-frontend/README.md`：ResourceTreePicker 前端使用契约。

## 背景

`DataSourceCascader.vue` 是 `common-frontend/basic` 提供的共享数据源级联选择器，用于在多个业务模块中选择数据源资源。

它当前主要服务以下场景：

- `develop` 工作流算子参数选择数据源。
- `service` 查询服务、瓦片服务选择源表。
- 共享封装组件 `DataSourceCascaderCard`、`DataSourceCascaderDialog` 间接复用。

组件交互模型是：

```text
engine -> schema / database / bucket -> table / object / ...
```

选中资源后，组件通过 `update:selection` 向调用方抛出选择结果。

## 当前契约

### 输入契约

组件当前支持：

```js
initialEngineId: Number
initialSelection: {
  engine_id,
  schema,
  table
}
```

`initialSelection` 的加载流程是：

1. 按 `engine_id` 选中 engine。
2. 加载 engine 下一级资源。
3. 按 `schema` 在第一层资源中查找节点。
4. 加载 schema 下级资源。
5. 按 `table` 在第二层资源中查找节点。
6. 触发选择完成。

### 输出契约

组件选中节点后调用 `DataSourceAPI.extractDataSourceSelection(node)`，向外抛出 selection。selection 通常包含：

- `engine_id`
- `schema`
- `table`
- `fullName`
- `locator`
- 几何列相关字段，如 `hasGeometry`、`geometryColumn`、`srid`、`extent`

也就是说，当前输出里已经可能包含 `locator`，但输入预加载仍以 `{ engine_id, schema, table }` 为主。

## 已暴露问题

### 1. 资源身份仍是三段式旧契约

ADDP 当前资源定位主线正在收敛到 ResourceLocator。`DataSourceCascader` 的 `initialSelection` 仍以 `engine_id/schema/table` 定位资源，和 locator-only 方向不一致。

这会导致：

- 只能自然表达数据库表类资源。
- 对对象存储、文件系统、多级目录、集合等资源不够统一。
- 前端需要理解层级拆分规则。
- 对重新扫描、节点 ID 漂移、path 规则差异缺少统一后端事实源。

### 2. 预加载路径依赖前端逐级查找

当前预加载通过前端加载 children 后按 label 匹配 `schema/table`。这和资源树 locator 祖先链定位能力的方向不同：

- locator 定位应由后端返回祖先链事实。
- 前端不应自行解释 engine catalog path。
- 不同 engine 的 catalog 层级差异不应由通用组件硬编码。

### 3. 组件承担了过多资源模型假设

`DataSourceCascader` 同时处理：

- engine 过滤。
- 树层级加载。
- schema/table 式初始定位。
- table/object 可选节点判断。
- 空间表几何检测。
- 新表虚拟节点创建。

这些职责混在一起后，迁移 locator 契约时容易影响 `develop`、`service` 等模块。

### 4. 跨模块影响范围较大

当前直接或间接使用点包括：

- `develop/frontend/src/components/workflow/OperatorParamsPanel.vue`
- `service/frontend/src/views/TileServiceForm.vue`
- `service/frontend/src/views/TileServiceDetail.vue`
- `service/frontend/src/views/QueryServiceForm.vue`
- `common-frontend/basic/src/components/DataSourceCascaderCard.vue`
- `common-frontend/basic/src/components/DataSourceCascaderDialog.vue`

因此该迁移不应作为 Manager 资源树定位 API 的局部收尾直接完成。

## 与资源树 locator 祖先链专题的关系

[Manager 数据预览与资源树实现规范](../../manager/docs/数据预览与资源树实现规范.md) 已确认 Manager 资源树定位以 locator 为主事实源，并定义了资源树祖先链定位 API。

`DataSourceCascader` 的问题更偏向“共享数据源选择器契约”：

- 它不只是资源树定位。
- 它是跨模块表单输入组件。
- 它还承担空间表选择、服务发布、工作流参数选择等业务入口。

因此建议单独拆专题推进。

## 已确认方向

### 1. 不再把 DataSourceCascader 作为长期主线迁移

`DataSourceCascader` 不再继续演进为 locator 版通用组件。原因是 ADDP 已经形成资源树与 ResourceLocator 的统一主线，Manager 预览和 Transfer 资源选择已经验证该路线能够覆盖跨 engine、跨层级、node / item 定位、回显和跳转需求。

继续给 Cascader 增加 `initialLocator`、祖先链回显、对象存储路径、多级目录和空间能力检测，会让旧组件扩张成第二套资源树，并保留 `engine_id/schema/table` 旧模型惯性，不符合单一技术路线原则。

### 2. 新增或复用支持过滤的资源树选择器

后续跨模块资源选择统一走资源树选择器，暂定能力边界为：

- 以 `locator` 作为已有资源的唯一主身份。
- 支持按 engine type、node / item type、data type、format、capability 等条件过滤。
- 支持 `initialLocator` 回显，回显流程复用 Meta resource-tree ancestors API。
- selection 输出以 locator 为主，展示字段和执行派生字段分层表达。
- 前端不自行解释 schema、table、bucket、path、collection 等 catalog 层级。
- locator 解析、构造和校验归属共享层：后端使用 `common/resourcetree`，前端使用 `@addp/common-frontend`；Manager 只是 locator 的消费方之一，其他模块不得为了 locator 工具依赖 Manager。
- 资源树事实查询归属 Meta，包括 tree root、locator node、ancestors、search 和 refresh；Transfer、Service、Develop 不复制资源树定位实现。

统一组件命名为 `ResourceTreePicker`。当前不新增 `ResourceLocatorPicker` 等平行选择器封装，避免资源选择入口重复建设。

### 3. develop 工作流算子应使用资源树选择器

`develop` 工作流算子参数需要的是“快速选中数据项”，不是固定层级的数据库表选择器。

迁移方向：

- 输入资源保存 locator 作为主身份。
- 算子 UI 通过过滤条件限制可选资源，例如 `table`、`file`、`object`、空间资源或可表格化资源。
- 执行阶段需要的 `engine_id/schema/table/path` 等参数由后端或执行适配层从 locator 解析、校验和派生。
- 对输出到新表的场景，不构造虚拟 locator，改用目标创建契约。

### 4. service 发布类模块也应使用资源树选择器

`service` 查询服务、瓦片服务、OGC 发布等创建入口同样应以 locator 选择源资源。

迁移方向：

- 表单输入以 locator 为主身份。
- 服务执行配置中如需保留 `engine_id/schema/table/geometry_column/srid`，应作为后端解析 locator 后生成的执行快照。
- 前端可以展示这些派生字段，但不把它们作为并行定位路线提交。
- 空间能力、字段结构、对象表格化能力等由独立 capability 接口按 locator 查询。

### 5. 几何检测从选择器中拆出

几何检测不应继续内置在通用资源选择器中。资源选择器只负责选择资源；调用方按业务约束触发空间能力检测。

建议能力形态：

- 选择器输出 locator。
- 调用方按需调用 `spatial capability by locator` 或更通用的 `resource capability by locator`。
- `requireGeometry` 一类约束留在业务调用方或 wrapper 中，不进入通用选择器核心。

### 6. 新表创建使用目标创建契约

新表创建不是已有资源定位，不允许组件构造虚拟 locator。

新表目标选择流程：

1. 通过资源树选择可写入的父级 node，例如 schema / database。
2. 用户输入新表名。
3. 表单输出目标创建 DTO，而不是 locator。

示例：

```json
{
  "target_parent_locator": "addp://engine/8/path/public?type=schema&node_id=12",
  "target_name": "new_table",
  "target_kind": "table",
  "write_mode": "replace"
}
```

真实 locator 只能在资源创建并形成 Meta node / item 事实后产生。

Transfer target endpoint 也遵守这个边界：source endpoint 指向已存在资源，继续使用 `locator`；target endpoint 表达待写入资源，必须使用 `parent_locator + name`，不再用目标全路径构造尚未存在的虚拟 locator。

建议 endpoint 形态：

```json
{
  "target": {
    "parent_locator": "addp://engine/8/path/public?type=schema&node_id=12",
    "name": "new_table",
    "data_type": "table",
    "representation": "native",
    "policy": {
      "write_mode": "overwrite"
    }
  }
}
```

对 encoded target，`parent_locator` 指向目录 / bucket / prefix 等父 node，`name` 是目标文件名或对象名。Planner 可以由父 node 与 name 计算写入 `CatalogPath`，但不能把这个待创建资源提前持久化为 locator。

## Transfer 现有成果复用分析

Transfer 模块已经形成较成熟的资源选择和 endpoint 配置主线，不应另起一套资源选择器。

### 已经可以复用的成果

1. Transfer 任务配置已统一为 source / target endpoint JSON，`locator` 是 endpoint 主身份。`transfer/README.md`、`transfer/docs/design.md` 和 `transfer/docs/transfer-基本概念及配置说明.md` 均明确旧 `connector_type`、`source_config`、`target_config`、旧 endpoint `engine_id` 等不再作为主路径。
2. `transfer/frontend/src/views/TaskWizard/Step1SelectSource.vue` 已使用 `common-frontend` 的 `ResourceTree` 作为源资源选择 UI，不再使用 Cascader。
3. Transfer 已经实践了资源过滤：按 engine storage capability、data type、representation、format、raw copy capability 等判断资源是否可选。
4. Transfer 已经形成 selection summary、字段加载、source endpoint 构造等真实业务闭环，可作为共享选择器输出 DTO 和业务 wrapper 的参考。

### 不能直接抽成通用组件的部分

Transfer Step1 当前把通用资源选择和 Transfer 专属任务规则混在一起，不能整块搬到 `common-frontend`：

- `isSupportedSourceShape`、`supportedEncodedSourceFormats`、`rawCopyFormats` 属于 Transfer 能力矩阵。
- `buildSourceEndpointResource`、字段映射预加载、source / target endpoint 构造属于 Transfer 任务配置。
- 当前懒加载仍主要走 `/nodes/{node_id}/children`，不是 locator node API / ancestors API。
- 当前回显基于 `sourceItem.path.segments` 展开，不是标准 `initialLocator -> ancestors`。
- Step2 target 已从 schema/table 下拉和 allow-create 收敛为“选父 node + 输入目标名”；任务 JSON 中 target 不提交待创建资源 locator，只提交 `parent_locator + name`。

### 抽取边界建议

共享层只抽“资源树选择”本身：

- engine 列表加载与过滤。
- resource tree root 加载。
- node children 懒加载。
- `initialLocator` ancestors 回显。
- 节点过滤与可选状态标记。
- 当前选中 locator、display 信息和基础 resolved 信息输出。

Transfer 保留业务 wrapper：

- Transfer capability 矩阵。
- source / target endpoint 构造。
- 字段列表加载和字段映射初始化。
- table / encoded / raw copy 的支持范围判断。
- 输出格式、写入策略和批处理配置。

因此，`ResourceTreePicker` 应从 Transfer Step1 的通用部分抽象出来，再让 Transfer Step1 反向改用共享 picker。Transfer 应作为共享 picker 的首个验证场，并接入 Meta resource-tree API，然后再迁移 develop 和 service。

当前迁移结果中，Transfer Step1 / Step2 已完成反向接入：source 选择使用共享 `ResourceTreePicker` 输出已有资源 `locator`；target 创建使用共享 `ResourceTreePicker mode="node"` 选择父 node，再由业务表单输入目标名，endpoint 固定为 `parent_locator + name`。

## 新版契约草案

### 已有资源选择 selection

```json
{
  "identity": {
    "locator": "addp://engine/8/path/public/cities?type=table&item_id=54"
  },
  "display": {
    "label": "cities",
    "path": "public / cities",
    "type": "table",
    "engine_name": "PostgreSQL"
  },
  "resolved": {
    "engine_id": 8,
    "resource_type": "table",
    "schema": "public",
    "table": "cities"
  },
  "capabilities": {
    "spatial": {
      "has_geometry": true,
      "geometry_column": "geom",
      "srid": 4326
    }
  }
}
```

约束：

- `identity.locator` 是唯一资源身份。
- `display` 只服务 UI 展示。
- `resolved` 是后端事实解析或调用方派生的执行参数，不作为公共定位入口。
- `capabilities` 可按业务需要异步补齐，通用选择器不默认承担检测。

### 新表目标 creation target

```json
{
  "target_parent_locator": "addp://engine/8/path/public?type=schema&node_id=12",
  "target_name": "new_table",
  "target_kind": "table",
  "write_mode": "replace"
}
```

约束：

- 不包含虚拟 locator。
- `target_parent_locator` 必须指向真实 node。
- 创建成功后由后端或后续扫描返回真实资源 locator。

## 迁移结果

1. 已基于 Transfer Step1 抽取共享资源树选择器边界，形成并实现 `ResourceTreePicker` 契约。
2. Meta 已提供 resource-tree facts API：tree root、locator node、ancestors。
3. `common-frontend` 已基于现有 `ResourceTree` 实现 `ResourceTreePicker`，复用 Meta ancestors 定位流程。
4. Transfer Step1 已反向改用共享 picker。
5. Transfer Step2 target 创建已收敛为选父 node + 输入目标名，不再依赖 schema/table 双下拉作为唯一 UI 路线。
6. Develop 工作流算子参数选择已迁移：table source 保存 `locator`，table target 保存 `target_parent_locator + target_name`，执行前由 Develop Backend 派生运行时参数。
7. Develop NFS 文件选择器已改用 Meta resource-tree API 和公共 locator parser，不再走 Manager tree 或 Develop 私有 NFS 引擎列表。
8. Service 查询服务、瓦片服务和发布入口已迁移到 locator / ResourceTreePicker 主线。
9. Service 表单推进条件以 locator 为主身份，`schema/table` 仅作为后端解析后的执行快照或展示字段。
10. Service 页面重复的 selection path 解析已收口到 `common-frontend` 的 `locatorPathFromSelection`。
11. 空间能力检测接口已从旧选择器中抽离，统一通过 Meta item 空间元数据能力查询。
12. Transfer 旧目录选择器、对象存储选择器、`objectStorageAPI`、后端 `/object-storage/browse` / `/object-storage/list-files` 以及旧 i18n 分组已删除，目标创建统一走 `parent_locator + name`。
13. Develop 私有 `catalog/children` 代理已删除；业务模块不再通过 Develop 透传 System catalog 获取资源树事实。
14. `common-frontend` 已删除旧 `dataSource.js` 和 System realtime catalog helper 公开导出，公共资源选择只保留 ResourceTreePicker / ResourceLocator / Meta resource-tree 主线。
15. `DataSourceCascader`、`DataSourceCascaderCard`、`DataSourceCascaderDialog` 以及 `DataSourceSelector` 系列组件和旧文档已删除。
16. Service 后端内部命名已从 `DataSourceHandler` 收口为 `ResourceCapabilityHandler`，只保留 Service 自有业务能力接口；资源选择、资源树、表级空间元数据统一回到 Meta resource-tree / item API，不再以 Service 数据源代理表达。
17. Service DataService 查询辅助接口已改为 locator 输入：`/data/query`、`/data/aggregate`、`/data/structure` 不再公开 `engine_id/schema/table` 三段式资源定位参数；后端仅在执行边界由 locator 派生这些运行时字段。

## 最小验证要求

迁移实施至少覆盖：

- 初始 locator 可通过 ancestors API 回显并高亮。
- 过滤条件能限制可选 node / item。
- 选择已有资源只输出 locator 主身份。
- develop 工作流保存与回显 locator。
- service 创建入口提交 locator，执行配置由后端解析。
- 新表目标创建不产生虚拟 locator。

已执行的关键验证记录见 `ResourceTreePicker共享资源选择器契约与迁移记录`。

稳定规则已并入正式文档，后续实现应以正式文档为准；本文不再作为唯一规范入口。
