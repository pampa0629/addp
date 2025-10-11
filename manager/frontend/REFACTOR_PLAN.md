# DataExplorer.vue 重构方案

## 当前问题
- 文件过大 (2500+ 行)
- 多种预览模式耦合在一个组件中
- 地图逻辑重复 (高德地图 + OpenLayers 各有一套)
- 难以扩展新文件类型

## 推荐方案: 插件化预览系统

### 架构设计

```
manager/frontend/src/
├── views/
│   └── DataExplorer.vue                  # 主组件 (简化到 ~300 行)
├── components/
│   ├── explorer/
│   │   ├── ResourceTree.vue              # 左侧资源树 (~150 行)
│   │   ├── PreviewPanel.vue              # 预览面板容器 (~100 行)
│   │   └── Splitter.vue                  # 可复用分隔器 (~50 行)
│   ├── previews/
│   │   ├── TablePreview.vue              # 表格预览 (~200 行)
│   │   ├── ObjectStoragePreview.vue      # 对象存储预览 (~150 行)
│   │   ├── ImagePreview.vue              # 图片预览 (~80 行)
│   │   ├── JsonPreview.vue               # JSON预览 (~100 行)
│   │   ├── GeoJsonPreview.vue            # GeoJSON预览 (~200 行)
│   │   └── TextPreview.vue               # 文本预览 (~60 行)
│   └── map/
│       ├── MapContainer.vue              # 地图容器 (~150 行)
│       ├── GaodeMapRenderer.vue          # 高德地图渲染器 (~250 行)
│       └── OpenLayersRenderer.vue        # OpenLayers渲染器 (~250 行)
├── composables/
│   ├── useMapConfig.js                   # 地图配置加载 (~100 行)
│   ├── useGaodeMap.js                    # 高德地图逻辑 (~200 行)
│   ├── useOpenLayersMap.js               # OpenLayers逻辑 (~200 行)
│   └── useResizable.js                   # 拖拽调整大小 (~80 行)
├── plugins/
│   └── previews/
│       ├── index.js                      # 预览插件注册中心
│       ├── table.js                      # 表格预览插件
│       ├── image.js                      # 图片预览插件
│       ├── json.js                       # JSON预览插件
│       ├── geojson.js                    # GeoJSON预览插件
│       └── text.js                       # 文本预览插件
└── utils/
    ├── geoConverter.js                   # GeoJSON转换工具
    └── formatters.js                     # 格式化工具

```

---

## 核心实现

### 1. 预览插件系统

```javascript
// plugins/previews/index.js
const previewRegistry = new Map()

export function registerPreview(config) {
  previewRegistry.set(config.name, {
    component: config.component,
    canHandle: config.canHandle,     // (data) => boolean
    priority: config.priority || 0
  })
}

export function getPreviewComponent(data) {
  const handlers = Array.from(previewRegistry.values())
    .filter(h => h.canHandle(data))
    .sort((a, b) => b.priority - a.priority)

  return handlers[0]?.component || null
}

// 预定义插件
import TablePreview from '@/components/previews/TablePreview.vue'
import ImagePreview from '@/components/previews/ImagePreview.vue'
import JsonPreview from '@/components/previews/JsonPreview.vue'
import GeoJsonPreview from '@/components/previews/GeoJsonPreview.vue'

registerPreview({
  name: 'table',
  component: TablePreview,
  canHandle: (data) => data.mode === 'table',
  priority: 10
})

registerPreview({
  name: 'image',
  component: ImagePreview,
  canHandle: (data) => data.object?.content?.kind === 'image',
  priority: 20
})

registerPreview({
  name: 'geojson',
  component: GeoJsonPreview,
  canHandle: (data) => data.object?.content?.kind === 'geojson',
  priority: 30
})

registerPreview({
  name: 'json',
  component: JsonPreview,
  canHandle: (data) => data.object?.content?.kind === 'json',
  priority: 20
})

registerPreview({
  name: 'text',
  component: () => import('@/components/previews/TextPreview.vue'),
  canHandle: () => true,  // 兜底
  priority: 0
})
```

### 2. 简化后的主组件

