# ResourceTreePicker 共享资源选择器契约与迁移记录

> 状态：方向已确认，核心迁移已落地，稳定契约已同步到正式文档。本文保留为迁移记录和实施细节索引。
>
> 正式规则入口：
> - `docs/spec/addp路径统一和指纹计算.md`：ResourceLocator、Meta resource-tree API、ancestors 回显、新资源 `parent_locator + name` 边界。
> - `docs/concepts/addp共享模块介绍.md`：`common/resourcetree`、Meta resource-tree API、`ResourceTree` / `ResourceTreePicker` 分工。
> - `common-frontend/README.md`：`ResourceTreePicker` 前端使用契约。

## 背景

ADDP 已经形成资源树与 ResourceLocator 的统一资源定位主线。Manager 预览、资源树跳转和 Transfer source endpoint 配置已经证明该路线可以覆盖跨 engine、跨层级、node / item、文件 / 对象 / 表 / 集合等资源选择场景。

`common-frontend` 中曾经存在 `DataSourceCascader` 级联选择器，它以 `engine_id/schema/table` 为核心输入契约，不适合作为长期统一资源选择入口。该组件已从共享前端中退场，后续跨模块资源选择统一抽象为 `ResourceTreePicker`。

## 价值目标

`ResourceTreePicker` 的目标不是重新实现资源树，而是在现有 `ResourceTree` 组件之上提供“表单级资源选择能力”：

1. 让 `develop`、`service`、`transfer` 等模块复用同一套资源选择 UI 和 locator 契约。
2. 支持调用方按业务能力过滤资源，而不是让通用组件理解业务。
3. 支持 `initialLocator` 回显，避免前端拆解 schema / table / bucket / path。
4. 输出结构化 selection，主身份统一为 locator。
5. 为新表、新文件等目标创建场景提供父 node 选择能力，但不构造虚拟 locator。

## 非目标

`ResourceTreePicker` 不负责：

1. 不负责 Transfer endpoint、Develop 算子参数、Service 发布配置等业务 DTO 的最终组装。
2. 不负责空间能力检测、字段加载、格式读写能力判断等业务能力查询。
3. 不负责创建新资源，也不生成尚不存在资源的 locator。
4. 不保留 `engine_id/schema/table` 作为公共定位入口。
5. 不替代 `ResourceTree` 的底层展示能力，而是组合使用它。

## 与现有能力的关系

### Locator 共享能力归属

ResourceLocator 是平台级定位契约，不归属于 Manager 模块。Manager 是 locator 的重要消费方，但其他模块不得为了 locator 解析、构造或校验依赖 Manager。

当前共享能力归属：

- 后端统一使用 `common/resourcetree`，包括 `ResourceLocator`、`ParseURI`、`ToURI`、`LocatorFromFullName`、`ProviderCatalogPathFromLocator` 和资源树 `TreeBuilder`。
- 前端统一使用 `@addp/common-frontend` 暴露的 `parseLocator`、`buildLocator`、`getFullName`、`getParentLocator`、`isLocatorEqual` 等工具。

约束：

1. 模块内不得新增手写 locator parser / builder。
2. 现有手写 parser / builder 应在迁移中逐步替换为共享工具。
3. 共享组件不得从 Manager 前端或 Manager 后端导入 locator 工具。
4. 需要新增 locator 能力时，优先补 `common/resourcetree` 和 `common-frontend`，再由各模块消费。

### ResourceTree

`ResourceTree` 继续作为树展示组件，负责树节点渲染、展开状态、当前节点、节点 action 和插槽。

`ResourceTreePicker` 在其上补齐：

- engine 选择。
- root tree 加载。
- children 懒加载。
- 初始 locator 回显。
- 可选状态过滤。
- selection DTO 输出。

### Transfer Step1

`transfer/frontend/src/views/TaskWizard/Step1SelectSource.vue` 是 `ResourceTreePicker` 的重要原型，但不能整块抽到共享层。

可抽取：

- engine 列表加载和过滤入口。
- `ResourceTree` 组合方式。
- tree node 标准化。
- 可选状态标记。
- 展开、选中和 summary 的基础交互。

应保留在 Transfer：

- Transfer capability 矩阵。
- source endpoint 构造。
- 字段加载和字段映射初始化。
- table / encoded / raw copy 支持范围判断。

### Meta resource-tree API

资源树事实查询应优先归属 Meta。原因是 Meta 拥有 `meta_node`、`meta_item`、attributes、扫描结果和祖先关系事实；Manager 只是消费这些事实用于预览、快显和检索回跳。

建议由 Meta 提供平台级资源树事实 API：

```http
GET /api/v1/meta/resource-tree/{engine_id}
GET /api/v1/meta/resource-tree/{engine_id}/node?locator={resource_locator}
GET /api/v1/meta/resource-tree/{engine_id}/ancestors?locator={resource_locator}
```

`expand_depth` 只属于 root API，用于首屏加载时控制 catalog root 的相对展开深度；node API 固定返回目标节点及其直接子级，供前端按需懒加载。

其中 `initialLocator` 回显必须走 ancestors：

```http
GET /api/v1/meta/resource-tree/{engine_id}/ancestors?locator={resource_locator}
```

资源树事实入口统一走 Meta resource-tree API。共享 picker 通过 adapter 接入 API，不硬编码 Manager 路由，也不按 locator path 自行逐层猜测 schema、bucket、table 或目录。业务模块如需额外权限判断，应围绕自己的业务动作处理，不重新提供资源树事实代理。

