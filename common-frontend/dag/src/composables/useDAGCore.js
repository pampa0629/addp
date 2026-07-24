/**
 * DAG 核心 Composable - G6 实例管理和生命周期
 */
import { ref, onUnmounted } from 'vue'
import G6 from '@antv/g6'
import { DAG_MAX_ZOOM, DAG_MIN_ZOOM } from '../utils/editing.js'

export function useDAGCore(containerRef, options = {}) {
  const graph = ref(null)
  let resizeObserver = null
  const primaryColor = themeColor('--el-color-primary')
  const dangerColor = themeColor('--el-color-danger')
  const edgeColor = themeColor('--addp-text-tertiary')

  const defaultOptions = {
    minZoom: DAG_MIN_ZOOM,
    maxZoom: DAG_MAX_ZOOM,
    modes: {
      default: ['drag-canvas', 'zoom-canvas', 'drag-node', 'click-select']
    },
    nodeStateStyles: {
      selected: {
        stroke: dangerColor,
        lineWidth: 3
      }
    },
    edgeStateStyles: {
      selected: {
        stroke: dangerColor,
        lineWidth: 3
      },
      hover: {
        stroke: primaryColor,
        lineWidth: 3
      }
    },
    defaultEdge: {
      type: 'polyline',
      style: {
        stroke: edgeColor,
        lineWidth: 2,
        endArrow: {
          path: 'M 0,0 L 8,4 L 8,-4 Z',
          fill: edgeColor
        }
      }
    }
  }

  /**
   * 初始化 G6 图实例
   */
  function initGraph() {
    const container = containerRef.value
    if (!container) {
      console.error('[useDAGCore] 容器未找到')
      return null
    }

    const width = container.offsetWidth || 1200
    const height = container.offsetHeight || 600

    const config = {
      container,
      width,
      height,
      ...defaultOptions,
      ...options
    }

    graph.value = new G6.Graph(config)

    // 设置 ResizeObserver
    setupResizeObserver()

    return graph.value
  }

  /**
   * 设置容器尺寸监听
   */
  function setupResizeObserver() {
    if (!containerRef.value) return

    resizeObserver = new ResizeObserver(() => {
      if (graph.value && containerRef.value) {
        const w = containerRef.value.offsetWidth
        const h = containerRef.value.offsetHeight
        if (w > 0 && h > 0) {
          graph.value.changeSize(w, h)
        }
      }
    })

    resizeObserver.observe(containerRef.value)
  }

  /**
   * 销毁图实例
   */
  function destroyGraph() {
    if (resizeObserver) {
      resizeObserver.disconnect()
      resizeObserver = null
    }
    if (graph.value) {
      graph.value.destroy()
      graph.value = null
    }
  }

  // 组件卸载时自动清理
  onUnmounted(() => {
    destroyGraph()
  })

  /**
   * 加载数据到图中
   */
  function loadData(nodes, edges) {
    if (!graph.value) return
    graph.value.data({ nodes, edges })
    graph.value.render()
  }

  /**
   * 保存图数据
   */
  function saveData() {
    if (!graph.value) return { nodes: [], edges: [] }
    return graph.value.save()
  }

  return {
    graph,
    initGraph,
    destroyGraph,
    loadData,
    saveData
  }
}

function themeColor(name) {
  if (typeof document === 'undefined') return ''
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}
