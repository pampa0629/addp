import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const source = await readFile(resolve('src/components/workflow/WorkflowDAGCanvas.vue'), 'utf8')
const nodeSource = await readFile(resolve('src/components/workflow/workflowEditorNode.js'), 'utf8')

assert.match(source, /graph\.value\.on\('node:click', handleNodeClick\)/)
assert.doesNotMatch(source, /node:dblclick/)
assert.match(source, /createDAGDirectEdgeBehavior\(\{/)
assert.match(source, /resolveSource: resolveOutputPort/)
assert.match(source, /resolveTarget: resolveInputPort/)
assert.match(source, /targetParam: targetPort\?\.name/)
assert.match(source, /useDAGViewport\(graph\)/)
assert.match(source, /:disabled="!canZoomOut"/)
assert.match(source, /:disabled="!canZoomIn"/)
assert.match(source, /useDAGHistory\(\{/)
assert.match(source, /useDAGClipboard\(graph/)
assert.match(source, /useDAGLayout\(graph\)/)
assert.match(source, /emit\('update:layout', captureLayout\(\)\)/)
assert.doesNotMatch(source, /useDAGEdgeMode|isAddEdgeMode|toggleAddEdgeMode/)
assert.match(nodeSource, /hasDistinctText\(operatorName, title\)/)
assert.match(nodeSource, /if \(showOperatorName\)/)

console.log('workflowCanvasInteraction tests passed')