### 现有实现调研结论

当前成熟代码主要分布在 `common` 和 `manager`，二者职责不同，迁移时不能整块搬用。

`common/resourcetree` 已经承担公共纯能力：

- `locator.go`：`ResourceLocator`、`ParseURI`、`ToURI`、`LocatorFromFullName`、`EngineRootLocatorForType`。
- `catalog_path.go`：`ProviderCatalogPathFromLocator`，负责 locator 到 provider `CatalogPath` 的纯转换。
- `tree_builder.go`：`TreeNode`、`TreeBuilder`、`BuildFromMetadataTree`、`ConvertMetaNodes`、`ConvertMetaItemsForEngine`、`ConvertNodeToTree`。

这些能力不依赖 System / Meta client，不处理租户权限、扫描、预览或业务 capability，适合作为 `Meta resource-tree API` 和前端 picker 的共享基础。公共资源树和 locator 只允许一套模型；如果字段不够，应扩展 `common/resourcetree.TreeNode`、`ResourceLocator` 或对应前端共享类型，不另起一套 TreeNode 或 locator DTO。只有业务私有输出确实不是公共资源树语义时，才允许在业务边界定义自己的 DTO。

`manager/backend/internal/service/explorer_service.go` 是当前最完整的应用编排参考：

- `GetTree`：从 System 获取 engine，从 Meta 获取 metadata tree，再用 `common/resourcetree.TreeBuilder` 构造资源树。
- `GetNodeChildren`：解析 locator，校验 engine，要求 locator 携带 `node_id`，再查 Meta children/items 并转换成 `TreeNode`。
- `GetAncestors`：解析 locator，校验 engine，按 locator path 回查当前 Meta node/item，再基于当前 ID 查询 ancestors，最后重新生成 target locator。

其中最值得保留的是 ancestors 的“按 path 回查当前事实，再重写 locator”语义。这样可以避免重新扫描后 locator 中旧 `node_id` / `item_id` 漂移导致回显失败。该语义已下沉到 Meta resource-tree 查询服务，Develop / Service / Transfer 不依赖 Manager 获取资源树事实。

`manager` 中不应上移为公共能力的部分：

- Manager 的预览、快显、瓦片缓存、向量化等业务 capability。
- Manager 对错误文案、预览刷新、扫描触发来源的处理。
- Manager 前端的页面状态和预览面板交互。

`meta` 当前已有事实查询基础：

- `GET /engines/:engine_id/tree`
- `GET /nodes/:node_id`
- `GET /nodes/:node_id/children`
- `GET /nodes/:node_id/items`
- `GET /nodes/:node_id/ancestors`
- `GET /items/:item_id`
- `GET /items/:item_id/ancestors`
- `GET /nodes/by-catalog-path`
- `GET /items/by-catalog-path`

这些接口返回的是 Meta 事实 DTO，不是前端资源树 DTO。新增 `Meta resource-tree API` 的工作量主要是薄编排：在 Meta 内部复用现有查询能力和 `common/resourcetree.TreeBuilder`，返回标准 `resourcetree.TreeNode` / ancestors result。

建议新增的 Meta 后端形态：

```go
type ResourceTreeService struct {
  metadataQueryService *MetadataQueryService
  systemClient *client.SystemClient
  treeBuilder *resourcetree.TreeBuilder
}
```

职责边界：

1. 只查询 Meta facts 和 System engine 基本信息。
2. 只返回资源树事实视图，不判断 Transfer / Develop / Service 私有能力。
3. 按租户隔离校验 engine 和 metadata facts。
4. locator 解析使用 `common/resourcetree.ParseURI`。
5. tree DTO 使用 `common/resourcetree.TreeNode`。
6. ancestors 以 locator 为入口，但必须按 path 重新解析到当前 Meta node/item，再生成当前 locator。

Meta resource-tree API：

```http
GET /api/v1/meta/resource-tree/{engine_id}
GET /api/v1/meta/resource-tree/{engine_id}/node?locator={resource_locator}
GET /api/v1/meta/resource-tree/{engine_id}/ancestors?locator={resource_locator}
```

`expand_depth` 只属于 root API；node API 固定返回目标节点及其直接子级，避免前端懒加载时出现两套展开语义。

搜索和刷新也归属 Meta resource-tree：

- 搜索使用 `GET /resource-tree/{engine_id}/search`，由 Meta 基于资源树事实返回标准 TreeNode results。
- 刷新使用 `POST /resource-tree/{engine_id}/refresh?locator=...`，由 Meta 触发对应资源的扫描刷新。
- 空间、读写、transfer capability 由业务模块按 locator 查询或计算，不能塞进 Meta resource-tree API。

### 公共模型唯一性原则

资源树与 locator 是平台级公共契约，后续只能有一套主模型。

后端主模型：

- `common/resourcetree.ResourceLocator`
- `common/resourcetree.TreeNode`

前端主模型：

- `@addp/common-frontend` 暴露的 locator 工具与资源树节点类型。
- `ResourceTreePicker` 输出的 selection 只能是对公共 `TreeNode` 的表单选择包装，不能重新定义资源身份。

字段不足时的处理原则：

1. 如果字段表达的是跨模块共享的资源事实，应扩展公共模型或公共 metadata 约定。
2. 如果字段表达的是业务执行参数，应放在业务 DTO 中，并引用公共 locator / selection。
3. 不允许因为某个模块临时缺字段而复制一套 TreeNode、locator parser、locator builder 或 resource selection 身份模型。
4. 业务 DTO 可以存在，但只能表达业务动作和业务快照，不能替代公共资源身份。

