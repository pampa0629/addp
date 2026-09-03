# @addp/common-frontend

ADDP 平台前端共享组件库，提供跨模块复用的 Vue 3 组件、工具函数和类型定义。

## 安装

```bash
# 在模块的 frontend 目录中
npm install file:../../common-frontend
```

## 测试

共享组件、结果契约和唯一所有权测试由 `common-frontend` 自己维护，统一入口为：

```bash
npm --prefix common-frontend test
```

根 `make test-platform` 会调用该入口。业务模块只验证自身的协议适配和组合行为，不再代跑共享组件测试。

## 使用

### 导入预览组件

```vue
<script setup>
import { GeoJsonPreview, TablePreview, StorageEngineForm } from '@addp/common-frontend'
import { formatFileSize, detectFormatByExtension } from '@addp/common-frontend'

const previewData = ref(null)
const engineForm = ref({
  engine_type: 'postgresql',
  name: '',
  connection_info: {}
})
</script>

<template>
  <TablePreview :data="previewData" />

  <StorageEngineForm v-model="engineForm" />
</template>
```

### 导入工具函数

```js
import {
  formatFileSize,
  formatDateTime,
  detectFormatByExtension,
  isGeospatialFormat
} from '@addp/common-frontend'

const size = formatFileSize(1024000) // "1.00 MB"
const format = detectFormatByExtension('data.shp') // "shapefile"
const isGeo = isGeospatialFormat('shapefile') // true
```

### 统一任务监控跳转

任务执行接口返回统一 `execution_id` 后，模块前端应通过公共工具进入 Monitor：

```js
import { openMonitorExecution } from '@addp/common-frontend'

await openMonitorExecution(execution.execution_id)
```

该工具在 Console iframe 中会请求父级切换到 `/monitor/executions?execution_id=...`；模块独立运行时会回退为在新窗口打开 Console 路由。业务模块不要自行硬编码 Console 端口或拼接跨模块 iframe URL。

模块内公开导航使用 `navigateConsoleModuleRoute()`；跨模块或需要由 Console 加载目标页面时使用 `openConsoleRoute()`。只有页面已经自行完成受控状态切换、只需要同步地址栏时，才直接使用底层 `syncConsoleRoute()`：

```js
import {
  navigateConsoleModuleRoute,
  openConsoleRoute,
  syncConsoleRoute
} from '@addp/common-frontend'

await navigateConsoleModuleRoute(router, 'develop', {
  path: '/workflow',
  query: { action: 'edit', id: '544' }
})
await openConsoleRoute('/manager/spatial-quick-view/vector-tile-cache?create=1')
await syncConsoleRoute('/manager/data-explorer?locator=...', { history: 'replace' })
```

`navigateConsoleModuleRoute()` 默认使用 `push`。在 iframe 中，它用模块 Router 的 `replace` 完成无刷新切换，再由 Console 写入唯一公开历史；standalone 模式直接使用模块 Router 的同名历史操作。创建成功写入 ID、Tab 切换或参数规范化时显式传 `{ history: 'replace' }`。

`openConsoleRoute()` 默认新增浏览器历史并由 Console 加载目标页面；`syncConsoleRoute()` 只允许当前 iframe 同步自身模块路由，支持 `push` / `replace`，且不得重载已经完成导航的 iframe。完整约束见 [`docs/spec/addp前端路由与可恢复状态规范.md`](../docs/spec/addp前端路由与可恢复状态规范.md)。

### Console 页面描述与最近访问

Console 只自动记录侧边栏中的固定菜单路由。固定菜单的短标签若依赖侧边栏模块上下文，Console 配置必须另设全局语境下可独立识别的 `recentLabel`。详情、工作台、任务和执行等动态页面必须由业务模块在对象加载完成后通过 `useConsolePageDescriptor()` 提供页面语义；Console 不解析业务路由，也不跨模块查询对象名称。

```js
import { computed } from 'vue'
import { useConsolePageDescriptor } from '@common-ui'

useConsolePageDescriptor(router, 'graph', {
  title: computed(() => t('graph.browser.recentVisitTitle')),
  subject: computed(() => graph.value?.name || ''),
  ready: computed(() => Boolean(graph.value)),
})
```