```vue
<!-- views/DataExplorer.vue -->
<template>
  <div class="data-explorer">
    <div class="split-container">
      <ResourceTree
        :tree-data="treeData"
        :loading="loadingTree"
        @refresh="loadTree"
        @node-click="handleNodeClick"
      />

      <Splitter v-model="treeWidth" :min="220" :max="600" />

      <PreviewPanel
        :selected-node="selectedNode"
        :preview-data="previewData"
        :loading="loadingPreview"
        @page-change="handlePageChange"
      />
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import ResourceTree from '@/components/explorer/ResourceTree.vue'
import PreviewPanel from '@/components/explorer/PreviewPanel.vue'
import Splitter from '@/components/explorer/Splitter.vue'
import dataExplorerAPI from '@/api/dataExplorer'

const treeData = ref([])
const selectedNode = ref(null)
const previewData = ref(null)
const loadingTree = ref(false)
const loadingPreview = ref(false)
const treeWidth = ref(320)

const loadTree = async () => {
  loadingTree.value = true
  try {
    const response = await dataExplorerAPI.getTree()
    treeData.value = transformResources(response.data?.data || [])
  } catch (error) {
    ElMessage.error('加载资源树失败')
  } finally {
    loadingTree.value = false
  }
}

const handleNodeClick = async (node) => {
  selectedNode.value = node
  loadPreview()
}

const loadPreview = async () => {
  if (!selectedNode.value) return
  loadingPreview.value = true
  try {
    const response = await dataExplorerAPI.getPreview({
      resource_id: selectedNode.value.resourceId,
      schema: selectedNode.value.schema,
      table: selectedNode.value.table
    })
    previewData.value = response.data
  } catch (error) {
    ElMessage.error('加载预览失败')
  } finally {
    loadingPreview.value = false
  }
}

onMounted(() => {
  loadTree()
})
</script>
```

### 3. 智能预览面板

```vue
<!-- components/explorer/PreviewPanel.vue -->
<template>
  <el-card>
    <template #header>
      <div class="panel-header">
        <span>{{ title }}</span>
      </div>
    </template>

    <div v-if="!selectedNode" class="empty-state">
      <el-empty description="从左侧选择数据查看预览" />
    </div>

    <component
      v-else
      :is="previewComponent"
      :data="previewData"
      :loading="loading"
      v-bind="$attrs"
    />
  </el-card>
</template>

<script setup>
import { computed } from 'vue'
import { getPreviewComponent } from '@/plugins/previews'

const props = defineProps({
  selectedNode: Object,
  previewData: Object,
  loading: Boolean
})

const previewComponent = computed(() => {
  if (!props.previewData) return null
  return getPreviewComponent(props.previewData)
})

const title = computed(() => {
  if (!props.selectedNode) return '数据预览'
  // 根据节点类型生成标题
  return generateTitle(props.selectedNode)
})
</script>
```

### 4. 独立的GeoJSON预览组件

```vue
<!-- components/previews/GeoJsonPreview.vue -->
<template>
  <div class="geojson-preview">
    <div class="controls">
      <span>地图预览</span>
      <el-switch v-model="showMap" />
      <el-select v-if="showMap" v-model="baseMapType">
        <el-option
          v-for="item in mapOptions"
          :key="item.value"
          :label="item.label"
          :value="item.value"
        />
      </el-select>
    </div>

    <MapContainer
      v-if="showMap"
      :features="geoFeatures"
      :base-map-type="baseMapType"
      height="360px"
    />

    <pre class="json-content">{{ formattedJson }}</pre>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import MapContainer from '@/components/map/MapContainer.vue'
import { useMapConfig } from '@/composables/useMapConfig'

const props = defineProps({
  data: Object  // { object: { content: { geojson: {...} } } }
})

const { mapOptions, defaultBaseMapType } = useMapConfig()
const showMap = ref(true)
const baseMapType = ref(defaultBaseMapType)

const geoFeatures = computed(() => {
  const geojson = props.data?.object?.content?.geojson
  if (!geojson) return []

  // 转换为统一的FeatureCollection格式
  if (geojson.type === 'FeatureCollection') {
    return geojson.features
  } else if (geojson.type === 'Feature') {
    return [geojson]
  } else if (geojson.type && geojson.coordinates) {
    return [{
      type: 'Feature',
      geometry: geojson,
      properties: {}
    }]
  }
  return []
})

const formattedJson = computed(() => {
  return JSON.stringify(props.data?.object?.content?.geojson, null, 2)
})
</script>
```

