import assert from 'node:assert/strict'
import { pathToFileURL } from 'node:url'
import { resolve } from 'node:path'

const mod = await import(pathToFileURL(resolve('../../common-frontend/dag/src/utils/directEdge.js')))
const connections = await import(pathToFileURL(resolve('../../common-frontend/dag/src/utils/connections.js')))

const outputEvent = {
  item: { getID: () => 'source' },
  x: 10,
  y: 20,
  shape: { cfg: { portType: 'output', portName: 'default' } }
}
const inputEvent = {
  item: { getID: () => 'target' },
  x: 30,
  y: 40,
  shape: { cfg: { portType: 'input', portName: 'input_df' } }
}
const emittedEvents = []
let edgeModel = null
let edgeCapture = true
const edge = {
  getKeyShape: () => ({ set: (_name, value) => { edgeCapture = value } })
}
const graph = {
  findById: id => ({ id }),
  addItem: (_type, model) => {
    edgeModel = { ...model }
    return edge
  },
  updateItem: (_item, model) => { edgeModel = { ...edgeModel, ...model } },
  removeItem: () => { edgeModel = null },
  emit: (name, payload) => emittedEvents.push({ name, payload })
}

const behaviorDefinition = mod.createDAGDirectEdgeBehavior({
  resolveSource: event => event.shape?.cfg?.portType === 'output'
    ? { name: event.shape.cfg.portName }
    : null,
  resolveTarget: event => event.shape?.cfg?.portType === 'input'
    ? { name: event.shape.cfg.portName }
    : null,
  canConnect: ({ sourceId, targetId }) => sourceId !== targetId,
  buildEdgeConfig: ({ sourcePort, targetPort }) => ({
    sourcePort: sourcePort.name,
    targetParam: targetPort?.name
  })
})
const behavior = { graph }

assert.deepEqual(behaviorDefinition.getEvents(), {
  'node:mousedown': 'onPortMouseDown',
  mousemove: 'onPortMouseMove',
  'node:mouseup': 'onPortMouseUp',
  mouseup: 'onPortMouseUp',
  mouseleave: 'onPortMouseLeave'
})
behaviorDefinition.onPortMouseDown.call(behavior, outputEvent)
assert.equal(edgeCapture, false)
assert.deepEqual(edgeModel, {
  source: 'source',
  target: { x: 10, y: 20 },
  sourcePort: 'default',
  targetParam: undefined
})
behaviorDefinition.onPortMouseMove.call(behavior, { x: 25, y: 35 })
assert.deepEqual(edgeModel.target, { x: 25, y: 35 })
behaviorDefinition.onPortMouseUp.call(behavior, inputEvent)
assert.equal(edgeCapture, true)
assert.equal(edgeModel.target, 'target')
assert.equal(edgeModel.sourcePort, 'default')
assert.equal(edgeModel.targetParam, 'input_df')
assert.deepEqual(emittedEvents.map(event => event.name), ['beforecreateedge', 'aftercreateedge'])
assert.equal(mod.isDAGPortEvent(outputEvent), true)
assert.equal(mod.isDAGPortEvent({ shape: { cfg: { name: 'node-body' } } }), false)

const connectionGraph = {
  getEdges: () => [{ getModel: () => ({ source: 'source', target: 'existing' }) }]
}
const noLoop = () => false
assert.equal(mod.validateDAGConnection({
  graph: connectionGraph,
  sourceId: 'source',
  targetId: 'target',
  hasLoop: noLoop
}), true)
assert.equal(mod.validateDAGConnection({
  graph: connectionGraph,
  sourceId: 'source',
  targetId: 'source',
  hasLoop: noLoop
}), 'loop')
assert.equal(mod.validateDAGConnection({
  graph: connectionGraph,
  sourceId: 'source',
  targetId: 'target',
  hasLoop: () => true
}), 'loop')
assert.equal(mod.validateDAGConnection({
  graph: connectionGraph,
  sourceId: 'source',
  targetId: 'existing',
  hasLoop: noLoop
}), 'duplicate')
assert.equal(mod.validateDAGConnection({
  graph: connectionGraph,
  sourceId: 'source',
  targetId: 'existing',
  hasLoop: noLoop,
  isDuplicate: model => model.source === 'source' && model.target === 'other'
}), true)

const graphNodes = [
  { getModel: () => ({ id: 'source', label: 'Source' }) },
  { getModel: () => ({ id: 'blocked', label: 'Blocked' }) },
  { getModel: () => ({ id: 'target', label: 'Target' }) }
]
const candidateGraph = {
  getNodes: () => graphNodes,
  getEdges: () => [{ getModel: () => ({ source: 'source', target: 'target' }) }]
}
assert.deepEqual(connections.getDAGIncomingEdgeModels(candidateGraph, 'target'), [
  { source: 'source', target: 'target' }
])
assert.deepEqual(
  connections.getDAGUpstreamCandidates({
    graph: candidateGraph,
    targetId: 'target',
    hasLoop: sourceId => sourceId === 'blocked'
  }).map(candidate => ({
    id: candidate.node.id,
    connected: candidate.connected,
    disabled: candidate.disabled
  })),
  [
    { id: 'source', connected: true, disabled: false },
    { id: 'blocked', connected: false, disabled: true }
  ]
)

console.log('commonDAGDirectEdge tests passed')