判断标准：

- “这个字段是否对 Develop / Service / Transfer / Manager 都有稳定意义？”如果是，进入公共模型。
- “这个字段是否只对某个任务执行、发布配置或算子参数有意义？”如果是，留在业务 DTO。
- “这个字段是否用于定位同一个资源？”如果是，必须回到 locator / TreeNode 公共契约。

## 组件形态

建议组件名：

```text
ResourceTreePicker
```

当前统一组件名为 `ResourceTreePicker`。除非未来出现明确且不可由 props / adapter 表达的业务必要性，不再新增 `ResourceLocatorPicker` 等平行选择器封装。

## Props 草案

```ts
interface ResourceTreePickerProps {
  apiClient: unknown
  apiBasePath?: string

  initialLocator?: string
  modelValue?: ResourceSelection | null

  engineTypes?: string[]
  engineFilter?: (engine: EngineOption) => boolean
  nodeFilter?: (node: ResourceTreeNode, context: PickerContext) => boolean
  selectableFilter?: (node: ResourceTreeNode, context: PickerContext) => boolean

  mode?: 'item' | 'node' | 'any'
  engineMultiple?: boolean
  selectAllEnginesByDefault?: boolean

  showEngineSelector?: boolean
  showSearch?: boolean
  searchSelectableOnly?: boolean
  showSelectionSummary?: boolean
  treeHeight?: string
}
```

### 说明

| Prop | 说明 |
| --- | --- |
| `apiClient` | 调用方传入 axios-like client。共享组件不绑定某个模块的 API client。 |
| `apiBasePath` | 资源树 API 前缀，例如 `/manager`、`/transfer`。最终请求形态需统一后再定。 |
| `initialLocator` | 已有资源回显入口，只接受 locator。 |
| `engineTypes` | 简单 engine type 过滤。 |
| `engineFilter` | 调用方自定义 engine 过滤，例如 Transfer 的 storage capability。 |
| `nodeFilter` | 控制树中哪些节点显示。 |
| `selectableFilter` | 控制哪些节点可选。未通过过滤的节点仍可展示和展开，但不会进入 selection。 |
| `mode` | 限定选择目标是 item、node 或任意资源。新表目标父级选择使用 `node`。 |
| `engineMultiple` | 控制 engine scope 是否可多选；selection 仍保持单个资源。 |
| `selectAllEnginesByDefault` | 在多引擎场景下默认选中全部通过过滤的 engine，适合 Transfer 这类“快速找 item”的入口。 |
| `searchSelectableOnly` | 搜索结果是否只展示可选资源；Transfer Step1 使用该模式，只暴露可传输 item。 |

## Events 草案

```ts
interface ResourceTreePickerEvents {
  'update:modelValue': (selection: ResourceSelection | null) => void
  'select': (selection: ResourceSelection) => void
  'engine-change': (engine: EngineOption | EngineOption[] | null) => void
  'node-click': (node: ResourceTreeNode) => void
  'error': (error: Error) => void
}
```

## Selection DTO 草案

```ts
interface ResourceSelection {
  identity: {
    locator: string
    engine_id: number
    node_id?: number
    item_id?: number
  }
  display: {
    label: string
    path: string
    type: string
    engine_name?: string
    engine_type?: string
  }
  resource: {
    kind: 'node' | 'item'
    type: string
    data_type?: string
    format?: string
    representation?: 'native' | 'encoded'
  }
  raw: {
    engine?: unknown
    node?: unknown
  }
}
```

约束：

1. `identity.locator` 是唯一资源身份。
2. `node_id` / `item_id` 只从 locator 或后端节点事实中读取，不由前端编造。
3. `display` 只用于 UI 展示。
4. `resource` 只承载通用事实，不放 Transfer、Develop、Service 私有字段。
5. `raw` 用于调用方短期消费已返回事实，不能作为跨模块持久化契约。
6. `ResourceSelection` 不是新的资源模型，只是 picker 的表单输出包装；持久化和跨模块传递时主身份仍是 locator。
7. 如 selection 需要新增公共字段，应回到公共资源树 / locator 契约扩展，不在业务模块复制 selection 类型。

## 目标创建 DTO 草案

新资源目标创建不应由 `ResourceTreePicker` 直接完成。建议由业务 wrapper 组合：

1. `ResourceTreePicker mode="node"` 选择父级 node。
2. 业务表单输入目标名、写入策略、格式等。
3. 输出目标创建 DTO。

```ts
interface ResourceCreationTarget {
  target_parent_locator: string
  target_name: string
  target_kind: string
  write_mode?: 'overwrite' | 'append'
}
```

示例：

```json
{
  "target_parent_locator": "addp://engine/8/path/public?type=schema&node_id=12",
  "target_name": "new_table",
  "target_kind": "table",
  "write_mode": "overwrite"
}
```

落到 Transfer 任务 endpoint 时，target 是待写入资源，不是已存在资源，因此使用同一语义但采用 endpoint 字段命名：

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

约束：

1. `source.locator` 只用于已存在资源。
2. `target.parent_locator` 必须指向已存在父 node。
3. `target.name` 表达父 node 下待创建或待覆盖的资源名。
4. `target.locator` 不作为新建目标的合法输入，避免虚拟 locator。
5. 真实 locator 只能在资源创建并形成 Meta 事实后由 Meta / resource-tree 链路产生。