- 展示统一为“页面标题 · 业务对象名称”；没有业务对象时只显示页面标题。
- `title` 是 Console 全局语境中的业务对象类型，必须脱离模块和页面上下文后仍能独立识别，例如“传输任务”“图谱构建任务”“质量检查执行”。不要直接复用“任务详情”“执行详情”“服务信息”等局部页面标题。
- `subject` 只承载具体对象的名称、标题或业务编码；不能依赖名称中恰好出现“导入”“导出”等词来补足 `title` 的业务语义。
- `title`、`subject`、`ready` 和 `recent` 可以是普通值、`ref` 或 `computed`，语言或对象变化后会自动重新发布。
- `subject` 优先使用名称、标题或业务编码；仅当对象确实没有业务名称时使用 ID。
- 创建、编辑、登录、OAuth 回调等临时页面不声明页面描述，因此不进入最近访问。
- 稳定动态页面必须等对象加载完成后再令 `ready=true`，不得先用模块名或路由参数占位。
- 最近访问保存完整公开 `fullPath`，包括可恢复状态所需的 query。

多个页面共享的稳定 Tab 使用 `resolveCanonicalTabRouteState()` 解析。业务模块负责提供允许值、默认值，以及已经按 owner 事实验证过的伴随 query；该函数统一省略默认 Tab、删除未知参数，并返回是否需要 `replace` 到 canonical URL。

### 导入类型定义

```js
import { FieldType, FormatType, ResourceType } from '@addp/common-frontend'

console.log(FieldType.STRING) // "string"
console.log(FormatType.SHAPEFILE) // "shapefile"
```

## 组件列表

### 数据血缘组件

`@addp/common-frontend/graph` 提供 `LineageViewer`、`createLineageApi` 和 `normalizeLineageGraph`，消费 Meta `GET /api/v1/meta/lineage/graph` 返回的 `{ nodes, edges }`，可在 Service、Manager、Asset 页面内嵌展示。请求函数由宿主注入，组件不处理 Token。

### ResourceLocator 定位符系统

统一的资源定位符 URI 系统，支持跨存储引擎的资源标识。

**URI 格式**: `addp://engine/{engine_id}/path/{resource_path}?type={type}&node_id={node_id}` 或 `...&item_id={item_id}`

**支持的资源类型**:
- `table` - 关系型数据库表
- `collection` - MongoDB 集合
- `object` - 对象存储对象
- `directory` - 对象存储目录
- `database` / `schema` / `bucket` - 容器类型

**工具函数**:
```js
import {
  parseLocator,       // 解析 URI 为对象
  buildLocator,       // 构建 URI 字符串
  getPathString,      // 获取路径字符串（如 "public/users"）
  getFullName,        // 获取完整名称（类型感知格式化）
  getLastSegment,     // 获取路径最后一段
  getParentLocator,   // 获取父路径定位符
  cloneLocator,       // 深拷贝定位符
  isLocatorEqual      // 比较两个定位符是否相等
} from '@addp/common-frontend'

// 示例
const locator = parseLocator('addp://engine/1/path/public/users?type=table')
// { engineId: 1, path: ['public', 'users'], type: 'table' }

const uri = buildLocator({
  engineId: 1,
  path: ['public', 'users'],
  type: 'table',
  itemId: 100
})
// 'addp://engine/1/path/public/users?type=table&item_id=100'
```

### 资源树选择器

跨模块资源选择统一使用 `ResourceTreePicker`。它基于标准 ResourceLocator 和 Meta resource-tree API，负责引擎选择、资源树浏览、懒加载、`initialLocator` 回显和 locator 主身份 selection 输出。

`ResourceTreePicker` 不负责业务 DTO 组装、空间能力检测、字段加载或新资源创建。调用方应在收到 locator 后按业务需要调用 capability API，并保存必要执行快照。

稳定约束：

- `mode="item"` 选择已有 data item，`mode="node"` 选择已有资源节点，`mode="any"` 仅用于确有混合选择语义的场景。
- 已有资源选择结果以 `selection.identity.locator` 作为唯一资源身份；`display` 只用于 UI 展示，`raw.node` 只作为调用方补充读取事实的原始材料。
- `initialLocator` 回显走 Meta resource-tree ancestors API。回显目标不满足当前 `mode` 或 `selectableFilter` 时，只允许展开和高亮，不应回填为有效 selection。
- 对明确不应出现的资源，优先通过 `nodeFilter` 直接过滤掉；`selectableFilter` 仅在需要保留上下文但禁止选择时使用。
- 选择器不构造尚不存在资源的 locator。创建新表、新文件或新对象时，业务表单应使用 `ResourceTreePicker mode="node"` 选择父节点，再输入名称，提交 `parent_locator + name`。

