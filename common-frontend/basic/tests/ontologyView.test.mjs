import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'

const source = readFileSync(new URL('../../graph/src/OntologyView.vue', import.meta.url), 'utf8')
function fixture(layout = 'dagre') {
  const callbacks = {}, instances = []
  const props = { layout, readonly: true, entityTypes: [{ id:'source', name:'source', label:'这是一个需要完整显示且不能在图节点内溢出的概念名称' }], relationTypes:[] }
  const ref = value => ({ value })
  const canvas = { offsetWidth:1000, offsetHeight:480 }
  class Graph {
    constructor(options) { this.options = options; this.events = {}; this.zoom = 1; instances.push(this) }
    on(event, callback) { this.events[event] = callback }
    once(event, callback) { this.events[event] = callback }
    get(key) { return this.options[key] }
    getZoom() { return this.zoom }
    zoomTo(zoom) { this.zoom = zoom }
    fitCenter() { this.centered = true }
    fitView(padding) { this.padding = padding }
    changeSize(width, height) { Object.assign(this.options, { width, height }) }
    updateLayout(options) { this.options.layout = options; this.events.afterlayout?.() }
    data(data) { this.dataValue = data }
    render() {}
    destroy() { this.destroyed = true }
  }
  const script = source.match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*\n/gm, '')
  const context = {
    ref, computed:fn => ({ get value() { return fn() } }), defineProps:() => props, defineEmits:() => () => {},
    useI18n:() => ({ t:key => key }), G6:{ Graph, Arrow:{ triangle:() => '' } },
    getComputedStyle:() => ({ getPropertyValue:() => 'theme-color' }),
    onMounted:fn => { callbacks.mount = fn }, onBeforeUnmount:fn => { callbacks.unmount = fn },
    watch:() => {}, nextTick:fn => fn ? Promise.resolve().then(fn) : Promise.resolve(),
    ResizeObserver:class { constructor(fn) { callbacks.resize = fn } observe() {} disconnect() { callbacks.disconnected = true } }
  }
  runInNewContext(`${script}\nthis.view = { containerRef, canvasRef, initGraph, actualSize, zoomBy, fitView, wrapLabel }`, context)
  context.view.containerRef.value = canvas
  context.view.canvasRef.value = canvas
  return { ...context.view, callbacks, instances, canvas, props }
}

test('shared ontology view retains layered default, uses actual canvas size and bounds manual zoom', async () => {
  const state = fixture()
  await state.callbacks.mount()
  const graph = state.instances[0]
  assert.equal(graph.options.layout.type, 'dagre')
  assert.equal(graph.options.height, 480)
  assert.equal(graph.options.container, state.canvas)
  assert.equal(graph.options.modes.default.includes('drag-node'), false)
  state.zoomBy(100)
  assert.equal(graph.zoom, 2)
  state.zoomBy(0.0001)
  assert.equal(graph.zoom, 0.02)
  state.actualSize()
  assert.equal(graph.zoom, 1)
  assert.equal(graph.centered, true)
  state.callbacks.unmount()
  assert.equal(graph.destroyed, true)
  assert.equal(state.callbacks.disconnected, true)
})

test('compact ontology layout has explicit spacing and refits only after resized layout completes', async () => {
  const state = fixture('grid')
  await state.callbacks.mount()
  const graph = state.instances[0]
  assert.equal(graph.options.layout.nodeSpacing, 24)
  assert.equal(graph.dataValue.nodes[0].labelCfg.style.fontSize, 14)
  assert.match(graph.dataValue.nodes[0].label, /\n/)
  assert.equal(graph.dataValue.nodes[0]._meta.label, state.props.entityTypes[0].label)
  Object.assign(state.canvas, { offsetWidth:600, offsetHeight:400 })
  state.callbacks.resize()
  assert.equal(graph.options.layout.width, 536)
  assert.equal(graph.padding, 32)
})

test('hidden ontology canvas waits for its first measurable size', async () => {
  const state = fixture()
  state.canvas.offsetWidth = 0
  await state.callbacks.mount()
  assert.equal(state.instances.length, 0)
  state.canvas.offsetWidth = 1000
  state.callbacks.resize()
  assert.equal(state.instances.length, 1)
})

test('ontology consumers use the unique shared renderer', () => {
  for (const file of ['ontology/frontend/src/views/PlatformDefinitions.vue', 'graph/frontend/src/views/OntologyDetail.vue']) {
    const page = readFileSync(new URL(`../../../${file}`, import.meta.url), 'utf8')
    assert.match(page, /<OntologyView\b/)
    assert.doesNotMatch(page, /new G6\.Graph/)
  }
})
