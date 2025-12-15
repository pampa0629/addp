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
const resourceForm = ref({
  resource_type: 'postgresql',
  name: '',
  connection_info: {}
})
</script>

<template>
  <ShapefilePreview :data="previewData" />

  <StorageEngineForm v-model="resourceForm" />
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