## API 适配草案

调用方通过 adapter 接入资源树事实能力，避免组件内硬编码具体模块 API。默认 adapter 面向 Meta resource-tree API；Transfer、Service、Develop 不保留资源树事实代理，只在业务动作中处理各自权限和 capability。

```ts
interface ResourceTreePickerAdapter {
  listEngines(): Promise<EngineOption[]>
  getTreeRoot(engineId: number, options?: { expandDepth?: number }): Promise<ResourceTreeNode>
  getNodeChildren(node: ResourceTreeNode): Promise<ResourceTreeNode[]>
  getAncestors(engineId: number, locator: string): Promise<ResourceAncestorsResult>
}
```

### 重要要求

1. `getAncestors` 是 `initialLocator` 回显的唯一主路径。
2. `getNodeChildren` 第一版可兼容 Transfer 当前 node_id children API，但共享契约目标应收敛到 Meta locator node API。
3. adapter 返回节点必须带 locator；缺 locator 的节点不得作为可选资源。
4. adapter 只负责把不同 API 响应映射到公共 `ResourceTreeNode`，不得为模块定义另一套树节点模型。
5. adapter 不应依赖 Manager 作为资源树事实中心。
6. adapter 内不得新增手写 locator parser / builder，只能调用共享 locator 工具。

## ResourceTreeNode 草案

```ts
interface ResourceTreeNode {
  id: string
  locator: string
  label: string
  type: string
  hasChildren?: boolean
  children?: ResourceTreeNode[]
  metadata?: {
    node_id?: number
    item_id?: number
    engine_id?: number
    engine_type?: string
    data_type?: string
    format?: string
    representation?: string
    selectable?: boolean
    [key: string]: unknown
  }
}
```

约束：

- `id` 优先使用 locator。
- `metadata.selectable` 是 UI 派生状态，不是后端事实。
- `metadata` 可携带 attributes 摘要，但不要把业务配置塞入树节点。
- `ResourceTreeNode` 是公共节点模型；字段不足时扩展公共类型或公共 metadata 约定，不在模块内复制节点类型。
- 业务私有状态只能由 wrapper、store 或业务 DTO 持有，不写入公共树节点作为持久契约。

## 过滤模型

过滤分两层：

1. `nodeFilter` 决定是否显示节点。
2. `selectableFilter` 决定节点是否可选。

示例：

```ts
const selectableFilter = (node) => {
  return node.metadata?.data_type === 'table'
}
```

Transfer 可以传入更复杂的过滤：

```ts
const selectableFilter = (node) => {
  return isSupportedTransferSourceShape({
    dataType: node.metadata?.data_type,
    representation: node.metadata?.representation,
    format: node.metadata?.format
  })
}
```

## 初始 locator 回显流程

1. 解析 `initialLocator` 得到 engine id。
2. 加载 engine 列表并选中对应 engine。
3. 调用 adapter `getAncestors(engineId, initialLocator)`。
4. 按 ancestors 构造或合并 tree path。
5. 展开祖先节点。
6. 设置 current node key 为目标 locator。
7. 若目标满足 `selectableFilter`，输出 selection。
8. 若目标不满足当前 `mode` 或 `selectableFilter`，只能展开和高亮，不得向 `v-model` 输出 selection。

禁止：

- 不得按 `/` 或 `.` 自行拆路径递归查找。
- 不得用 `schema/table` 兜底回显。
- 不得在找不到 ancestors 时返回半成功状态；应交给调用方展示错误或提示刷新扫描。

## Transfer 接入草案

Transfer Step1 改造后应保留自己的业务 wrapper：

```vue
<ResourceTreePicker
  :adapter="transferResourceTreeAdapter"
  :engine-filter="hasStorageCapability"
  :selectable-filter="isSupportedTransferSourceNode"
  :initial-locator="wizardState.sourceLocator.value"
  @select="handleSourceSelected"
/>
```

`handleSourceSelected` 继续由 Transfer 负责：

- 构造 source endpoint。
- 加载字段。
- 初始化字段映射。
- 同步 wizard state。

Transfer Step2 native table target 后续可改为：

```vue
<ResourceTreePicker
  mode="node"
  :adapter="transferResourceTreeAdapter"
  :selectable-filter="isWritableTableParentNode"
  @select="handleTargetParentSelected"
/>
<el-input v-model="targetName" />
```

并输出：

```json
{
  "target_parent_locator": "...?type=schema&node_id=12",
  "target_name": "roads_copy",
  "target_kind": "table",
  "write_mode": "overwrite"
}
```

Transfer 最终 endpoint 仍由 Transfer 自己根据该 DTO 和写入策略组装。

## Develop 接入草案

Develop 工作流算子参数选择：

- table source 参数使用 `ResourceTreePicker mode="item"`。
- 通过 `selectableFilter` 限制可选 table / collection 等可执行表资源。
- 保存 `locator` 作为参数主身份，不再由前端保存 `schema/table/engine_id` 作为资源选择契约。
- Develop Backend 在执行前把 `locator` 派生为 Python runtime 需要的 `engine_id/schema/table`，随后再用既有流程将 `engine_id` 替换成 `connection_info`。

输出到新表：

