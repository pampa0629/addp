/**
 * Common Frontend DAG 模块统一导出
 */

// Composables
export { useLoopDetection } from './composables/useLoopDetection.js'
export { useDAGCore } from './composables/useDAGCore.js'
export { useDAGEdgeMode } from './composables/useDAGEdgeMode.js'
export { useDAGSelection } from './composables/useDAGSelection.js'

// Nodes
export { registerMultiPortNode } from './nodes/MultiPortNode.js'
export { generateColor } from './nodes/SimpleNode.js'

// Components
export { default as DAGViewer } from './components/DAGViewer.vue'

// Web Components（供 amis 等非 Vue 环境使用）
export { registerDagViewerElement } from './web-components/dag-viewer-wc.js'

// Utils
export { DAG_SCHEMA_URL, NodeStatus, validateDAGSchema } from './utils/dagSchema.js'
