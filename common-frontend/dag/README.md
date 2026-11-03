# Common Frontend DAG 模块

[返回共享前端总入口](../README.md) · [公共导出](src/index.js) · [接入与依赖](../docs/ARCHITECTURE.md#接入与验证) · [共享测试与覆盖边界](../README.md#测试) · [消费模块浏览器隔离](../README.md#浏览器测试隔离)

DAG 共享前端库，提供可编辑画布的 composable、交互与布局工具，以及只读工作流查看器。业务模块维护节点参数、执行与授权等领域语义。

本页内容：[安装](#安装)、[基础编辑示例](#基础-dag-编辑器)、[只读展示示例](#只读-dag-展示)、[API 分类](#api)、[交互与布局约束](#交互与布局约束)、[编辑器主题与状态保持](#编辑器主题与状态保持)。

## 安装

依赖声明、别名、Vue 单一实例和主题／语言初始化统一按[新模块前端接入指南](../../docs/spec/addp新模块开发指南.md#创建新模块前端)配置。DAG 的 G6 等依赖由消费模块维护，版本遵循[技术栈规约](../../docs/spec/addp技术栈规约.md#地图与可视化)。

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

`DAGViewer` 接收包含 `tasks` 的工作流对象，以 `id` 定位节点、`operator` 显示名称、`depends_on` 生成连线；本例只演示展示，不执行算子或证明执行状态。输入映射见[查看器实现](src/components/DAGViewer.vue)。

只读组件与 Web Component 的职责、主题、视口及清理要求见[只读 DAG 查看器](../docs/components/预览与结果展示.md#只读-dag-查看器)；消费方标准门禁统一见[测试与验收导航](../docs/components/预览与结果展示.md#测试与验收导航)，共享门禁不代替消费方验证。

```vue
<script setup>
import { DAGViewer } from '@addp/common-frontend/dag'

const dagData = {
  tasks: [
    { id: '1', operator: 'Source', params: {}, depends_on: [] },
    { id: '2', operator: 'Transform', params: {}, depends_on: ['1'] }
  ]
}
</script>

<template>
  <DAGViewer :dag-data="dagData" :height="400" />
</template>
```

## API

以下为常用入口，完整导出以 [src/index.js](src/index.js) 为准。按职责选择入口；基础结构校验、画布展示和领域执行各自验证。

本节导航：[Composables](#composables)、[组件](#组件)、[工具函数](#工具函数)、[连线与键盘交互](#连线与键盘交互)、[布局、历史与主题](#布局历史与主题)、[节点与结构](#节点与结构)。

### Composables

- `useDAGCore(containerRef, options)` - G6 实例管理
- `useLoopDetection(graph)` - 循环检测
- `useDAGSelection(graph, { focusTarget })` - 选中管理；鼠标选择节点或连线后可把焦点移交给画布容器，并按画布坐标循环选择前后节点
- `useDAGViewport(graph, options)` - 缩放、适应窗口和自动布局
- `useDAGHistory(options)` - 撤销/重做历史，支持使用 `mergeKey` 合并连续参数输入
- `useDAGClipboard(graph, options)` - 节点复制和粘贴；只复制节点模型，不复制连线
- `useDAGLayout(graph)` - 捕获和恢复节点坐标及画布视口
- `useDAGTheme(refresh)` - 主题观察入口；宿主重绘、状态保持与卸载职责见[编辑器主题与状态保持](#编辑器主题与状态保持)。

### 组件

- `DAGViewer` - 只读 DAG 展示组件
- `registerDagViewerElement()` - 注册 `addp-dag-viewer` 原生元素，将 JSON `dag-data` 和数值 `height` 适配到同一个 `DAGViewer`；已有登记时不重复注册，参见[原生元素适配](src/web-components/dag-viewer-wc.js)。

### 工具函数

#### 连线与键盘交互

- `createDAGDirectEdgeBehavior(options)` - 直接拖拽连线行为
- `createDAGDragNodeBehavior()` - 与端口拖拽互斥的节点拖动行为
- `createDAGKeyboardHandler(actions)` - 统一处理撤销、重做、复制、粘贴、快速复制、删除、方向键节点导航、Enter 激活和 `Esc` 取消选择，并忽略输入控件中的键盘事件
- `validateDAGConnection(options)` - 统一校验无效端点、自环、环路和重复边；端口级编辑器可通过 `isDuplicate` 定义连接唯一性
- `getDAGIncomingEdgeModels(graph, targetId)` - 查询目标节点的当前入边模型
- `getDAGUpstreamCandidates(options)` - 查询可作为目标节点上游的候选节点，并标记当前连接和环路禁用状态

`focusDAGConnections(graph, item)` 聚焦节点的直接关联连线或单条连线，传 `null` 恢复全图；只改变渲染透明度，不写入节点、边或布局模型。

端口事件识别与键盘节点排序等辅助函数分别见[连线工具](src/utils/directEdge.js)和[键盘工具](src/utils/keyboard.js)；共享层提供的校验范围与消费模块职责见[键盘与依赖编辑](#键盘与依赖编辑)。

#### 布局、历史与主题

- `normalizeDAGEditorLayout(layout)` - 规范化统一的 `editor_layout` 结构
- 布局捕获、坐标应用、视口恢复、复制及历史存储的独立工具见[编辑工具](src/utils/editing.js)；缩放上限与下限由同一实现维护，业务模块复用相应 composable。
- `updateDAGItemTheme(graph, item, appearance)` - 更新节点或连线的基础样式后，恢复原有 G6 活动状态；与 `useDAGTheme` 配合使用，不由共享层推断领域配色。

#### 节点与结构

- `generateColor(identifier)` - 根据模块名生成颜色
- `registerMultiPortNode()` - 注册多端口节点类型
- `DAG_SCHEMA_URL`、`NodeStatus`、`validateDAGSchema(data)` 的声明见[基础结构工具](src/utils/dagSchema.js)。`validateDAGSchema` 只检查对象、固定 `$schema` 及 `nodes/edges` 数组，不检查节点字段、环路、算子、权限或可执行性；`DAGViewer` 独立读取 `tasks`，不调用该校验器转换数据。

## 交互与布局约束

### 键盘与依赖编辑

可编辑画布容器必须可通过 `Tab` 聚焦，使用 `role="region"`、国际化的 `aria-label` 和共享 `addp-dag-focus-region` 类。快捷键仅在画布区域持有焦点时生效；左右/上下方向键按节点的横坐标、纵坐标和 ID 稳定顺序循环选择节点，Enter 激活节点配置，`Esc` 清除当前选择。输入框、文本域、下拉框和可编辑区域中的删除、复制、粘贴行为不得被画布接管。

键盘用户通过节点参数面板或配置抽屉维护依赖关系，鼠标用户仍可直接拖拽端口连线。共享 DAG 只提供连接查询与基础校验；端口级输入绑定、步骤级 `depends_on` 等领域语义由消费模块维护。

### 编辑器主题与状态保持

Develop 与 Orchestrator 的可编辑 DAG 通过 `useDAGTheme` 共用主题观察与卸载清理。主题刷新原位更新节点所有子形状、端口、数据/控制连线、描边底线和执行标记，并更新 G6 缓存的状态样式后恢复选中、悬停与校验状态；不重载工作流，不写入布局或撤销历史；撤销、重做与粘贴恢复的数据立即按当前主题重绘，不复活历史配色。

`useDAGTheme(refresh)` 挂载后观察根元素的主题 class／style 变化并调用宿主刷新函数，卸载时断开主题观察；`useDAGCore` 负责卸载时断开尺寸观察、销毁图实例并清除引用。宿主负责领域节点、连线和执行标记的主题投影，共享层不推断领域配色。

视口边界、布局持久化与等待自动布局完成的要求统一见[布局恢复与自动布局](#布局恢复与自动布局)。只读展示的输入、自动适应与清理另见[只读 DAG 查看器](../docs/components/预览与结果展示.md#只读-dag-查看器)。

Develop 与 Orchestrator 的真实页面浏览器用例分别验证主题与编辑状态保持、恢复后配色；标准门禁及现有前端 CI 分工见[测试与验收导航](../docs/components/预览与结果展示.md#测试与验收导航)。只读组件回归不能替代编辑器消费验证，画布验证不证明领域执行或授权；未运行或未覆盖项不计通过。

### 布局恢复与自动布局

使用 `useDAGViewport` 的可编辑画布统一限制在 `50%–150%` 缩放范围内，按钮按 `10%` 调整；滚轮缩放、适应窗口和持久化视口也遵循相同边界。

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

`useDAGViewport().autoLayout({ layout, fit })` 可按当前图传入布局配置及视口适配函数，省略时使用 composable 的配置；在 `afterlayout` 后适配视图并返回 Promise；需要记录布局历史或保存坐标时应等待完成。节点实际宽高通过 G6 节点模型的 `size: [width, height]` 提供。
