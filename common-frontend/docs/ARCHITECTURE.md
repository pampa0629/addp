# Common Frontend - 拆分架构

为了避免不必要的依赖，`common-frontend` 已被拆分为两个子模块：

## 目录结构

```
common-frontend/
├── basic/          # 基础 UI 组件（无地图依赖）
│   └── src/
│       ├── components/
│       │   ├── StorageEngineForm.vue
│       │   └── ResourceTree.vue
│       ├── utils/
│       │   └── formatters.js
│       ├── types/
│       │   └── index.js
│       └── index.js
│
└── map/            # 地图相关组件（需要 ol 和 @amap/amap-jsapi-loader）
    └── src/
        ├── components/
        │   ├── map/
        │   │   ├── MapContainer.vue
        │   │   ├── GaodeMapRenderer.vue
        │   │   └── OpenLayersRenderer.vue
        │   ├── GeoJsonPreview.vue
        │   ├── TablePreview.vue
        │   └── TilePreview.vue
        ├── composables/
        │   ├── useMapConfig.js
        │   ├── useGaodeMap.js
        │   ├── useOpenLayersMap.js
        │   └── useResizable.js
        ├── utils/
        │   └── formatters.js
        └── index.js
```

## 使用方法

### 基础组件（System, Transfer 等不需要地图的模块）

在 `vite.config.js` 中：

```javascript
resolve: {
  alias: {
    '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
  }
}
```

在组件中：

```javascript
import { StorageEngineForm, ResourceTree } from '@common-ui'
```

**依赖要求**: 只需要 Vue 和 Element Plus

### 预览组件（Manager 等需要文件预览的模块）

预览组件从单独入口导入：

```javascript
import { ImagePreview, MarkdownPreview, PdfPreview } from '@common-ui/previews'
```

**依赖要求**: 使用预览入口的模块需要显式安装对应预览依赖，例如 `geotiff`、`marked`、`dompurify`、`mermaid`、`jszip`、`mammoth`。不使用预览入口的模块不应因为 `@common-ui` 主入口被迫安装这些依赖。

### 地图组件（Manager 等需要地图的模块）

在 `vite.config.js` 中：

```javascript
resolve: {
  alias: {
    '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src')
  }
}
```

在组件中：

```javascript
import { MapContainer, GeoJsonPreview, TablePreview } from '@common-ui-map'
```

**依赖要求**:
```json
{
  "dependencies": {
    "ol": "^9.2.4",
    "@amap/amap-jsapi-loader": "^1.0.1"
  }
}
```

## 优势

✅ **按需引入**: 不需要地图功能的模块无需安装地图依赖
✅ **减小包体积**: 基础模块的打包体积更小（约减少 2-3MB）
✅ **清晰职责**: 组件职责更加明确，易于维护
✅ **统一管理**: 所有共享组件都在 common-frontend 中统一维护

## 迁移指南

如果您的模块只使用 `StorageEngineForm`，请：

1. 更新 `vite.config.js`，将 `@common-ui` 指向 `basic/src`
2. 从 `package.json` 移除 `ol` 和 `@amap/amap-jsapi-loader`（如果没使用）
3. 运行 `npm uninstall ol @amap/amap-jsapi-loader` 清理依赖

如果您的模块使用地图组件，请：

1. 添加别名 `@common-ui-map` 指向 `map/src`
2. 确保 `package.json` 包含地图依赖
3. 更新导入语句

## 示例

### System Frontend (无地图)

```javascript
// vite.config.js
export default defineConfig({
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
    }
  }
})

// Engines.vue
import { StorageEngineForm } from '@common-ui'
```

### Manager Frontend (有地图)

```javascript
// vite.config.js
export default defineConfig({
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src')
    }
  }
})

// DataPreview.vue
import { TablePreview, GeoJsonPreview } from '@common-ui-map'
```
