// @vitest-environment jsdom
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
import G6 from '@antv/g6'
import GraphCanvas from '../src/components/GraphCanvas.vue'
import GraphResultView from '../../../common-frontend/graph/src/GraphResultView.vue'

const { instances, edgeTypes } = vi.hoisted(() => ({ instances: [], edgeTypes: new Map() }))
vi.mock('@antv/g6', async () => ({ default: {
  Shape: (await import('@antv/g6-core/lib/element/shape.js')).default,
  _nativeEdges: await import('@antv/g6-core/lib/element/edge.js'),
  Util: { processParallelEdges: (await import('@antv/g6-core/lib/util/base.js')).processParallelEdges },
  registerEdge: vi.fn((name, definition, parent) => edgeTypes.set(name, { definition, parent })),
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
    fitView = vi.fn()
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


for (const count of [1, 3]) {
  it(`waits for final layout before locating ${count} search results`, async () => {
    vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
    const nodes = ['0', '1', '2'].map(id => ({ id }))
    const wrapper = mount(GraphCanvas, { props: { nodes, edges: [] }, global: { directives: { loading: {} } } })
    await nextTick()
    const graph = instances[0]
    graph.handlers.beforelayout()
    wrapper.vm.focusNodes(nodes.slice(0, count).map(node => node.id))
    // Existing nodes may still have temporary positions while force layout is running.
    expect(graph.focused).toBeUndefined()
    expect(graph.fitView).not.toHaveBeenCalled()
    graph.handlers.afterlayout()
    if (count === 1) {
      expect(graph.focused).toBe(graph.model.nodes[0].id)
      expect(graph.fitView).not.toHaveBeenCalled()
    } else {
      expect(graph.focused).toBeUndefined()
      expect(graph.fitView).toHaveBeenCalledExactlyOnceWith(undefined, { onlyOutOfViewPort: true })
    }
    wrapper.unmount()
  })
}

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


it('keeps reverse and parallel relationships distinct and selects native loop geometry', async () => {
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} })
  const nodes = [{ id: 'a' }, { id: 'b' }]
  const edges = [{ id: 'ab', source: 'a', target: 'b' }, { id: 'ba', source: 'b', target: 'a' },
    { id: 'parallel', source: 'a', target: 'b' }, { id: 'self', source: 'a', target: 'a' }]
  const wrapper = mount(GraphCanvas, { props: { nodes, edges }, global: { directives: { loading: {} } } })
  await nextTick()
  const graph = instances[0]
  const [first, second, third, loop] = graph.model.edges
  {
    const offsets = [first, second, third].map(edge => edge.source === first.source ? edge.curveOffset : -edge.curveOffset)
    expect(new Set(offsets).size).toBe(3)
  }
  expect(edgeTypes.get(loop.type)?.parent).toBe('loop')
  expect(loop.loopCfg.dist).toBeGreaterThan(0)
  wrapper.unmount()
})

it('preserves inherited quadratic and cubic paths and uses their final tangent for direction', () => {
  for (const [type, path] of [['quadratic', [['M', 0, 0], ['Q', 20, -30, 100, 0]]],
    ['loop', [['M', -10, -20], ['C', -80, -80, 80, -80, 10, -20]]]]) {
    const registered = [...edgeTypes.values()].find(entry => entry.parent === type)
    expect(registered).toBeDefined()
    const created = new Map()
    const group = { find: predicate => [...created.values()].find(predicate), addShape: (shape, config) => {
      const element = { get: key => key === 'name' ? config.name : undefined, attrs: config.attrs }
      created.set(config.name, element); return element
    } }
    const points = path[0].slice(1)
    const cfg = { startPoint: { x: points[0], y: points[1] }, endPoint: { x: points[0], y: points[1] }, style: {}, _meta: { directed: true } }
    registered.definition.afterDraw.call({ getPathPoints: value => value, getControlPoints: () => [], getPath: () => path }, cfg, group)
    expect(created.get('semantic-edge-line')?.attrs.path).toEqual(path)
    expect(created.get('direction-marker')?.attrs.path[0]).toEqual(['M', ...path.at(-1).slice(-2)])
  }
})


it('recomputes loop anchors from the current node instead of retaining invalid layout anchors', () => {
  const registered = [...edgeTypes.values()].find(entry => entry.parent === 'loop')
  const sourceNode = {
    getModel: () => ({ x: 100, y: 200 }),
    get: () => ({ getMatrix: () => [1, 0, 0, 0, 1, 0, 100, 200, 1] }),
    getKeyShape: () => ({ getBBox: () => ({ minX: -22, minY: -22, maxX: 22, maxY: 22, width: 44, height: 44 }), get: () => 'circle' })
  }
  const inherited = { ...G6.Shape.getFactory('edge').getShape(registered.parent), ...registered.definition }
  const points = inherited.getPathPoints({ sourceNode, source: 'a', target: 'a', startPoint: { x: NaN, y: NaN },
    endPoint: { x: NaN, y: NaN }, controlPoints: [{ x: NaN, y: NaN }], loopCfg: { position: 'top', dist: 50 } })
  expect([points.startPoint, ...points.controlPoints, points.endPoint].every(point => Number.isFinite(point.x) && Number.isFinite(point.y))).toBe(true)
  expect(points.startPoint).not.toEqual(points.endPoint)
})
