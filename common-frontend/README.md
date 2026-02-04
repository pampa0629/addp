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
import { ShapefilePreview, GeoJsonPreview, TablePreview, StorageEngineForm } from '@addp/common-frontend'
import { formatFileSize, detectFormatByExtension } from '@addp/common-frontend'

const previewData = ref(null)
const engineForm = ref({
  engine_type: 'postgresql',
  name: '',
  connection_info: {}
})
</script>

<template>
  <ShapefilePreview :data="previewData" />

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

### 导入类型定义

```js
import { FieldType, FormatType, ResourceType } from '@addp/common-frontend'

console.log(FieldType.STRING) // "string"
console.log(FormatType.SHAPEFILE) // "shapefile"
```

## 组件列表

### ResourceLocator 定位符系统

统一的资源定位符 URI 系统，支持跨存储引擎的资源标识。

**URI 格式**: `addp://engine/{engine_id}/path/{resource_path}?type={type}`

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
  type: 'table'
})
// 'addp://engine/1/path/public/users?type=table'
```

### 数据源选择器

> 📚 **详细文档**: [数据源选择器使用指南](./docs/数据源选择器使用指南.md)

提供统一的数据源（表/对象）选择体验，支持多种存储引擎。

**组件系列**:
- `DataSourceSelector` - 核心选择器组件
- `DataSourceSelectorDialog` - Dialog 包装（弹窗场景）
- `DataSourceSelectorCard` - Card 包装（表单嵌入场景）

**主要特性**:
- ✅ 多种数据源类型（PostgreSQL、MySQL、MongoDB、MinIO、S3、Doris、ClickHouse、Spark）
- ✅ 引擎类型过滤和节点类型过滤
- ✅ 自动几何列检测（空间数据）
- ✅ 单选/多选模式
- ✅ 懒加载优化

**使用示例**:
```vue
<template>
  <DataSourceSelectorDialog
    v-model:visible="dialogVisible"
    api-base-url="/api/service"
    :engine-types="['postgresql', 'mysql']"
    :selectable-node-types="['table']"
    @confirm="handleConfirm"
  />
</template>

<script setup>
import { ref } from 'vue'
import { DataSourceSelectorDialog } from '@addp/common-frontend'

const dialogVisible = ref(false)

const handleConfirm = (selection) => {
  console.log('选择:', selection)
  // { engineId, schema, tableName, fullName, locator, hasGeometry, ... }
}
</script>
```

**相关 API 函数**:
```js
import {
  getEngines,              // 获取引擎列表
  getEngineTree,           // 获取引擎树结构
  getNodeChildren,         // 懒加载子节点
  detectTableMetadata,     // 检测表元数据（几何列等）
  extractDataSourceSelection  // 从树节点提取选择信息
} from '@addp/common-frontend'
```

### 预览组件

- **ShapefilePreview** - Shapefile 文件预览（带地图）
- **GeoJsonPreview** - GeoJSON 文件预览（带地图）
- **TablePreview** - 表格数据预览
- **ImagePreview** - 图片预览

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

树增量加载器，封装增量加载、缓存、错误处理逻辑。

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
  'addp://engine/1/path/public?type=schema',
  1  // 展开深度
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