**使用示例**:
```vue
<template>
  <ResourceTreePicker
    v-model="selection"
    api-base-url="/api/v1/meta"
    :engine-families="['tabular', 'dynamic_schema']"
    mode="item"
    :selectable-filter="node => node.type === 'table'"
    @select="handleSelect"
  />
</template>

<script setup>
import { ref } from 'vue'
import { ResourceTreePicker } from '@addp/common-frontend'

const selection = ref(null)

const handleSelect = (selection) => {
  console.log('选择:', selection)
  // selection.identity.locator 是资源主身份
}
</script>
```

新表创建示例：

```vue
<template>
  <ResourceTreePicker
    v-model="parentSelection"
    api-base-url="/api/v1/meta"
    mode="node"
    :selectable-filter="node => ['schema', 'database'].includes(node.type)"
  />
  <el-input v-model="targetName" />
</template>

<script setup>
import { computed, ref } from 'vue'
import { ResourceTreePicker } from '@addp/common-frontend'

const parentSelection = ref(null)
const targetName = ref('')

const targetCreateDTO = computed(() => ({
  parent_locator: parentSelection.value?.identity?.locator || '',
  name: targetName.value.trim()
}))
</script>
```

**相关 API 函数**:
```js
import {
  listResourceTreeEngines,
  getResourceTree,
  getResourceTreeNode,
  getResourceTreeAncestors
} from '@addp/common-frontend'
```

`ResourceTreePicker` 的 selection 统一包含 `display.engine_name`、`display.engine_type`、按引擎原生风格格式化的 `display.path`，以及 `resource.spatial.geometry_columns` 和 `resource.spatial.primary_geometry_column`。`identity.locator` 用于提交和持久化，`display` 只用于界面展示；geometry 字段必须从 `resource.spatial` 自动回填，不得读取 `raw` 私有结构或让用户自由输入未识别字段名。

### 预览组件

预览组件从 `@common-ui/previews` 单独导入，使用方模块需要在自己的 `package.json` 中声明对应预览依赖。

- **GeoJsonPreview** - GeoJSON 文件预览（带地图）
- **TablePreview** - 表格数据预览，支持空间字段渲染
- **ImagePreview** - 图片预览（依赖 `geotiff` 支持 TIFF 渲染）
- **MarkdownPreview / DocxPreview / PptxPreview / PdfPreview** - 文档预览组件

地图预览、CRS registry、底图 profile 和 GCJ-02 展示适配规则见 [Map 前端组件说明](./map/README.md)。

### 数据服务结果渲染器

已发布数据服务的消费结果使用三组共享 primitive，业务模块只负责根据自身配置分派 renderer：

- `basic/src/components/TabularResultRenderer.vue`：表格预览与有界结果的唯一基础表格实现，按显式列配置展示标量和结构化值；
- `basic/src/components/DataPagination.vue`：预览场景的唯一受控分页组件，只表达分页状态和变化事件，不决定客户端切片或服务端加载；
- `basic/src/components/ScalarValueRenderer.vue`：显示服务已返回的唯一行数值结果，不在浏览器求和、计数或猜测口径；
- `chart/src/ChartRenderer.vue`：展示 `bar | line | pie`，只使用服务已返回的明细值，不在浏览器聚合；
- `map/src/components/GeoJSONResultRenderer.vue`：只读取 Consumer Descriptor 明确声明的 geometry 字段和 CRS，并可使用显式 label、tooltip 与 `uniform | categorical | continuous` 受控主题样式；不猜测业务字段，不接受原始颜色或任意样式 DSL。

Value、Chart 和 Map 只接受单次查询得到的完整有界结果；`has_more=true` 时必须拒绝渲染，Value 还必须恰好一行，Chart 和 Map 还必须遵守各自结果上限。Workbench 等消费模块应在自己的 Renderer Host 中按需加载这些组件，不得复制 renderer，也不得把 Service、Outdoor 或其他 owner 的 DTO 写入共享层。

三组结果 renderer 在用户选择当前结果时统一发出 `result-select`，payload 只包含 `{ row_index }`。`row_index` 始终指向宿主传入的原始 `rows`，renderer 不携带字段值、参数名、目标组件或查询片段；宿主负责根据自己的声明式配置解释选择。数据更新、resize 和重绘不得发出该事件。

