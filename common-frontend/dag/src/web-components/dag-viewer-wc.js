/**
 * DAGViewer Web Component 封装
 *
 * 将 Vue 3 DAGViewer 组件封装为浏览器原生 Custom Element，
 * 可在 amis 等非 Vue 环境中以 <addp-dag-viewer> 标签使用。
 *
 * 属性：
 *   dag-data   - JSON 字符串，工作流数据（tasks/edges）
 *   height     - 数字，图表高度（像素），默认 400
 *
 * 示例：
 *   <addp-dag-viewer dag-data='{"tasks":[...]}' height="400"></addp-dag-viewer>
 *
 *   // 或通过 JS property 传入对象（推荐，避免 JSON 序列化）：
 *   element.dagData = { tasks: [...] }
 */

import { defineCustomElement, h, ref, watch } from 'vue'
import G6 from '@antv/g6'
import { registerMultiPortNode } from '../nodes/MultiPortNode.js'

/**
 * 内部包装组件：处理 attribute（字符串）和 property（对象）两种输入方式
 */
const DAGViewerWCInner = {
  props: {
    // 接收 JSON 字符串（来自 dag-data attribute）
    dagData: {
      type: String,
      default: ''
    },
    height: {
      type: [Number, String],
      default: 400
    }
  },
  emits: [],
  setup(props) {
    const containerRef = ref(null)
    const graph = ref(null)
    let resizeObserver = null

    function getParsedData() {
      if (!props.dagData) return null
      try {
        return JSON.parse(props.dagData)
      } catch {
        return null
      }
    }

    function initGraph() {
      if (!containerRef.value) return
      registerMultiPortNode()

      const width = containerRef.value.offsetWidth || 800
      const h = containerRef.value.offsetHeight || Number(props.height) || 400

      graph.value = new G6.Graph({
        container: containerRef.value,
        width,
        height: h,
        modes: { default: ['drag-canvas', 'zoom-canvas'] },
        defaultNode: { type: 'workflow-node', size: [140, 60] },
        defaultEdge: {
          type: 'polyline',
          style: {
            stroke: '#A3B1BF',
            lineWidth: 2,
            radius: 10,
            endArrow: { path: G6.Arrow.triangle(10, 12, 0), fill: '#A3B1BF', d: 0 }
          }
        }
      })
    }

    function loadWorkflow(workflow) {
      if (!graph.value || !workflow?.tasks) return
      graph.value.clear()
      const nodes = []
      const edges = []
      workflow.tasks.forEach((task, index) => {
        nodes.push({
          id: task.id,
          label: task.operator,
          x: 100 + (index % 3) * 200,
          y: 100 + Math.floor(index / 3) * 120,
          operator: task.operator,
          params: task.params || {},
          depends_on: task.depends_on || []
        })
        if (task.depends_on?.length > 0) {
          task.depends_on.forEach(sourceId => {
            edges.push({
              source: sourceId,
              target: task.id,
              type: 'polyline',
              style: {
                stroke: '#A3B1BF',
                lineWidth: 2,
                radius: 10,
                endArrow: { path: G6.Arrow.triangle(10, 12, 0), fill: '#A3B1BF', d: 0 }
              }
            })
          })
        }
      })
      graph.value.data({ nodes, edges })
      graph.value.render()
      graph.value.fitView(20)
    }

    function setupResizeObserver() {
      resizeObserver = new ResizeObserver(() => {
        if (graph.value && containerRef.value) {
          const w = containerRef.value.offsetWidth
          const h = containerRef.value.offsetHeight
          if (w > 0 && h > 0) graph.value.changeSize(w, h)
        }
      })
      if (containerRef.value) resizeObserver.observe(containerRef.value)
    }

    // Vue 的 setup + onMounted 等生命周期在 defineCustomElement 中也可用
    // 但为了兼容性，通过 return 中的 ref 处理
    const onMountedCb = () => {
      setTimeout(() => {
        initGraph()
        const data = getParsedData()
        if (data?.tasks?.length > 0) loadWorkflow(data)
        setupResizeObserver()
      }, 200)
    }

    watch(() => props.dagData, (newVal) => {
      if (newVal && graph.value) {
        const data = getParsedData()
        if (data?.tasks?.length > 0) loadWorkflow(data)
      }
    })

    return () => h('div', {
      ref: containerRef,
      style: {
        width: '100%',
        height: `${Number(props.height) || 400}px`,
        background: 'var(--addp-bg-primary, #1a1a2e)',
        borderRadius: '4px',
        overflow: 'hidden',
        position: 'relative'
      },
      // 在元素挂载后初始化图
      onVnodeMounted: onMountedCb
    })
  }
}

/**
 * 将内部组件注册为 Custom Element
 * 在 amis 中通过 <addp-dag-viewer dag-data="..." height="400"> 使用
 */
const DAGViewerElement = defineCustomElement(DAGViewerWCInner)

export function registerDagViewerElement() {
  if (!customElements.get('addp-dag-viewer')) {
    customElements.define('addp-dag-viewer', DAGViewerElement)
  }
}
