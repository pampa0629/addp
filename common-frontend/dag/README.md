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
  createDAGKeyboardHandler,
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
- `useDAGSelection(graph, { focusTarget })` - 选中管理；鼠标选择节点或连线后可把焦点移交给画布容器，并按画布坐标循环选择前后节点
- `useDAGViewport(graph, options)` - 缩放、适应窗口和自动布局
- `useDAGHistory(options)` - 撤销/重做历史，支持使用 `mergeKey` 合并连续参数输入
- `useDAGClipboard(graph, options)` - 节点复制和粘贴；只复制节点模型，不复制连线
- `useDAGLayout(graph)` - 捕获和恢复节点坐标及画布视口
- `createDAGDirectEdgeBehavior(options)` - 直接拖拽连线行为
- `createDAGDragNodeBehavior()` - 与端口拖拽互斥的节点拖动行为
- `createDAGKeyboardHandler(actions)` - 统一处理撤销、重做、复制、粘贴、快速复制、删除、方向键节点导航、Enter 激活和 `Esc` 取消选择，并忽略输入控件中的键盘事件
- `validateDAGConnection(options)` - 统一校验无效端点、自环、环路和重复边；端口级编辑器可通过 `isDuplicate` 定义连接唯一性
- `getDAGIncomingEdgeModels(graph, targetId)` - 查询目标节点的当前入边模型
- `getDAGUpstreamCandidates(options)` - 查询可作为目标节点上游的候选节点，并标记当前连接和环路禁用状态

### 组件

- `DAGViewer` - 只读 DAG 展示组件

### 工具函数

- `generateColor(identifier)` - 根据模块名生成颜色
- `registerMultiPortNode()` - 注册多端口节点类型
- `normalizeDAGEditorLayout(layout)` - 规范化统一的 `editor_layout` 结构

共享画布统一限制在 `50%–150%` 缩放范围内，按钮按 `10%` 调整；滚轮缩放、适应窗口和持久化视口也遵循相同边界。

可编辑画布容器必须可通过 `Tab` 聚焦，使用 `role="region"`、国际化的 `aria-label` 和共享 `addp-dag-focus-region` 类。快捷键仅在画布区域持有焦点时生效；左右/上下方向键按节点的横坐标、纵坐标和 ID 稳定顺序循环选择节点，Enter 激活节点配置，`Esc` 清除当前选择。输入框、文本域、下拉框和可编辑区域中的删除、复制、粘贴行为不得被画布接管。

键盘用户通过节点参数面板或配置抽屉维护依赖关系，鼠标用户仍可直接拖拽端口连线。共享 DAG 只提供连接查询与基础校验；端口级输入绑定、步骤级 `depends_on` 等领域语义由消费模块维护。

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