- 使用 `ResourceTreePicker mode="node"` 选择父级 schema / database。
- 输入新表名。
- 保存 `target_parent_locator + target_name`，不保存虚拟 locator。
- Develop Backend 在执行前把 `target_parent_locator + target_name` 派生为 `engine_id/schema/table`，并删除资源选择字段，避免 Python runtime 依赖平台 locator。

NFS 文件选择：

- `nfs_file_picker` 是工作流文件算子的专用交互，但资源树读取同样走 Meta resource-tree API。
- NFS 引擎列表通过 Meta `resource-tree` 引擎列表按 `engine_type=nfs` 过滤，不再使用 Develop 私有 `/engines/nfs`。
- NFS 文件树读取不再依赖 Manager `/tree` API，locator path 解析统一使用 `common-frontend` 的 `parseLocatorSafe`。

## Service 接入草案

Service 发布表单：

- 创建入口以 locator 选择源资源。
- 空间服务通过业务层调用 spatial capability by locator。
- 查询服务通过业务层调用 table / object table capability by locator。
- 后端保存或生成执行快照，例如 `engine_id/schema/table/geometry_column/srid`，但公共输入不以这些字段定位资源。

## 实施状态

### 阶段 0：核验 Meta resource-tree 半成品

当前工作区中 Meta 后端已经存在一轮 resource-tree 实现草稿，下一步不应从零新建，也不应再复制 Manager 实现。

已存在的实现位置：

- `meta/backend/internal/service/resource_tree_service.go`
- `meta/backend/internal/api/handler_resource_tree.go`
- `meta/backend/internal/api/router.go`
- `meta/backend/internal/models/dto.go`
- `meta/backend/docs/swagger.json`
- `meta/backend/docs/swagger.yaml`
- `meta/backend/docs/docs.go`

已覆盖的主路径：

- `GET /api/v1/meta/resource-tree/:engine_id`
- `GET /api/v1/meta/resource-tree/:engine_id/node?locator=...`
- `GET /api/v1/meta/resource-tree/:engine_id/ancestors?locator=...`
- service 内复用 `common/resourcetree.TreeBuilder`。
- ancestors 已采用按 locator path 回查当前 Meta node/item，再生成当前 locator 的语义。

已核验并收口的问题：

1. 错误状态码需要收口：invalid locator、engine mismatch、缺少 `node_id/item_id` 应返回 400；目标不存在应返回 404；租户无权访问应返回 403；不能全部落到 500。（已处理：`ErrInvalidResourceLocator` + handler 统一 `handleServiceError`）
2. `ResourceTreeService` 的 lite DTO 到 common DTO 转换不能丢失关键公共字段；如 `scanned_at`、`data_updated_at` 未来需要展示，应扩展公共转换路径，而不是模块内另建节点类型。（已处理：保留节点 `scanned_at`、item `scanned_at` / `data_updated_at` 到公共 TreeNode metadata）
3. `GetNode` 当前以直接子级加载为主，`expand_depth` 是否只保留参数需要在 Swagger 和文档中保持一致。（已处理：node API 固定返回直接 children/items，不再声明 `expand_depth`；root tree API 保留 `expand_depth`）
4. `ResourceTreeAncestorsResponse` 可以作为 ancestors 响应 DTO，但它只包装公共 `resourcetree.TreeNode`，不能演变成另一套资源身份模型。（已处理：ancestors 响应只承载公共 TreeNode 链和 target locator）
5. Swagger 注解、真实路由和生成产物必须一致。（已处理：已执行 Swagger 生成和覆盖校验）
6. Manager 旧 `/tree/:engine_id/...` 代理已删除；资源树事实入口只保留 Meta resource-tree。（已处理）

阶段 0 完成情况：

- 已完成。Meta resource-tree 三个 API 已作为统一资源树事实入口。
- 已覆盖 service / handler 核心路径和错误映射。
- Swagger 覆盖校验已通过。
- 文档确认 Meta 是资源树事实查询入口，Manager 只是消费方。

### 阶段 1：前端共享 picker

已完成：

1. `common-frontend` 已实现 `ResourceTreePicker`。
2. adapter 默认面向 Meta resource-tree API。
3. selection 只包装公共 `ResourceTreeNode`，不定义新的资源身份。
4. 前端 locator parse / build / safe parse / display path 统一使用 `@addp/common-frontend` 共享工具。

### 阶段 2：Transfer 首个迁移场

已完成：

1. Transfer Step1 已改用 `ResourceTreePicker`，保留 Transfer capability、endpoint 构造、字段加载和字段映射初始化。
2. Transfer Step2 已收敛为父 node + 目标名。
3. Transfer source 使用 `locator`，target 使用 `parent_locator + name`，不再构造待创建资源虚拟 locator。
4. Transfer 内手写 locator parser / builder 已替换为共享工具。
5. Transfer 私有数据源树代理和旧表元数据代理已删除。
6. Transfer 编辑回填时 `target.parent_locator` 会进入 `targetConfig.parentLocator`，确保父 node 选择、review 和最终提交都沿同一条 `parent_locator + name` 主路径。

### 阶段 3：Develop / Service 迁移

已完成：

