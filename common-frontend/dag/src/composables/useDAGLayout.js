import { watch } from 'vue'
import { readBrowserPreference, writeBrowserPreference } from '../../../basic/src/utils/tenantUserBrowserPreference.js'
import {
  applyDAGNodePositions,
  captureDAGEditorLayout,
  captureDAGViewport,
  restoreDAGViewport
} from '../utils/editing.js'

export function useDAGLayout(graph, viewportKey, syncZoom) {
  let draftViewport = null
  function restoreViewport(fit) {
    if (!graph.value) return
    const key = viewportKey()
    const viewport = key ? readBrowserPreference(key) : draftViewport
    if (!viewport && fit) fit()
    else restoreDAGViewport(graph.value, viewport)
    syncZoom()
  }
  watch(viewportKey, () => {
    draftViewport = null
    restoreViewport()
  })

  return {
    captureLayout: () => captureDAGEditorLayout(graph.value),
    applyNodePositions: applyDAGNodePositions,
    restoreViewport,
    saveViewport() {
      if (!graph.value?.getNodes().length) return
      const viewport = captureDAGViewport(graph.value)
      const key = viewportKey()
      if (key) writeBrowserPreference(key, viewport)
      else draftViewport = viewport
    }
  }
}
