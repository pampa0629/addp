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
import { useDAGCore, useLoopDetection, useDAGSelection } from '@addp/common-frontend/dag'

const container = ref(null)
const { graph, initGraph } = useDAGCore(container)
const { hasLoop } = useLoopDetection(graph)
const { selectedItem, deleteSelected } = useDAGSelection(graph)

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
- `useDAGEdgeMode(graph)` - 连线模式

### 组件

- `DAGViewer` - 只读 DAG 展示组件

### 工具函数

- `generateColor(identifier)` - 根据模块名生成颜色
- `registerMultiPortNode()` - 注册多端口节点类型
