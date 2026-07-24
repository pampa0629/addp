import { computed, ref } from 'vue'
import {
  calculateDAGFitViewport,
  clampDAGZoom,
  DAG_MAX_ZOOM,
  DAG_MIN_ZOOM
} from '../utils/editing.js'

const ZOOM_STEP = 0.1
const ZOOM_EPSILON = 0.0001

export function useDAGViewport(graph, {
  fitPadding = 32,
  layout = {
    type: 'dagre',
    rankdir: 'LR',
    nodesep: 48,
    ranksep: 96
  }
} = {}) {
  const zoom = ref(1)
  const canZoomIn = computed(() => zoom.value < DAG_MAX_ZOOM - ZOOM_EPSILON)
  const canZoomOut = computed(() => zoom.value > DAG_MIN_ZOOM + ZOOM_EPSILON)

  function syncZoom() {
    zoom.value = graph.value?.getZoom?.() || 1
    return zoom.value
  }

  function zoomTo(value) {
    if (!graph.value) return
    const next = clampDAGZoom(value)
    graph.value.zoomTo(next)
    syncZoom()
  }

  function zoomIn() {
    zoomTo(syncZoom() + ZOOM_STEP)
  }

  function zoomOut() {
    zoomTo(syncZoom() - ZOOM_STEP)
  }

  function fitView() {
    const instance = graph.value
    if (!instance || instance.getNodes().length === 0) return

    const group = instance.get('group')
    const previousMatrix = group.getMatrix() ? [...group.getMatrix()] : null
    group.resetMatrix()
    const viewport = calculateDAGFitViewport({
      width: instance.get('width'),
      height: instance.get('height'),
      bbox: group.getCanvasBBox(),
      padding: fitPadding
    })

    if (!viewport) {
      if (previousMatrix) group.setMatrix(previousMatrix)
      instance.paint()
      syncZoom()
      return
    }

    instance.translate(viewport.translate.x, viewport.translate.y)
    instance.zoomTo(viewport.zoom, viewport.center)
    syncZoom()
  }

  function autoLayout() {
    if (!graph.value || graph.value.getNodes().length === 0) return
    graph.value.updateLayout({ ...layout })
    fitView()
  }

  return {
    zoom,
    canZoomIn,
    canZoomOut,
    zoomIn,
    zoomOut,
    fitView,
    autoLayout,
    syncZoom
  }
}