1. Develop 工作流算子资源选择已迁移到 `resource_tree_picker`。
2. Develop table source 保存 `locator`，table target 保存 `target_parent_locator + target_name`，执行前由 Develop Backend 派生运行时参数。
3. Develop NFS 文件选择器已改用 Meta resource-tree API 和公共 locator parser，不再走 Manager tree 或 Develop 私有 NFS 引擎列表。
4. Develop 私有 `catalog/children` 代理已删除；资源树事实读取不再通过 Develop 透传 System catalog。
5. Service 查询服务、瓦片服务和发布入口已迁移到 `ResourceTreePicker`。
6. Service 表单推进条件以 locator 为主身份，`schema/table` 仅作为后端解析后的执行快照或展示字段。
7. Service / Transfer 不再保留私有数据源树代理。
8. Transfer 旧目录选择器 / 对象存储选择器以及对应 `objectStorageAPI` 已删除，目标创建统一使用 `ResourceTreePicker mode="node"` 选择父 node。
9. `common-frontend` 不再公开 System realtime catalog helper；跨模块资源选择公共入口只保留 Meta resource-tree / ResourceTreePicker / ResourceLocator 主线。Meta 前端扫描配置如需实时 catalog 控制面能力，作为 Meta 模块内部实现处理。
10. Service 页面重复的 selection path 解析已收口到 `common-frontend` 的 `locatorPathFromSelection`。
11. `DataSourceCascader` / `DataSourceSelector` 组件、包装组件和旧文档已删除。
12. Service 后端内部 `DataSourceHandler` 已收口为 `ResourceCapabilityHandler`；路由只保留 `GET /graphs/node-shapes` 和 `POST /sql/spatial-metadata` 等业务能力接口，不再保留资源树或 `schema/table` 数据源代理命名。
13. Service `DataService` 的 `POST /data/query`、`POST /data/aggregate`、`GET /data/structure` 已改为 locator 输入，由后端解析 locator 派生 `engine_id/schema/table` 执行参数，不再公开三段式资源定位参数。

后续收口：

1. Manager 前端仍有少量业务 helper 以“失败返回 null”的函数签名包装公共 `parseLocatorSafe`，属于 Manager 业务 UI 容错层；如果后续继续整理 Manager 资源树工具，应避免恢复手写 locator 解析。
2. Meta 前端扫描配置仍可使用 System realtime catalog 控制面 API，这是 Meta 扫描配置能力，不作为跨模块资源选择公共入口。
3. 已完成正式文档落位：稳定规则已并入 `docs/spec/addp路径统一和指纹计算.md`、`docs/concepts/addp共享模块介绍.md` 和 `common-frontend/README.md`；本文后续只保留迁移记录。

### Manager 后续迁移边界

Manager 仍然需要资源树 UI 和资源树交互，但资源树事实来源已经迁移到 Meta resource-tree。功能归属如下：

已迁入 Meta：

1. `GET /meta/resource-tree/:engine_id`：资源树 root。
2. `GET /meta/resource-tree/:engine_id/node`：按 locator 获取 node children。
3. `GET /meta/resource-tree/:engine_id/ancestors`：按 locator 回显祖先链。
4. `GET /meta/resource-tree/:engine_id/search`：资源树事实搜索。
5. `POST /meta/resource-tree/:engine_id/refresh`：资源事实刷新 / 扫描触发。

应留在 Manager：

1. `GET /manager/preview`：数据预览，包括表、文件、对象、container 内部 child 预览。
2. `storage-download` / stream / preview download 等数据管理动作。
3. quick-view、tile-cache、vectorization 等 Manager 业务任务。
4. Manager 页面状态、预览面板交互、错误文案和业务跳转。

container 预览边界：

- container child 是 Manager preview selection，不是 ResourceLocator identity。
- 外部资源仍由 locator 定位到真实 Meta item，例如 zip / xlsx / gpkg / sqlite / shapefile 主资源。
- `child_name`、`ref_path`、`nested_child_path` 只用于 Manager 预览时选择 container 内部视图，不进入 Meta resource-tree，不构造虚拟 locator。
- container children 可作为 Meta item attributes 中的内容摘要供预览展示，但不应混入普通资源树 TreeNode；除非未来明确把 container child 提升为一等 MetaItem。
- Manager 资源树事实迁移到 Meta 后，预览面板仍通过 `GET /manager/preview?locator=...&child_name=...` 等参数预览 container 内部内容。

当前落地状态：

- Manager 前端资源树 root / node children / ancestors / search / refresh 已通过 `manager/frontend/src/api/dataExplorer.js` 调用 Meta resource-tree。
- Manager 前端不再直接调用 `/manager/tree/:engine_id`、`/manager/tree/:engine_id/node`、`/manager/tree/:engine_id/ancestors`、`/manager/tree/:engine_id/search`、`/manager/tree/:engine_id/refresh`。
- Manager 后端旧 `/tree` 通用资源树代理路由已删除。
- Manager 保留 preview / storage-stream / storage-download / quick-view / tile-cache / vectorization 等数据管理动作。

### 本轮审计补充