Manager 表格预览、容器内表格预览、Develop 查询结果、Workbench 表格结果和 Agent UI 的 `TablePreview` 协议适配器必须组合上述基础表格，不得分别维护 `el-table`、结构化值详情或单元格格式化实现。Manager 的服务端分页与 Develop 的客户端分页仍由各自宿主负责；二者只共享受控分页界面和事件契约。

### Agent UI 协议组件

`agent-ui/` 提供 ADDP Agent 界面的共享协议适配层，完整协议、安全上限和降级规则见 `docs/spec/addp智能体交互协议规范.md`：

- 使用官方 `@a2ui/web_core/v0_9` 处理 A2UI Surface；
- 使用 `addp.catalog/v1` 注册受信任 Vue 组件；
- 当前包含 `WorkflowDag`、`ClarificationChoice`、`ApprovalRequest`、`MapView`、`TablePreview` 和 `ResourcePicker`；
- 未注册组件会被拒绝，不执行 Agent 生成的任意前端代码；
- A2UI wrapper 只映射声明式 props 和 action，业务事实仍通过 ResultRef / Interaction 访问 owner API。

`MapView` 只渲染最多 200 个 WGS84 GeoJSON Feature，不接受 URL；`TablePreview` 只渲染最多 50 列、100 行 JSON 标量；`ResourcePicker` 只允许提交当前 Interaction 已持久化的 locator 候选，不等同于实时 `ResourceTreePicker`。

组件扩展必须由 `docs/spec/addp智能体评测规范.md` 中的稳定场景驱动；当前未开放 `GraphView`，不得只为展示愿景注册空组件。

消费模块必须在自己的 `package.json` 中声明 `@a2ui/web_core`、Vue、Element Plus 和 Zod 依赖，`common-frontend` 不保存独立 `node_modules`。

AG-UI 等基于 Fetch 的流式客户端必须使用共享 `createAuthenticatedFetch()`，以便动态注入 Browser AuthSession 内存中的 User Access Token，并执行统一的主动刷新、401 单次重试和多标签页协调。模块内不得复制 Token 持久化、刷新或重试逻辑。

```javascript
import { A2UISurface } from '@addp/common-frontend/agent-ui'
import { createAuthenticatedFetch } from '@common-ui'
```

### 表单组件

- **StorageEngineForm** - 存储引擎配置表单（支持 PostgreSQL、MinIO/S3）
- **StatusAnnouncer** - 为校验、保存、执行等动态状态提供统一的辅助技术播报区域

### 可调整布局

- **useResizable** - 提供受最小/最大值约束的鼠标拖拽和键盘调整能力；分隔条消费方使用方向键逐步调整，使用 `Home` / `End` 跳到边界。右侧面板从左边缘拖拽时传入 `{ reverse: true }`，可用 `setSize` / `resetSize` 恢复布局

### 树形组件

- **ResourceTree** - 通用资源树组件
  - 配置式节点操作（刷新、查看详情等）
  - 全树搜索，自动展开匹配路径，提供无匹配结果状态，清空后恢复搜索前状态
  - 智能展开状态管理
  - 双击展开/折叠交互
  - 支持 v-model 绑定（展开状态、选中节点、搜索关键词）
  - 自定义节点渲染（插槽支持）

**使用示例**:

```vue
<template>
  <ResourceTree
    :tree-data="treeData"
    :loading="loading"
    :refreshing-node-ids="refreshingNodeIds"
    v-model:expanded-keys="expandedKeys"
    v-model:current-node-key="currentNodeKey"
    v-model:filter-text="searchKeyword"
    :node-actions="nodeActions"
    title="存储引擎"
    @refresh="handleRefresh"
    @node-click="handleNodeClick"
    @node-action="handleNodeAction"
  />
</template>

<script setup>
import { ref, computed } from 'vue'
import { ResourceTree } from '@addp/common-frontend'

const treeData = ref([
  {
    id: 'engine-1',
    type: 'resource',
    label: 'PostgreSQL-Prod',
    metadata: { engineId: 1, resourceType: 'postgresql' },
    children: [
      {
        id: 'schema-1-public',
        type: 'schema',
        label: 'public',
        metadata: { engineId: 1, schema: 'public' },
        children: [
          {
            id: 'table-1-users',
            type: 'table',
            label: 'users',
            metadata: { engineId: 1, schema: 'public', table: 'users' }
          }
        ]
      }
    ]
  }
])

const nodeActions = [
  {
    name: 'refresh',
    icon: 'Refresh',
    visible: (node) => node.type !== 'resource',
    disabled: (node) => loading.value,
    loading: (node) => refreshingNodeIds.value.includes(node.id),
    tooltip: '刷新'
  }
]

const handleNodeAction = ({ action, node }) => {
  if (action === 'refresh') {
    // 处理刷新逻辑
  }
}
</script>
```