### 5. 统一的地图容器

```vue
<!-- components/map/MapContainer.vue -->
<template>
  <div class="map-container" :style="{ height }">
    <component
      :is="mapRenderer"
      :features="features"
      :config="mapConfig"
      @feature-click="$emit('feature-click', $event)"
    />
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useMapConfig } from '@/composables/useMapConfig'
import GaodeMapRenderer from './GaodeMapRenderer.vue'
import OpenLayersRenderer from './OpenLayersRenderer.vue'

const props = defineProps({
  features: Array,
  baseMapType: String,
  height: { type: String, default: '360px' }
})

const { mapConfig } = useMapConfig()

const mapRenderer = computed(() => {
  if (props.baseMapType === 'amapVector') {
    return GaodeMapRenderer
  }
  if (['tiandituVector', 'tiandituImage'].includes(props.baseMapType)) {
    return OpenLayersRenderer
  }
  return null
})
</script>
```

---

## 用户扩展新文件类型

### 方式一: 动态插件加载

```javascript
// 用户在 manager/frontend/public/plugins/my-custom-preview.js
window.DataExplorerPlugins = window.DataExplorerPlugins || []
window.DataExplorerPlugins.push({
  name: 'csv',
  component: {
    template: `
      <div class="csv-preview">
        <el-table :data="parsedData">
          <el-table-column
            v-for="col in columns"
            :key="col"
            :prop="col"
            :label="col"
          />
        </el-table>
      </div>
    `,
    props: ['data'],
    computed: {
      parsedData() {
        // 解析CSV文本
        return parseCSV(this.data.object?.content?.text)
      }
    }
  },
  canHandle: (data) => {
    const contentType = data.object?.content_type || ''
    return contentType.includes('csv')
  },
  priority: 25
})

// 在 index.html 中加载
// <script src="/plugins/my-custom-preview.js"></script>
```

### 方式二: 配置式扩展

```javascript
// manager/frontend/config/preview-plugins.config.js
export default {
  plugins: [
    {
      name: 'pdf',
      component: () => import('@/components/previews/custom/PdfPreview.vue'),
      match: {
        contentType: 'application/pdf'
      },
      priority: 30
    },
    {
      name: 'excel',
      component: () => import('@/components/previews/custom/ExcelPreview.vue'),
      match: {
        contentType: ['application/vnd.ms-excel', 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet']
      },
      priority: 30
    },
    {
      name: 'video',
      component: () => import('@/components/previews/custom/VideoPreview.vue'),
      match: {
        contentType: /^video\//
      },
      priority: 25
    }
  ]
}
```

### 方式三: 组件目录自动注册

```javascript
// plugins/previews/index.js
const previewModules = import.meta.glob('./custom/*.vue', { eager: true })

Object.entries(previewModules).forEach(([path, module]) => {
  const config = module.default.previewConfig
  if (config) {
    registerPreview({
      name: config.name,
      component: module.default,
      canHandle: config.canHandle,
      priority: config.priority
    })
  }
})
```

```vue
<!-- plugins/previews/custom/MarkdownPreview.vue -->
<template>
  <div class="markdown-preview" v-html="renderedHtml"></div>
</template>

<script setup>
import { computed } from 'vue'
import { marked } from 'marked'

const props = defineProps({
  data: Object
})

const renderedHtml = computed(() => {
  const text = props.data?.object?.content?.text || ''
  return marked(text)
})

// 导出插件配置
export const previewConfig = {
  name: 'markdown',
  canHandle: (data) => {
    const contentType = data.object?.content_type || ''
    const path = data.object?.path || ''
    return contentType.includes('markdown') || path.endsWith('.md')
  },
  priority: 25
}
</script>
```

---

## 实施步骤

