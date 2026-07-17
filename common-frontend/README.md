# @addp/common-frontend

ADDP 平台前端共享组件库，提供跨模块复用的 Vue 3 组件、工具函数和类型定义。

## 安装

```bash
# 在模块的 frontend 目录中
npm install file:../../common-frontend
```

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

### 导入类型定义

```js
import { FieldType, FormatType, ResourceType } from '@addp/common-frontend'

console.log(FieldType.STRING) // "string"
console.log(FormatType.SHAPEFILE) // "shapefile"
```

## 组件列表

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
  getResourceTreeAncestors,
  selectionFromResourceTreeNode
} from '@addp/common-frontend'
```

### 预览组件

预览组件从 `@common-ui/previews` 单独导入，使用方模块需要在自己的 `package.json` 中声明对应预览依赖。

- **GeoJsonPreview** - GeoJSON 文件预览（带地图）
- **TablePreview** - 表格数据预览，支持空间字段渲染
- **ImagePreview** - 图片预览（依赖 `geotiff` 支持 TIFF 渲染）
- **MarkdownPreview / DocxPreview / PptxPreview / PdfPreview** - 文档预览组件

地图预览、CRS registry、底图 profile 和 GCJ-02 展示适配规则见 [Map 前端组件说明](./map/README.md)。

### Agent UI 协议组件

`agent-ui/` 提供 ADDP Agent 界面的共享协议适配层：

- 使用官方 `@a2ui/web_core/v0_9` 处理 A2UI Surface；
- 使用 `addp.catalog/v1` 注册受信任 Vue 组件；
- 当前包含 `WorkflowDag`、`ClarificationChoice`、`ApprovalRequest`、`MapView`、`TablePreview` 和 `ResourcePicker`；
- 未注册组件会被拒绝，不执行 Agent 生成的任意前端代码；
- A2UI wrapper 只映射声明式 props 和 action，业务事实仍通过 ResultRef / Interaction 访问 owner API。

`MapView` 只渲染最多 200 个 WGS84 GeoJSON Feature，不接受 URL；`TablePreview` 只渲染最多 50 列、100 行 JSON 标量；`ResourcePicker` 只允许提交当前 Interaction 已持久化的 locator 候选，不等同于实时 `ResourceTreePicker`。

消费模块必须在自己的 `package.json` 中声明 `@a2ui/web_core`、Vue、Element Plus 和 Zod 依赖，`common-frontend` 不保存独立 `node_modules`。

AG-UI 等基于 Fetch 的流式客户端必须使用共享 `createAuthenticatedFetch()`，以便动态注入 Browser AuthSession 内存中的 User Access Token，并执行统一的主动刷新、401 单次重试和多标签页协调。模块内不得复制 Token 持久化、刷新或重试逻辑。

```javascript
import { A2UISurface } from '@addp/common-frontend/agent-ui'
import { createAuthenticatedFetch } from '@common-ui'
```

### 表单组件

- **StorageEngineForm** - 存储引擎配置表单（支持 PostgreSQL、MinIO/S3）

### 树形组件

- **ResourceTree** - 通用资源树组件
  - 配置式节点操作（刷新、查看详情等）
  - 全树搜索，自动展开匹配路径
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
