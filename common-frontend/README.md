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
import { makeNodeId, findNodeById, flattenTree, findNodePath } from '@addp/common-frontend'

// 生成唯一节点 ID
const nodeId = makeNodeId('engine', 1, 'public', 'users')

// 查找节点
const node = findNodeById(treeData, 'table-1-users')

// 扁平化树
const allNodes = flattenTree(treeData)

// 查找节点路径
const path = findNodePath(treeData, 'table-1-users')
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
