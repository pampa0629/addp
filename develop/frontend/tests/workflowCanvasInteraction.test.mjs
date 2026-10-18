import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { runInNewContext } from 'node:vm'

const source = await readFile(resolve('src/components/workflow/WorkflowDAGCanvas.vue'), 'utf8')
const nodeSource = await readFile(resolve('src/components/workflow/workflowEditorNode.js'), 'utf8')
const edgeSource = await readFile(resolve('src/components/workflow/workflowEditorEdge.js'), 'utf8')

assert.match(source, /graph\.value\.on\('node:click', handleNodeClick\)/)
assert.doesNotMatch(source, /node:dblclick/)
assert.match(source, /createDAGDirectEdgeBehavior\(\{/)
assert.match(source, /registerWorkflowEditorEdge\(\(\) => \(\{[\s\S]*?stroke: cssColor\('--addp-bg-secondary'\)[\s\S]*?lineWidth: 6/)
assert.match(source, /type: WORKFLOW_EDGE_TYPE/)
assert.match(source, /stroke: cssColor\('--addp-bg-secondary'\)/)
assert.match(source, /type: 'click-select',\s*selectEdge: true/)
assert.match(source, /resolveSource: resolveOutputPort/)
assert.match(source, /resolveTarget: resolveInputPort/)
assert.match(source, /targetParam: targetPort\?\.name/)
assert.match(source, /useDAGViewport\(graph\)/)
assert.match(source, /:disabled="!canZoomOut"/)
assert.match(source, /:disabled="!canZoomIn"/)
assert.match(source, /useDAGHistory\(\{/)
assert.match(source, /useDAGClipboard\(graph/)
assert.match(source, /useDAGLayout\(graph\)/)
assert.match(source, /createDAGKeyboardHandler\(\{/)
assert.match(source, /useDAGSelection\(graph, \{\s*focusTarget: container/)
assert.match(source, /class="addp-dag-focus-region"/)
assert.match(source, /role="region"/)
assert.match(source, /canvasAriaLabel/)
assert.match(source, /aria-keyshortcuts="ArrowLeft ArrowRight ArrowUp ArrowDown Enter Delete Escape"/)
assert.match(source, /selectPreviousNode/)
assert.match(source, /selectNextNode/)
assert.match(source, /activateSelection/)
assert.match(source, /navigationAnnouncement/)
assert.match(source, /getDAGIncomingEdgeModels/)
assert.match(source, /getDAGUpstreamCandidates/)
assert.match(source, /function updateInputConnection\(/)
assert.match(source, /function findWorkflowBindings\(/)
assert.match(source, /bindings\.forEach\(binding/)
assert.match(source, /isDuplicate: model/)
assert.match(source, /updateInputConnection,/)
assert.match(source, /inputConnections: inputConnectionsForNode\(model\.id\)/)
assert.match(source, /inputConnectionOptions: inputConnectionOptionsForNode\(model\)/)
assert.match(source, /emit\('update:layout', captureLayout\(\)\)/)
assert.match(source, /lineAppendWidth: 10/)
assert.match(source, /edgeStateStyles: \{[\s\S]*?hover: \{[\s\S]*?selected: \{/)
assert.match(source, /graph\.value\.on\('edge:mouseenter', handleEdgeMouseEnter\)/)
assert.match(source, /graph\.value\.on\('edge:mouseleave', handleEdgeMouseLeave\)/)
assert.match(source, /graph\.value\.on\('edge:click', handleEdgeClick\)/)
assert.match(source, /function handleEdgeMouseEnter\(event\) \{[\s\S]*?toFront\(\)[\s\S]*?setItemState\?\.\(event\.item, 'hover', true\)/)
assert.match(source, /function handleEdgeMouseLeave\(event\) \{[\s\S]*?setItemState\?\.\(event\.item, 'hover', false\)[\s\S]*?hasState\?\.\('selected'\)[\s\S]*?toBack\?\.\(\)/)
const viewportHandlers = source.slice(
  source.indexOf('function handleZoomIn()'),
  source.indexOf('function handleAutoLayout()')
)
const autoLayoutHandler = source.match(/function handleAutoLayout\(\) \{[\s\S]*?\n\}/)?.[0] || ''
assert.doesNotMatch(viewportHandlers, /emitLayout\(\)/)
assert.match(autoLayoutHandler, /emitLayout\(\)/)
assert.doesNotMatch(source, /useDAGEdgeMode|isAddEdgeMode|toggleAddEdgeMode/)
assert.doesNotMatch(source, /function handleKeydown\(event\)/)
assert.doesNotMatch(nodeSource, /workflow-node-operator/)
assert.doesNotMatch(nodeSource, /showOperatorName/)
assert.match(edgeSource, /G6\.registerEdge\(WORKFLOW_EDGE_TYPE/)
assert.match(edgeSource, /}, 'polyline'\)/)
assert.match(edgeSource, /name: WORKFLOW_EDGE_CASING/)
assert.match(edgeSource, /capture: false/)
assert.match(edgeSource, /casing\.attr\(\{[\s\S]*?path: keyShape\.attr\('path'\)/)

console.log('workflowCanvasInteraction tests passed')

// Refreshing colors must not move same-named input/output labels to each other's row.
{
  let definition
  const context = {
    G6: { registerNode: (_name, node) => { definition = node } },
    document: { documentElement: {} },
    getComputedStyle: () => ({ getPropertyValue: name => name }),
    isWorkflowInputParameter: () => true
  }
  runInNewContext(nodeSource.replace(/^import .*\n/gm, '').replace(/^export /gm, '') + '\nregisterWorkflowEditorNode()', context)
  const shapes = []
  const group = {
    addShape(type, { name, attrs }) {
      const shape = { type, name, attrs: { ...attrs }, get: key => key === 'name' ? name : undefined,
        attr(value) { if (typeof value === 'string') return this.attrs[value]; Object.assign(this.attrs, value) } }
      shapes.push(shape)
      return shape
    },
    find: predicate => shapes.find(predicate)
  }
  const model = { label: 'Transform', inputPorts: [{ name: 'first' }, { name: 'shared' }], outputPorts: [{ name: 'shared' }] }
  definition.draw(model, group)
  const labels = shapes.filter(shape => shape.type === 'text' && shape.attrs.text === 'shared')
  assert.equal(labels.length, 2)
  const originalPositions = labels.map(shape => ({ x: shape.attrs.x, y: shape.attrs.y }))
  assert.notEqual(originalPositions[0].y, originalPositions[1].y)
  definition.update(model, { getContainer: () => group, getModel: () => model })
  assert.deepEqual(labels.map(shape => ({ x: shape.attrs.x, y: shape.attrs.y })), originalPositions)
  assert.ok(labels.every(shape => shape.attrs.fill === '--addp-text-secondary'))
}
