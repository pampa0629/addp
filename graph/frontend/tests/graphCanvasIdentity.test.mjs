// @vitest-environment jsdom
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
import GraphCanvas from '../src/components/GraphCanvas.vue'
import GraphResultView from '../../../common-frontend/graph/src/GraphResultView.vue'

const { instances } = vi.hoisted(() => ({ instances: [] }))
vi.mock('@antv/g6', () => ({ default: {
  registerEdge: vi.fn(),
  Arrow: { triangle: vi.fn() },
  Graph: class {
    constructor() { this.handlers = {}; this.states = []; instances.push(this) }
    on(name, handler) { this.handlers[name] = handler }
    data(data) {
      this.model = data
      this.items = new Map([...data.nodes, ...data.edges].map(model => [model.id, {
        getModel: () => model,
        getContainer: () => ({ find: () => null, show() {}, hide() {} }),
        getKeyShape: () => ({ attr: vi.fn() }),
        hasState: () => false,
      }]))
    }
    render() {}
    getNodes() { return this.model.nodes.map(node => this.items.get(node.id)) }
    getEdges() { return this.model.edges.map(edge => this.items.get(edge.id)) }
    findById(id) { return this.items.get(id) }
    clearItemStates() {}
    setItemState(id, state) { this.states.push([id, state]) }
    getZoom() { return 1 }
    focusItem(item) { this.focused = item.getModel().id }
    updateItem(item) { this.updated = item.getModel().id }
    destroy() {}
  },
} }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
vi.mock('@addp/common-frontend/graph', async () => ({
  ...await vi.importActual('../../../common-frontend/graph/src/graphIdentity.js'),
  getContrastingTextColor: () => '#fff',
  readGraphTheme: () => ({ categoryColors: ['#123456'], analysisLow: '#123456', analysisHigh: '#abcdef' }),
}))

afterEach(() => { instances.length = 0; vi.unstubAllGlobals() })

it('keeps overlapping native node and relationship IDs distinct through rendering, selection and click events', async () => {
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
  const nodes = [{ id: '0' }, { id: '1' }, { id: 'edge:0' }]
  const edges = [{ id: '0', source: '0', target: '1', type: 'Link' }]
  const wrapper = mount(GraphCanvas, { props: { nodes, edges }, global: { directives: { loading: {} } } })
  await nextTick()
  const graph = instances[0]
  const [firstNode, secondNode] = graph.model.nodes
  const [edge] = graph.model.edges
  expect(new Set([...graph.model.nodes, ...graph.model.edges].map(item => item.id)).size).toBe(4)
  expect([edge.source, edge.target]).toEqual([firstNode.id, secondNode.id])
  await wrapper.setProps({ selectedNodeId: '0', pathEdgeIds: ['0'] })
  expect(graph.states).toContainEqual([firstNode.id, 'selected'])
  expect(graph.states).toContainEqual([edge.id, 'path'])
  graph.handlers['node:click']({ item: graph.items.get(firstNode.id) })
  graph.handlers['edge:click']({ item: graph.items.get(edge.id) })
  expect(wrapper.emitted('node-click')).toEqual([['0']])
  expect(wrapper.emitted('node-select')[0][0].id).toBe('0')
  expect(wrapper.emitted('edge-select')[0][0].id).toBe('0')
  await wrapper.setProps({ selectedNodeId: '', selectedEdgeId: '0' })
  expect(graph.states).toContainEqual([edge.id, 'selected'])
  wrapper.vm.focusNodes(['1'])
  expect(graph.focused).toBe(secondNode.id)
  wrapper.vm.applyScoreColors([{ node_id: '0', score: 1 }])
  expect(graph.updated).toBe(firstNode.id)
  wrapper.unmount()
})


it('applies the same native ID isolation to shared query graph results', async () => {
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
  const nativeNode = { element_id: '0', labels: ['Definition'], properties: { name: 'entry' } }
  const wrapper = mount(GraphResultView, { props: { graphData: {
    nodes: [nativeNode, { element_id: '1', labels: ['Definition'], properties: {} }],
    relationships: [{ element_id: '0', type: 'Link', start_node_id: '0', end_node_id: '1' }],
  } }, global: { stubs: { ElTag: true } } })
  await nextTick()
  const graph = instances[0]
  const [node, target] = graph.model.nodes
  const [edge] = graph.model.edges
  expect(new Set([node.id, target.id, edge.id]).size).toBe(3)
  expect([edge.source, edge.target]).toEqual([node.id, target.id])
  graph.handlers['node:click']({ item: graph.items.get(node.id) })
  await nextTick()
  expect(wrapper.text()).toContain('entry')
  expect(graph.items.get(node.id).getModel()._meta).toEqual(nativeNode)
  wrapper.unmount()
})
