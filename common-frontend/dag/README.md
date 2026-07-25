# Common Frontend DAG 模块

DAG 编辑器共享组件库，提供可复用的 composable 和组件。

## 安装

```bash
# 在模块的 package.json 中添加
{
  "dependencies": {
    "@addp/common-frontend": "file:../../common-frontend"
  }
}
```

## 使用示例

### 基础 DAG 编辑器

```vue
<script setup>
import { ref, onMounted } from 'vue'
import {
  createDAGDirectEdgeBehavior,
  createDAGDragNodeBehavior,
  useDAGClipboard,
  useDAGCore,
  useDAGHistory,
  useDAGLayout,
  useDAGSelection,
  useDAGViewport,
  useLoopDetection
} from '@addp/common-frontend/dag'

const container = ref(null)
const { graph, initGraph } = useDAGCore(container, {
  modes: {
    default: [
      'drag-canvas',
      'zoom-canvas',
      createDAGDragNodeBehavior(),
      createDAGDirectEdgeBehavior({
        resolveSource: event => event.shape?.cfg?.portType === 'output' ? event.shape.cfg : null,
        resolveTarget: event => event.shape?.cfg?.portType === 'input' ? event.shape.cfg : null
      })
    ]
  }
})
const { hasLoop } = useLoopDetection(graph)
const { selectedItem, deleteSelected } = useDAGSelection(graph)
const { zoomIn, zoomOut, fitView, autoLayout } = useDAGViewport(graph)
const { captureLayout, restoreViewport } = useDAGLayout(graph)
const history = useDAGHistory({
  capture: () => graph.value.save(),
  restore: data => graph.value.changeData(data)
})
const clipboard = useDAGClipboard(graph, {
  createNodeId: node => `${node.id}-copy-${Date.now()}`
})

onMounted(() => {
  initGraph()
})
</script>

<template>
  <div ref="container" style="width: 100%; height: 600px;"></div>
</template>
```

### 只读 DAG 展示

```vue
<script setup>
import { DAGViewer } from '@addp/common-frontend/dag'

const dagData = {
  nodes: [
    { id: '1', label: '任务1', status: 'success' },
    { id: '2', label: '任务2', status: 'running' }
  ],
  edges: [
    { source: '1', target: '2' }
  ]
}
</script>

<template>
  <DAGViewer :dag-data="dagData" :height="400" />
</template>
```

## API

### Composables

- `useDAGCore(containerRef, options)` - G6 实例管理
- `useLoopDetection(graph)` - 循环检测
- `useDAGSelection(graph)` - 选中管理
- `useDAGViewport(graph, options)` - 缩放、适应窗口和自动布局
- `useDAGHistory(options)` - 撤销/重做历史，支持使用 `mergeKey` 合并连续参数输入
- `useDAGClipboard(graph, options)` - 节点复制和粘贴；只复制节点模型，不复制连线
- `useDAGLayout(graph)` - 捕获和恢复节点坐标及画布视口
- `createDAGDirectEdgeBehavior(options)` - 直接拖拽连线行为
- `createDAGDragNodeBehavior()` - 与端口拖拽互斥的节点拖动行为
- `validateDAGConnection(options)` - 统一校验无效端点、自环、环路和重复边

### 组件

- `DAGViewer` - 只读 DAG 展示组件

### 工具函数

- `generateColor(identifier)` - 根据模块名生成颜色
- `registerMultiPortNode()` - 注册多端口节点类型
- `normalizeDAGEditorLayout(layout)` - 规范化统一的 `editor_layout` 结构

共享画布统一限制在 `50%–150%` 缩放范围内，按钮按 `10%` 调整；滚轮缩放、适应窗口和持久化视口也遵循相同边界。

编辑器布局使用独立的展示状态，不得混入运行时 DAG：

```json
{
  "nodes": {
    "node_id": { "x": 120, "y": 240 }
  },
  "viewport": {
    "zoom": 1,
    "translate_x": 0,
    "translate_y": 0
  }
}
```