1. `ResourceTreePicker` 已补充 root 加载和 `initialLocator` 回显的请求代次保护，避免快速切换 engine / locator 时旧异步请求晚返回覆盖当前选择。
2. `ResourceTreePicker` 内部 engine id 比较统一按数字化处理，避免 adapter 返回字符串 ID 时回显 selection 缺失当前引擎信息。
3. `common-frontend/basic/src/api/dataSource.js` 已收敛为 `resourceCapability.js`；公共导出仍为 `detectTableMetadata`，语义限定为 Meta item 空间能力检测，不再保留“数据源选择 API”的命名暗示。
4. Meta resource-tree root 的 `expand_depth` 已按 catalog root 的相对深度解释，而不是直接使用 `meta_node.depth` 的绝对值过滤。真实数据中 catalog root 可能从 `depth=1` 起步；如果按绝对值判断，`expand_depth=1` 会把第一层 schema / bucket / dir 过滤掉，导致资源树首屏为空。该问题已由 `TestResourceTreeGetTreeUsesExpandDepthRelativeToCatalogRoot` 覆盖。
5. `common-frontend/basic/src/types/resourceLocator.js` 的 `ResourceType` 已补齐 `common/resourcetree` 中的公共 catalog 术语，包括 `file`、`prefix`、`root`、`server`、`service`、`dir` 等，避免前端模块各自用字符串补丁判断资源类型。
6. Manager 前端内部 `getNodeChildren` / `loadNodeChildren` 已去掉 `expandDepth` 参数，node children 调用只表达“按 locator 加载直接子级”；root tree 初始加载继续通过 `expand_depth` 控制首屏展开深度。
7. Meta scanflow 已改用 `common/resourcetree.ParseURI` 解析 locator，避免扫描目标解析保留手写 locator parser。
8. Manager `SpatialPreview` 的 locator 展示路径已改用 `formatLocatorDisplayPath`，不再手写 `/path/` 字符串拆分。
9. Transfer 前端重复的 locator 解析适配已收口到 `transfer/frontend/src/utils/resourceLocator.js`，不再在各页面复制 `parseLocator`。
10. Transfer 旧 `CatalogDirectoryPicker` / `ObjectStoragePathPicker`、`objectStorageAPI` 以及后端 `/object-storage/browse`、`/object-storage/list-files` 已删除，对应 i18n 分组已清理。
11. Develop 旧 `POST /develop/engines/:id/catalog/children` 私有代理和前端导出已删除；Swagger 已重新生成。
12. `common-frontend/basic/src/api/catalog.js` 已删除，`common-frontend` 不再公开 System realtime catalog helper，避免业务模块绕过 Meta resource-tree。
13. Console 前端未使用的 `getSchemas` / `listAvailableSchemas` 旧 API 已删除。
14. 真实环境 smoke 已覆盖 Meta root / node / ancestors / search、Manager preview、Develop 算子参数配置、Service 查询 / 瓦片 / 注册服务列表，确认资源树事实入口走 Meta，Manager 预览仍保留业务入口且不与 container child 预览边界冲突。
15. Service 后端资源能力接口内部命名已从 `DataSourceHandler` 收敛为 `ResourceCapabilityHandler`，Swagger tag 同步为 `资源能力 | Resource Capabilities`；旧 `/engines`、`/engines/:engine_id/tree`、`/nodes/:node_id/children`、`/tables/metadata`、`/tables/spatial-metadata` 生成物残留已清理。
16. Service DataService 查询辅助接口已迁移到 locator 单一路线：`DataQueryRequest` / `AggregationRequest` 只接受 `locator`，`/data/structure` 只接受 `locator` query；无效 locator 返回 400。`tile_service_service.go` 等仍出现的 `engine_id/schema/table` 属于服务执行配置快照或瓦片运行时参数，不是资源选择公共入口。
17. `ResourceTreePicker` 已补充搜索入口：单引擎模式下选择 engine 后在当前 engine 内搜索，未选择 engine 时可对当前可用且通过 `engineFilter` 的 engine 并发搜索；多引擎模式下搜索范围严格等于当前 engine scope。Transfer Step1 启用多引擎 scope，默认选中全部支持 storage capability 的 engine，用户可随时移除若干 engine；下方树和搜索结果都只来自当前 scope，用于快速找到可传输 item。树展开路径仍保留为熟悉资源位置时的主交互。
18. Transfer Step1 不再在 node 或不可选资源旁展示“当前数据项暂不支持传输”类负面提示；不符合过滤条件的节点只是不产生 selection，下一步按钮保持不可用。Step2 对源数据无法继续时的提示也改为“请选择可传输的数据项”。

## 最小验证

阶段 0 至少验证：

```bash
cd meta/backend && go test ./internal/service ./internal/api
bash scripts/swagger/gen-swagger.sh meta
bash scripts/swagger/check-route-coverage.sh meta
```

阶段 1 / 2 至少验证：

```bash
cd transfer/frontend && npm run build
rg -n "DataSourceCascader" develop/frontend/src service/frontend/src transfer/frontend/src common-frontend/basic/src
rg -n "initialSelection|engine_id.*schema|schema.*table" common-frontend/basic/src develop/frontend/src service/frontend/src transfer/frontend/src
```

说明：`common-frontend` 当前没有独立 build 脚本，需通过主要消费方构建验证共享导出。

已执行验证：

