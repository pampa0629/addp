/**
 * Common Frontend DAG 模块统一导出
 */

// Composables
export { useLoopDetection } from './composables/useLoopDetection.js'
export { useDAGCore } from './composables/useDAGCore.js'
export { useDAGSelection } from './composables/useDAGSelection.js'
export { useDAGViewport } from './composables/useDAGViewport.js'
export { useDAGHistory } from './composables/useDAGHistory.js'
export { useDAGClipboard } from './composables/useDAGClipboard.js'
export { useDAGLayout } from './composables/useDAGLayout.js'

// Nodes
export { registerMultiPortNode } from './nodes/MultiPortNode.js'
export { generateColor } from './nodes/SimpleNode.js'

// Components
export { default as DAGViewer } from './components/DAGViewer.vue'

// Web Components（供 amis 等非 Vue 环境使用）
export { registerDagViewerElement } from './web-components/dag-viewer-wc.js'

// Utils
export { DAG_SCHEMA_URL, NodeStatus, validateDAGSchema } from './utils/dagSchema.js'
export {
  createDAGDirectEdgeBehavior,
  createDAGDragNodeBehavior,
  isDAGPortEvent,
  linkPointPort,
  validateDAGConnection
} from './utils/directEdge.js'
export {
  createDAGKeyboardHandler,
  findAdjacentDAGNode,
  isDAGKeyboardEventFromEditableTarget,
  sortDAGNodesSpatially
} from './utils/keyboard.js'
export {
  getDAGIncomingEdgeModels,
  getDAGUpstreamCandidates
} from './utils/connections.js'
export {
  applyDAGNodePositions,
  captureDAGEditorLayout,
  clampDAGZoom,
  cloneDAGNodeForPaste,
  cloneDAGValue,
  createDAGHistoryStore,
  DAG_MAX_ZOOM,
  DAG_MIN_ZOOM,
  normalizeDAGEditorLayout,
  restoreDAGViewport
} from './utils/editing.js'