### 阶段1: 拆分地图逻辑 (1-2天)
1. 抽取 `useMapConfig`, `useGaodeMap`, `useOpenLayersMap` composables
2. 创建 `MapContainer`, `GaodeMapRenderer`, `OpenLayersRenderer` 组件
3. 测试地图功能完整性

### 阶段2: 拆分预览组件 (2-3天)
1. 创建 `TablePreview`, `ImagePreview`, `JsonPreview`, `GeoJsonPreview` 组件
2. 实现插件注册系统
3. 迁移原有逻辑到各预览组件

### 阶段3: 重构主组件 (1天)
1. 简化 `DataExplorer.vue` 为容器组件
2. 抽取 `ResourceTree`, `PreviewPanel` 组件
3. 创建可复用 `Splitter` 组件

### 阶段4: 扩展性增强 (1天)
1. 实现动态插件加载机制
2. 编写插件开发文档
3. 添加示例自定义预览插件

---

## 预期效果

### 重构前
```
DataExplorer.vue: 2500+ 行
├── 表格预览逻辑
├── 对象存储预览逻辑
├── 高德地图实现
├── OpenLayers实现
├── GeoJSON地图实现
├── 拖拽调整大小逻辑
└── 数据转换工具
```

### 重构后
```
DataExplorer.vue: ~300 行 (主容器)
├── ResourceTree.vue: ~150 行
├── PreviewPanel.vue: ~100 行
├── Splitter.vue: ~50 行
├── previews/
│   ├── TablePreview.vue: ~200 行
│   ├── GeoJsonPreview.vue: ~150 行
│   └── ... (其他预览组件)
├── map/
│   ├── MapContainer.vue: ~100 行
│   ├── GaodeMapRenderer.vue: ~250 行
│   └── OpenLayersRenderer.vue: ~250 行
└── composables/
    ├── useMapConfig.js: ~100 行
    ├── useGaodeMap.js: ~200 行
    └── useOpenLayersMap.js: ~200 行
```

### 优势
✅ **职责清晰**: 每个文件只负责一个功能
✅ **易于测试**: 组件和逻辑解耦,单元测试简单
✅ **可扩展**: 新增文件类型只需添加插件,无需修改核心代码
✅ **可维护**: 修改地图逻辑不影响预览组件
✅ **性能优化**: 按需加载预览组件 (懒加载)

---

## 扩展示例

### 添加 Parquet 文件预览

```vue
<!-- components/previews/custom/ParquetPreview.vue -->
<template>
  <div class="parquet-preview">
    <el-tabs v-model="activeTab">
      <el-tab-pane label="数据" name="data">
        <el-table :data="tableData" height="400">
          <el-table-column
            v-for="col in columns"
            :key="col.name"
            :prop="col.name"
            :label="col.name"
          />
        </el-table>
      </el-tab-pane>

      <el-tab-pane label="Schema" name="schema">
        <pre>{{ schema }}</pre>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import parquetjs from 'parquetjs-lite'

const props = defineProps({
  data: Object
})

const activeTab = ref('data')
const tableData = ref([])
const columns = ref([])
const schema = ref('')

onMounted(async () => {
  // 从backend获取Parquet数据
  const response = await fetch(`/api/preview/parquet?path=${props.data.object.path}`)
  const result = await response.json()

  tableData.value = result.rows
  columns.value = result.schema.fields.map(f => ({ name: f.name }))
  schema.value = JSON.stringify(result.schema, null, 2)
})

// 插件配置
export const previewConfig = {
  name: 'parquet',
  canHandle: (data) => {
    const path = data.object?.path || ''
    return path.endsWith('.parquet')
  },
  priority: 30
}
</script>
```

只需将文件放入 `plugins/previews/custom/` 目录,系统自动识别!

---

## 总结

通过**插件化预览系统**:
1. **解决了文件过大问题** - 从2500行拆分为多个 < 300 行的组件
2. **提高了可扩展性** - 新增文件类型无需修改核心代码
3. **降低了维护成本** - 每个组件职责单一,修改影响范围小
4. **支持用户自定义** - 提供3种扩展方式,满足不同场景

这是一个**真正的企业级数据平台**应该有的架构设计! 🚀
