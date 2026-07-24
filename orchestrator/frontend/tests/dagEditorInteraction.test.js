import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import test from 'node:test'

test('DAG editor uses shared direct connection and viewport controls', async () => {
  const source = await readFile(resolve('src/components/DAGEditor.vue'), 'utf8')

  assert.match(source, /createDAGDirectEdgeBehavior\(\{/)
  assert.match(source, /resolveSource: event => linkPointPort\(event, 'right'\)/)
  assert.match(source, /resolveTarget: event => linkPointPort\(event, 'left'\)/)
  assert.match(source, /useDAGViewport\(graph\)/)
  assert.match(source, /:disabled="!canZoomOut"/)
  assert.match(source, /:disabled="!canZoomIn"/)
  assert.match(source, /useDAGHistory\(\{/)
  assert.match(source, /useDAGClipboard\(graph/)
  assert.match(source, /useDAGLayout\(graph\)/)
  assert.match(source, /function addTask\(nodeData, point = null\)/)
  assert.match(source, /addTask\(nodeData, point\)/)
  assert.match(source, /defineExpose\(\{\s*addTask,/)
  assert.match(source, /function applyCurrentNodeDraft\(parameters/)
  assert.match(source, /recordHistory\(\{ mergeKey: `node-draft:\$\{currentNode\.value\.id\}` \}\)/)
  assert.match(source, /watch\(structuredParameters/)
  assert.match(source, /watch\(parametersStr/)
  assert.match(source, /emit\('update:layout', captureLayout\(\)\)/)
  const handleDropSource = source.match(/function handleDrop\(event\) \{[\s\S]*?\n\}/)?.[0] || ''
  assert.doesNotMatch(handleDropSource, /graph\.value\.addItem/)
  assert.doesNotMatch(source, /saveNodeConfig|saveConfigBtn|configSaved/)
  assert.doesNotMatch(source, /useDAGEdgeMode|isAddEdgeMode|toggleAddEdgeMode/)
})