**树形工具函数**:

```js
import {
  // 数据结构操作
  flattenTree,        // 扁平化树为数组
  filterTree,         // 过滤树节点
  sortTree,           // 树节点排序
  cloneTree,          // 深拷贝树
  getAllNodeKeys,     // 获取所有节点 ID（✨ 新增）

  // 节点查询
  findNodeById,       // 根据 ID 查找节点
  findNodePath,       // 查找节点路径（根到目标的所有 ID）
  getParentIds,       // 获取所有父节点 ID
  getLeafNodes,       // 获取所有叶子节点

  // 树遍历
  traverseTree,       // 递归遍历每个节点（提供回调）
  getTreeDepth        // 计算树的最大深度
} from '@addp/common-frontend'

// 使用示例

// 获取所有节点 ID
const allKeys = getAllNodeKeys(treeData)
// ['engine-1', 'schema-1-public', 'table-1-users']

// 查找节点
const node = findNodeById(treeData, 'table-1-users')

// 扁平化树
const allNodes = flattenTree(treeData)

// 查找节点路径
const path = findNodePath(treeData, 'table-1-users')
// ['engine-1', 'schema-1-public', 'table-1-users']

// 遍历树（带父节点和层级信息）
traverseTree(treeData, (node, parent, level) => {
  console.log(`Level ${level}: ${node.label}`)
})

// 过滤树节点
const tables = filterTree(treeData, (node) => node.type === 'table')

// 树节点排序
const sorted = sortTree(treeData, (a, b) => a.label.localeCompare(b.label))
```

### 定时调度组件

> 📚 **详细文档**: [ScheduleConfig.md](./basic/docs/ScheduleConfig.md)

- **ScheduleConfig** - 定时调度配置组件，用于配置 Cron 表达式
  - 11 种快捷预设（每天、每周、每月等）
  - 4 种调度模式（每天/每周/每月/自定义 Cron）
  - 实时中文预览
  - 格式验证
  - 支持自定义预设列表
  - 支持禁用、只读、紧凑等模式

- **ScheduleDisplay** - 表格单元格渲染组件，用于显示调度信息
  - 简洁的图标 + 文字展示
  - 自动调用 `describeCron` 转换为中文描述
  - 支持自定义空值文本

**使用示例**:

```vue
<!-- ScheduleConfig: 表单嵌入模式 -->
<template>
  <el-form-item label="定时调度">
    <ScheduleConfig
      v-model="form.schedule"
      :allow-custom-cron="true"
      :show-presets="true"
    />
  </el-form-item>
</template>

<!-- ScheduleDisplay: 表格渲染 -->
<template>
  <el-table-column label="调度配置">
    <template #default="{ row }">
      <ScheduleDisplay :cron="row.schedule" />
    </template>
  </el-table-column>
</template>
```

### 认证组件 (Composables)

> 📚 **详细文档**: [AUTH_USAGE_GUIDE.md](./basic/composables/AUTH_USAGE_GUIDE.md)

- **createAuthGuard(authStore, config)** - 创建标准化的 Vue Router 路由守卫
- **createAuthInterceptor(authStore, moduleName)** - 创建智能等待的 Axios 请求拦截器
- **createAuthStoreConfig(storeName, authAPI, options)** - 生成标准化的 Pinia auth store 配置

**快速示例**:

```javascript
// 1. Auth Store (从 120 行 → 10 行)
import { defineStore } from 'pinia'
import { createAuthStoreConfig } from '@common-ui'
import { authAPI } from '../api/auth'

export const useAuthStore = defineStore('manager-auth', {
  ...createAuthStoreConfig('manager-auth', authAPI, {
    persistUser: false
  })
})

// 2. Router Guard (从 100 行 → 10 行)
import { createAuthGuard } from '@common-ui'
router.beforeEach(createAuthGuard(useAuthStore(), {
  moduleName: 'Manager',
  loginRouteName: 'Login'
}))

// 3. Axios Interceptor (从 20 行 → 3 行)
import { createAuthInterceptor } from '@common-ui'
client.interceptors.request.use(
  createAuthInterceptor(useAuthStore(), 'Manager')
)
```

