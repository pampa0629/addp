# Common Frontend DAG 模块

[返回共享前端总入口](../README.md) · [公共导出](src/index.js) · [接入与依赖](../docs/ARCHITECTURE.md#接入与验证) · [共享测试与覆盖边界](../README.md#测试) · [消费模块浏览器隔离](../README.md#浏览器测试隔离)

DAG 共享前端库，提供可编辑画布的 composable、交互与布局工具，以及只读工作流查看器。业务模块维护节点参数、执行与授权等领域语义。

本页内容：[安装](#安装)、[编辑器接入](#基础-dag-编辑器)、[只读展示示例](#只读-dag-展示)、[API 分类](#api)、[交互与布局约束](#交互与布局约束)、[选择与依赖候选](#选择与依赖候选)、[复制粘贴与删除](#复制粘贴与删除)、[编辑器主题与状态保持](#编辑器主题与状态保持)、[布局恢复与自动布局](#布局恢复与自动布局)、[撤销重做与历史恢复](#撤销重做与历史恢复)、[验证分工](#验证分工)。

## 安装

依赖声明、别名、Vue 单一实例和主题／语言初始化统一按[新模块前端接入指南](../../docs/spec/addp新模块开发指南.md#创建新模块前端)配置。DAG 的 G6 等依赖由消费模块维护，版本遵循[技术栈规约](../../docs/spec/addp技术栈规约.md#地图与可视化)。

## 使用示例

### 基础 DAG 编辑器

编辑器的初始化、节点／连线映射及事件接线分别参考 [Develop 工作流画布](../../develop/frontend/src/components/workflow/WorkflowDAGCanvas.vue)、[Orchestrator 编排画布](../../orchestrator/frontend/src/components/DAGEditor.vue)。端口映射、节点 ID 生成与领域通知由各消费方维护。

接入时按[API 分类](#api)定位共享入口，按[交互与布局约束](#交互与布局约束)选择具体契约；工具、页面与真实链路的验证分工见[验证分工](#验证分工)。

### 只读 DAG 展示

`DAGViewer` 接收包含 `tasks` 的工作流对象，以 `id` 定位节点、`operator` 显示名称、`depends_on` 生成连线；本例只演示展示，不执行算子或证明执行状态。输入映射见[查看器实现](src/components/DAGViewer.vue)。

只读组件与 Web Component 的职责、主题、视口及清理要求见[只读 DAG 查看器](../docs/components/预览与结果展示.md#只读-dag-查看器)。

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

以下为常用入口，完整导出以 [src/index.js](src/index.js) 为准；验证层次见[验证分工](#验证分工)。

本节导航：[Composables](#composables)、[组件](#组件)、[工具函数](#工具函数)、[连线与键盘交互](#连线与键盘交互)、[布局、历史与主题](#布局历史与主题)、[节点与结构](#节点与结构)。

### Composables

- `useDAGCore(containerRef, options)` - G6 实例管理
- `useLoopDetection(graph)` - 循环检测
- `useDAGSelection(graph, { focusTarget })` - 选中与操作入口；选择状态、焦点及修改职责见[选择状态与操作归属](#选择状态与操作归属)。
- `useDAGViewport(graph, options)` - 缩放、适应窗口和自动布局
- `useDAGHistory(options)` - 编辑历史入口；记录、合并与恢复职责见[撤销重做与历史恢复](#撤销重做与历史恢复)。
- `useDAGClipboard(graph, options)` - 节点剪贴板入口；复制范围与宿主处理见[节点复制与粘贴](#节点复制与粘贴)。
- `useDAGLayout(graph)` - 布局捕获、坐标应用与视口恢复入口，见[布局恢复与自动布局](#布局恢复与自动布局)。
- `useDAGTheme(refresh)` - 主题观察入口；宿主重绘、状态保持与卸载职责见[编辑器主题与状态保持](#编辑器主题与状态保持)。

### 组件

- `DAGViewer` - 只读 DAG 展示组件
- `registerDagViewerElement()` - 注册 `addp-dag-viewer` 原生元素，将 JSON `dag-data` 和数值 `height` 适配到同一个 `DAGViewer`；已有登记时不重复注册，参见[原生元素适配](src/web-components/dag-viewer-wc.js)。

### 工具函数

#### 连线与键盘交互

- `createDAGDirectEdgeBehavior(options)` - 直接拖拽连线行为
- `createDAGDragNodeBehavior()` - 与端口拖拽互斥的节点拖动行为
- `createDAGKeyboardHandler(actions)` - 快捷键适配入口；事件范围与宿主回调见[画布焦点与快捷键](#画布焦点与快捷键)。
- `validateDAGConnection(options)` - 基础连线校验；拒绝条件、判重与领域职责见[依赖写入与连线校验](#依赖写入与连线校验)。
- `getDAGIncomingEdgeModels(graph, targetId)` - 当前入边查询，见[上游候选与提交校验](#上游候选与提交校验)。
- `getDAGUpstreamCandidates(options)` - 上游候选投影，标记规则与领域校验见[上游候选与提交校验](#上游候选与提交校验)。
- `focusDAGConnections(graph, item)` - 关联连线展示聚焦，见[关联连线展示与重置](#关联连线展示与重置)。

端口事件识别与键盘节点排序等辅助函数分别见[连线工具](src/utils/directEdge.js)和[键盘工具](src/utils/keyboard.js)；共享层提供的校验范围与消费模块职责见[键盘与依赖编辑](#键盘与依赖编辑)。

#### 布局、历史与主题

- `normalizeDAGEditorLayout(layout)` - 规范化统一的 `editor_layout` 结构
- 布局、复制及历史存储的独立工具由[编辑工具](src/utils/editing.js)维护；接入分别沿[布局恢复](#布局恢复与自动布局)、[历史恢复](#撤销重做与历史恢复)，业务模块复用相应 composable。
- `updateDAGItemTheme(graph, item, appearance)` - 更新节点或连线的基础样式后，恢复原有 G6 活动状态；与 `useDAGTheme` 配合使用，不由共享层推断领域配色。

#### 节点与结构

- `generateColor(identifier)` - 根据模块名生成颜色
- `registerMultiPortNode()` - 注册多端口节点类型
- `DAG_SCHEMA_URL`、`NodeStatus`、`validateDAGSchema(data)` 的声明见[基础结构工具](src/utils/dagSchema.js)。`validateDAGSchema` 只检查对象、固定 `$schema` 及 `nodes/edges` 数组，不检查节点字段、环路、算子、权限或可执行性；`DAGViewer` 独立读取 `tasks`，不调用该校验器转换数据。

## 交互与布局约束

### 键盘与依赖编辑

本节导航：[焦点与快捷键](#画布焦点与快捷键) · [依赖与连线校验](#依赖写入与连线校验)。无障碍播报要求见[交互专题](../docs/frontend-style/组件交互.md#6-dag-编辑交互)。

#### 画布焦点与快捷键

可编辑画布容器必须可通过 `Tab` 聚焦，使用 `role="region"`、国际化的 `aria-label` 和共享 `addp-dag-focus-region` 类。快捷键仅在画布区域持有焦点时生效；左右/上下方向键按节点的横坐标、纵坐标和 ID 稳定顺序循环选择节点，Enter 激活节点配置，`Esc` 清除当前选择。输入框、文本域、下拉框和可编辑区域中的删除、复制、粘贴行为不得被画布接管。

宿主将 `createDAGKeyboardHandler(actions)` 绑定到画布 `keydown`，由该绑定限定焦点范围；共享处理器不自行检查当前活动元素。撤销、重做、复制、粘贴、快速复制、删除及上述导航动作均由宿主注入回调，未提供的动作不接管事件，已处理或来自编辑控件的事件被忽略；具体键位统一见[键盘工具](src/utils/keyboard.js)。选择与焦点移交见[选择状态与操作归属](#选择状态与操作归属)。

#### 依赖写入与连线校验

DAG 连线不得要求进入全局“连线模式”。画布边是依赖关系的唯一编辑事实源，表单操作必须复用同一套环路、重复连接校验和历史记录。

键盘用户通过节点参数面板或配置抽屉维护依赖关系，鼠标用户仍可直接拖拽端口连线。共享 DAG 只提供连接查询与基础校验；端口级输入绑定、步骤级 `depends_on` 等领域语义由消费模块维护。

`validateDAGConnection` 对缺少端点 ID、自环及宿主 `hasLoop` 检出的环路返回 `loop`，对已有重复连接返回 `duplicate`，通过时返回 `true`；默认按源、目标节点对判重，端口级编辑器通过 `isDuplicate` 定义连接唯一性。端口可绑定性等领域条件由宿主继续校验，不把基础校验通过当作完整依赖可用。

`createDAGDirectEdgeBehavior` 通过宿主 `canConnect` 接入上述校验，只有结果严格为 `true` 才提交连线并发出 `aftercreateedge`；拒绝或拖拽结束取消时清除临时边，不发成功事件。成功修改由宿主记录历史并同步领域状态；基线、合并和恢复规则统一见[撤销重做与历史恢复](#撤销重做与历史恢复)。

### 选择与依赖候选

本节导航：[选择状态](#选择状态与操作归属) · [关联连线展示](#关联连线展示与重置) · [上游候选](#上游候选与提交校验)。

#### 选择状态与操作归属

`useDAGSelection` 维护当前 `selectedItem`；图就绪后由宿主注册 `initSelectionListener()` 接收选择事件，`selectItem()` 更新图项的选中状态，`clearSelection()` 清除当前选择。鼠标选择节点或连线后可通过 `useDAGSelection(graph, { focusTarget })` 将焦点移交给画布容器。键盘范围与节点顺序统一见[画布焦点与快捷键](#画布焦点与快捷键)。

删除与清空属于图数据修改，助手范围和宿主的绑定清理统一见[删除清空与绑定清理](#删除清空与绑定清理)；共享选择行为以[选择 composable](src/composables/useDAGSelection.js)为准。

#### 关联连线展示与重置

`focusDAGConnections(graph, item)` 聚焦节点的直接关联连线或单条连线，传 `null` 恢复全图透明度；只调整渲染透明度与聚焦连线的绘制层级，不写入节点、边或布局模型。

节点聚焦包含该节点的直接入边和出边，不递归展开祖先或后代；聚焦单条边按图项区分平行连线。该工具不设置 `selectedItem` 或记录编辑历史；选择、详情和重置的联动由宿主组合。Orchestrator 的关联面板随当前选择更新，悬停不替换点击选择，空白画布或清除选择后恢复全图，接入见[编排画布](../../orchestrator/frontend/src/components/DAGEditor.vue)。

#### 上游候选与提交校验

`getDAGIncomingEdgeModels(graph, targetId)` 只读取目标节点的当前直接入边模型。`getDAGUpstreamCandidates({ graph, targetId, hasLoop })` 从当前画布中排除目标自身和无 ID 节点，按已有入边标记 `connected`；仅未连接候选按宿主 `hasLoop` 标记 `disabled`，已连接候选不据此禁用，目标 ID 为空时返回空列表。共享查询与展示聚焦均由[关联查询与聚焦工具](src/utils/connections.js)维护。

候选表示当前画布上的节点关系，不等于已通过端口类型、绑定或执行授权校验。Develop 继续按输入／输出端口类型组合候选，Orchestrator 按步骤依赖与参数／控制连线处理写入；领域映射见[编辑器接入](#基础-dag-编辑器)。提交仍复用[依赖写入与连线校验](#依赖写入与连线校验)，成功编辑才进入宿主历史；不在候选查询中创建连线或写历史。

### 复制粘贴与删除

本节导航：[节点复制](#节点复制与粘贴) · [删除与绑定清理](#删除清空与绑定清理) · [记录与验收](#复制删除的记录与验收)。

#### 节点复制与粘贴

`useDAGClipboard` 的 `copy(item)` 只接收节点并保存模型快照；只复制节点模型，不复制连线。普通参数随模型克隆，输入绑定的领域处理见[绑定清理](#删除清空与绑定清理)。`paste({ position })` 用宿主 `createNodeId` 生成新 ID，按指定位置或累积偏移创建单个节点；图、已复制节点或 ID 生成函数缺失时返回 `null`。ID 生成与冲突控制由宿主负责，共享助手不记录历史或发送领域通知。

`clear()` 只清空剪贴板快照与粘贴计数，不删除已粘贴节点。快速复制由宿主组合复制和粘贴；共享复制、定位和清空行为以[剪贴板 composable](src/composables/useDAGClipboard.js)与[节点克隆工具](src/utils/editing.js)为准。

#### 删除清空与绑定清理

`useDAGSelection` 的 `deleteSelected()` 删除当前选中的图项，`clearGraph()` 清空图并清除选择，均属于图数据修改；这些助手不记录历史或发送领域通知。清除选择的展示行为另见[选择状态与操作归属](#选择状态与操作归属)。宿主负责连线、参数绑定、依赖与详情的同步，具体入口见[编辑器接入](#基础-dag-编辑器)。

Develop 在输出工作流时先按已声明的输入参数清除旧引用，再依据当前画布边重建输入引用和 `depends_on`；该投影用于粘贴与删除后的输出，不代表共享克隆已清理模型内所有嵌套参数。删除节点时清除当前详情，删除边时按目标节点刷新已打开的对应详情，行为见[工作流画布](../../develop/frontend/src/components/workflow/WorkflowDAGCanvas.vue)及[输入引用工具](../../develop/frontend/src/utils/workflowInputBindings.js)。

Orchestrator 粘贴后清除已识别的输入参数绑定；删除参数边前清理目标输入绑定，删除源节点前清理其输出参数边对应的目标绑定，删除后同步依赖与当前详情。绑定识别与清理按声明的输入端口及路径处理，见[编排画布](../../orchestrator/frontend/src/components/DAGEditor.vue)和[参数绑定工具](../../orchestrator/frontend/src/utils/orchestrationPorts.js)。清空画布的确认、取消及编辑记录由消费方入口处理。

#### 复制删除的记录与验收

复制和剪贴板清空不产生图编辑历史；成功粘贴、删除或清空画布后，由宿主记录编辑并同步领域状态。基线、合并、撤销重做统一见[历史恢复](#撤销重做与历史恢复)，恢复后配色见[主题与状态保持](#编辑器主题与状态保持)，按钮和快捷键沿同一宿主操作入口。操作验收见[验证分工](#验证分工)。

### 编辑器主题与状态保持

Develop 与 Orchestrator 的可编辑 DAG 通过 `useDAGTheme` 共用主题观察与卸载清理。主题刷新原位更新节点所有子形状、端口、数据/控制连线、描边底线和执行标记，并更新 G6 缓存的状态样式后恢复选中、悬停与校验状态；不重载工作流，不写入布局或撤销历史；撤销、重做与粘贴恢复的数据立即按当前主题重绘，不复活历史配色。

`useDAGTheme(refresh)` 挂载后观察根元素的主题 class／style 变化并调用宿主刷新函数，卸载时断开主题观察；`useDAGCore` 负责卸载时断开尺寸观察、销毁图实例并清除引用。宿主负责领域节点、连线和执行标记的主题投影，共享层不推断领域配色。

视口边界、布局持久化与等待自动布局完成的要求统一见[布局恢复与自动布局](#布局恢复与自动布局)。只读展示的输入、自动适应与清理另见[只读 DAG 查看器](../docs/components/预览与结果展示.md#只读-dag-查看器)。

### 布局恢复与自动布局

本节导航：[视口边界](#编辑器视口边界) · [坐标与视口恢复](#编辑器坐标与视口恢复) · [等待自动布局完成](#等待自动布局完成)。

#### 编辑器视口边界

使用 `useDAGViewport` 的可编辑画布统一限制在 `50%–150%` 缩放范围内，按钮按 `10%` 调整；滚轮缩放、适应窗口和持久化视口也遵循相同边界。

#### 编辑器坐标与视口恢复

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

`useDAGLayout(graph)` 的 `captureLayout()` 捕获节点坐标及视口，`normalizeLayout(layout)` 统一布局结构；`applyNodePositions(nodes, layout)` 返回应用已存坐标的节点列表，不直接修改图实例。宿主将这些节点载入画布后，再用 `restoreViewport(layout)` 恢复缩放和平移；该方法不恢复节点或重新排列。使用 `useDAGViewport` 的宿主随后调用 `syncZoom()` 同步缩放控件状态。方法实现统一见[布局 composable](src/composables/useDAGLayout.js)与[编辑工具](src/utils/editing.js)。

#### 等待自动布局完成

`useDAGViewport().autoLayout({ layout, fit })` 可按当前图传入布局配置及视口适配函数，省略时使用 composable 的配置；在 `afterlayout` 后适配视图并返回 Promise；需要记录布局历史或保存坐标时应等待完成。节点实际宽高通过 G6 节点模型的 `size: [width, height]` 提供。

恢复已存布局与触发自动布局是不同动作；后者会重新排列并适配视口。历史记录规则见[撤销重做与历史恢复](#撤销重做与历史恢复)，主题及观察器清理见[编辑器主题与状态保持](#编辑器主题与状态保持)。

### 撤销重做与历史恢复

`useDAGHistory({ capture, restore })` 由宿主提供快照采集与恢复回调，不自动监听图变更。载入工作流后用 `reset(snapshot)` 建立新基线并丢弃旧历史；业务编辑完成后调用 `record()`，连续参数输入可通过相同 `mergeKey` 在合并窗口内合并。合并窗口、历史容量和快照去重统一由[历史存储实现](src/utils/editing.js)维护；撤销后记录新变更会丢弃旧的重做分支。

`undo()`／`redo()` 将克隆后的快照交给 `restore`，当前 [history composable](src/composables/useDAGHistory.js) 同步调用该回调，不等待异步恢复；恢复期间的 `record()` 被忽略。宿主负责画布重载、旧选择清理和领域通知；恢复后的配色统一按[编辑器主题与状态保持](#编辑器主题与状态保持)处理。编辑快照不等于已持久化保存或业务执行回滚。

## 验证分工

标准入口及现有 CI 归属统一见[测试与验收导航](../docs/components/预览与结果展示.md#测试与验收导航)，共享能力与唯一所有权沿[共享测试入口](../README.md#测试)。下表定位断言与消费方分工，入口登记、源码引用和用例存在不等于当前版本执行通过或已覆盖全部分支。

基础结构校验、共享工具、消费方页面和真实后端分别验证；共享门禁不代替消费方回归，只读组件回归不代替可编辑画布。示例、工具夹具、连线成功或受控页面均不证明业务执行或授权；未运行、失败、跳过或未覆盖项不计通过。

| 场景 | 独有断言与归属 |
| --- | --- |
| 只读展示与 Web Component | 主题、视口、数据清空、尺寸与卸载断言统一由[只读 DAG 查看器](../docs/components/预览与结果展示.md#只读清理与验收)维护，消费方页面与真实工作流执行另验。 |
| 键盘与依赖编辑 | 工具断言与消费方页面分别核对画布焦点、编辑控件事件不被接管、快捷键回调、环路与重复边拒绝、取消临时边，以及成功编辑的撤销重做。 |
| 选择、关联聚焦与候选 | [聚焦工具断言](../basic/tests/dagConnectionFocus.test.mjs)覆盖平行连线、全图恢复和模型不变，归共享门禁；[候选标记断言](../../develop/frontend/tests/commonDAGDirectEdge.test.mjs)归 Develop 门禁。消费方页面核对选择、悬停、详情重置与编辑恢复。 |
| 剪贴板与删除 | [克隆工具断言](../../develop/frontend/tests/commonDAGEditing.test.mjs)核对示例节点的新 ID、偏移坐标及参数内容；页面分别核对复制后的依赖与参数、删除后的连线及绑定清理、撤销重做、清空取消和实际保存投影。现有 [Orchestrator 受控用例](../../orchestrator/frontend/e2e/dag-layout-persistence.spec.js)包括复制不带控制边、删除最后一条参数边后的绑定／隐式依赖清理，不能外推为所有分支已验收。 |
| 编辑器主题与状态 | Develop 与 Orchestrator 的真实页面浏览器用例分别验证主题与编辑状态保持、恢复后配色。 |
| 布局与历史 | 布局、视口和历史分支的[工具断言](../../develop/frontend/tests/commonDAGEditing.test.mjs)由 Develop 门禁执行；消费方另验布局持久化、撤销重做、状态保持及清理。 |