```bash
cd meta/backend && go test ./internal/service ./internal/api
bash scripts/swagger/check-route-coverage.sh meta

cd transfer/backend && go test ./internal/api ./internal/planner ./internal/service
cd transfer/frontend && npm run build
bash scripts/swagger/check-route-coverage.sh transfer

cd service/backend && go test ./...
cd service/frontend && npm run build
bash scripts/swagger/check-route-coverage.sh service

cd develop/backend && go test ./internal/service ./internal/api
cd develop/frontend && npm run build
cd engines/python-workflow && python3 -m py_compile operators/io_operators.py operators/base.py
bash scripts/swagger/check-route-coverage.sh develop

cd manager/frontend && npm run test:map
cd manager/frontend && npm run build

cd service/frontend && npm run build
cd develop/frontend && npm run build
cd manager/frontend && npm run build
cd manager/frontend && npm run test:map

# 2026-06-18 本轮审计补充验证
cd transfer/frontend && npm run build
cd service/frontend && npm run build
cd develop/frontend && npm run build
cd manager/frontend && npm run build
cd meta/frontend && npm run build
cd console/frontend && npm run build
cd manager/frontend && npm run test:map
bash scripts/swagger/check-route-coverage.sh meta
bash scripts/swagger/check-route-coverage.sh manager
bash scripts/swagger/check-route-coverage.sh transfer
bash scripts/swagger/check-route-coverage.sh service
bash scripts/swagger/check-route-coverage.sh develop
bash scripts/swagger/check-route-coverage.sh manager
python -m compileall common-python/addp_common agent/backend/tools engines/python-workflow/operators
cd meta/backend && go test ./internal/api ./internal/service
cd manager/backend && go test ./internal/api ./internal/service
cd transfer/backend && go test ./internal/planner ./internal/service
cd service/backend && go test ./internal/api ./internal/service
git diff --check

# 2026-06-18 收口补充验证
cd common && go test ./resourcetree
cd meta/backend && go test ./internal/scanflow ./internal/scanresolver ./internal/api ./internal/service
cd manager/backend && go test ./internal/api ./internal/service
cd transfer/backend && go test ./internal/api ./internal/planner ./internal/service
bash scripts/swagger/gen-swagger.sh transfer
bash scripts/swagger/check-route-coverage.sh transfer
cd develop/backend && go test ./internal/api ./internal/service
cd service/backend && go test ./internal/api ./internal/service
cd manager/frontend && npm run build
cd transfer/frontend && npm run build
cd develop/frontend && npm run build
cd service/frontend && npm run build
cd meta/frontend && npm run build
cd console/frontend && npm run build
bash scripts/swagger/check-route-coverage.sh meta manager transfer develop service
git diff --check

# 2026-06-18 Service 命名收口补充验证
cd service/backend && gofmt -w cmd/server/main.go internal/api/router.go internal/api/resource_capability_handler.go
cd service/backend && go test ./internal/api ./internal/service
bash scripts/swagger/gen-swagger.sh service
bash scripts/swagger/check-route-coverage.sh service

# 2026-06-18 Service DataService locator 化补充验证
cd service/backend && go test ./internal/api ./internal/service ./internal/service/data
bash scripts/swagger/gen-swagger.sh service
bash scripts/swagger/check-route-coverage.sh service
```

真实 API smoke 摘要：

- `GET /api/v1/meta/resource-tree/8?expand_depth=1` 返回 PostgreSQL engine 的 schema children，验证 root 展开深度按 catalog root 相对解释。
- `GET /api/v1/meta/resource-tree/26?expand_depth=1` 返回 NFS engine 的目录 children，验证文件类资源树首屏可用。
- `GET /api/v1/meta/resource-tree/8/search?q=te&limit=5` 返回 table 搜索结果，验证 search 已归属 Meta resource-tree。
- `GET /api/v1/meta/resource-tree/8/node?locator=...public...` 返回 schema 下 children/items；`GET /api/v1/meta/resource-tree/8/ancestors?locator=...test...` 返回 root -> schema -> table 回显链。
- `GET /api/v1/meta/items/67/spatial` 能按 locator 对应 item 查询空间能力，业务 capability 不塞进 ResourceTreePicker。
- `GET /api/v1/manager/preview?locator=...test...` 返回表预览；NFS `README.md` locator 返回 object 预览，验证 Manager preview 继续作为业务动作保留。
- `GET /api/v1/develop/operators` 返回 `resource_tree_picker` / `nfs_file_picker` 参数配置，其中 source 使用 `locator`，target 使用 `target_parent_locator + target_name`。
- `GET /api/v1/service/query`、`GET /api/v1/service/tile`、`GET /api/v1/service/registered` 均可正常返回分页数据，验证 Service 列表链路未受资源选择迁移影响。

浏览器 smoke 摘要：

- Console 使用默认开发账号登录正常。
- Transfer `tasks/create` Step1 使用 `ResourceTreePicker` 正常加载引擎列表；选择 `Business PostgreSQL` 后首屏出现 `public / tiger / tiger_data / topology`；展开 `public` 后能看到 table items；选择 `public/test` 后展示行数、字段数、空间字段和 SRID，下一步按钮解锁。
- Transfer Step2 初始只要求选择目标引擎，符合 `target parent node + name` 的交互入口；本轮浏览器自动化未稳定点中 Element Plus 浮层 option，因此未把 Step2 继续深入作为产品失败判定。
- Service `query-services/create` 表配置模式使用同一 `ResourceTreePicker` 正常加载 PostgreSQL / MinIO / MySQL；选择 PostgreSQL 后 schema root 正常显示；展开 `public` 并选择 `test` 后 selection summary 出现，下一步按钮解锁。
- Develop `workflow` 页面正常加载 Python 工作流引擎，算子面板展示 `load` / `save`；DAG canvas 拖拽在浏览器自动化中未稳定触发，参数契约以后端 `operators` API 和 `OperatorParamsPanel` 静态链路验证为准。
- Manager `data-explorer` 首屏已显示多 engine resource-tree；展开 PostgreSQL `public` 后可见 table items；选择 `public/test` 后 Manager preview 正常展示表格数据、地图预览、分页、下载和快显入口，验证 Manager 业务预览未与 Meta resource-tree 迁移冲突。