**收益**: 每个模块的认证代码从 ~240 行减少到 ~23 行 (**-90%**) 🎉

### 树管理组件 (Composables)

提供通用的树加载、缓存管理功能，适用于需要树形数据的场景。

#### useTreeCache

树节点缓存管理器，提供自动过期、容量控制的缓存功能。

**使用示例**:
```javascript
import { useTreeCache } from '@addp/common-frontend'

const cache = useTreeCache({
  maxAge: 5 * 60 * 1000,  // 5分钟过期
  maxSize: 100             // 最多100个缓存项
})

// 设置缓存
cache.setNodeChildrenCache('node_id', children)

// 获取缓存
const cached = cache.getNodeChildrenCache('node_id')

// 清空所有缓存
cache.clearCache()
```

#### useTreeLoader

树增量加载器，封装增量加载、缓存、错误处理逻辑，默认请求 Meta resource-tree API。

**使用示例**:
```javascript
import { useTreeLoader } from '@addp/common-frontend'
import client from '@/api/client'

const treeLoader = useTreeLoader(client, {
  enableCache: true,
  cacheOptions: { maxAge: 5 * 60 * 1000 }
})

// 增量加载子节点
const children = await treeLoader.loadNodeChildren(
  'addp://engine/1/path/public?type=schema'
)

// 搜索节点
const results = await treeLoader.searchNodes(
  1,        // engineId
  'users',  // keyword
  { nodeTypes: ['table'], limit: 50 }
)

// 清空缓存
treeLoader.clearCache()
```

**主要特性**:
- ✅ 自动缓存管理（TTL + 容量控制）
- ✅ 增量加载优化
- ✅ 搜索结果缓存
- ✅ 错误处理和状态管理
- ✅ 支持强制刷新

### 地图组件

- **MapContainer** - 地图容器组件
- **OpenLayersRenderer** - OpenLayers 地图渲染器
- **GaodeMapRenderer** - 高德地图渲染器

## 工具函数

### 格式化

- `formatFileSize(bytes)` - 格式化文件大小
- `formatDateTime(dateTime)` - 格式化日期时间
- `formatCoordinate(coord, precision)` - 格式化坐标

### 格式检测

- `detectFormatByExtension(filename)` - 根据扩展名检测格式
- `isGeospatialFormat(format)` - 判断是否为地理空间格式
- `isTabularFormat(format)` - 判断是否为表格格式
- `isDocumentFormat(format)` - 判断是否为文档格式
- `isMediaFormat(format)` - 判断是否为媒体格式

### 类型工具

- `getFieldTypeLabel(fieldType)` - 获取字段类型的中文名称
- `normalizeFieldType(fieldOrType)` - 将数据库方言类型（包括参数化 decimal、geometry/geography）转换为平台规范字段类型；精度、空间子类型和 SRID 仍由字段事实单独保存

### 通用工具

- `deepClone(obj)` - 深拷贝对象
- `debounce(func, wait)` - 防抖函数
- `throttle(func, limit)` - 节流函数

## 类型定义

### FieldType

标准化字段类型（对应后端 `format.FieldType`）：

```js
FieldType.STRING    // 字符串
FieldType.INT       // 整数
FieldType.FLOAT     // 浮点数
FieldType.GEOMETRY  // 几何类型
// ... 更多类型
```

### FormatType

数据格式类型：

```js
FormatType.SHAPEFILE   // Shapefile
FormatType.GEOJSON     // GeoJSON
FormatType.CSV         // CSV
FormatType.EXCEL       // Excel
// ... 更多格式
```

### ResourceType

资源类型：

```js
ResourceType.DATABASE        // 数据库
ResourceType.OBJECT_STORAGE  // 对象存储
ResourceType.FILE_SYSTEM     // 文件系统
ResourceType.API             // API
```

## 开发

### 添加新组件

1. 在 `src/components/` 创建 Vue 组件
2. 在 `src/index.js` 中导出
3. 更新 README

### 添加新工具函数

1. 在 `src/utils/index.js` 添加函数
2. 添加 JSDoc 注释
3. 在 `src/index.js` 中导出

### 添加新类型

1. 在 `src/types/index.js` 添加类型定义
2. 在 `src/index.js` 中导出

## 依赖

- Vue 3.3+
- Element Plus 2.4+
- Axios 1.6+

## 许可

MIT
