import {
  applyDAGNodePositions,
  captureDAGEditorLayout,
  normalizeDAGEditorLayout,
  restoreDAGViewport
} from '../utils/editing.js'

export function useDAGLayout(graph) {
  return {
    captureLayout: () => captureDAGEditorLayout(graph.value),
    normalizeLayout: normalizeDAGEditorLayout,
    applyNodePositions: applyDAGNodePositions,
    restoreViewport: layout => restoreDAGViewport(graph.value, layout)
  }
}
