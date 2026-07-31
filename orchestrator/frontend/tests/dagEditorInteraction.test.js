import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import test from 'node:test'

test('DAG editor uses shared direct connection and viewport controls', async () => {
  const source = await readFile(resolve('src/components/DAGEditor.vue'), 'utf8')

  assert.match(source, /createDAGDirectEdgeBehavior\(\{/)
  assert.match(source, /validateDAGConnection\(\{/)
  assert.match(source, /resolveSource: event => linkPointPort\(event, 'right'\)/)
  assert.match(source, /resolveTarget: event => linkPointPort\(event, 'left'\)/)
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
  assert.match(source, /v-model="currentDependencyIds"/)
  assert.match(source, /multiple/)
  assert.match(source, /@change="applyCurrentDependencies"/)
  assert.match(source, /size="min\(520px, calc\(100vw - 12px\)\)"/)
  assert.match(source, /function applyCurrentDependencies\(sourceIds\)/)
  assert.match(source, /function addTask\(nodeData, point = null\)/)
  assert.match(source, /addTask\(nodeData, point\)/)
  assert.match(source, /defineExpose\(\{\s*addTask,/)
  assert.match(source, /function applyCurrentNodeDraft\(parameters/)
  assert.match(source, /recordHistory\(\{ mergeKey: `node-draft:\$\{currentNode\.value\.id\}` \}\)/)
  assert.match(source, /watch\(structuredParameters/)
  assert.match(source, /watch\(parametersStr/)
  assert.match(source, /emit\('update:layout', captureLayout\(\)\)/)
  const viewportHandlers = source.slice(
    source.indexOf('function handleZoomIn()'),
    source.indexOf('function handleAutoLayout()')
  )
  const autoLayoutHandler = source.match(/function handleAutoLayout\(\) \{[\s\S]*?\n\}/)?.[0] || ''
  assert.doesNotMatch(viewportHandlers, /emitLayout\(\)/)
  assert.match(autoLayoutHandler, /emitLayout\(\)/)
  assert.match(source, /canBuildOwnerTaskUrl\(rawUrl, currentNode\.value, currentOwnerGraphId\.value\)/)
  const handleDropSource = source.match(/function handleDrop\(event\) \{[\s\S]*?\n\}/)?.[0] || ''
  assert.doesNotMatch(handleDropSource, /graph\.value\.addItem/)
  assert.doesNotMatch(source, /saveNodeConfig|saveConfigBtn|configSaved/)
  assert.doesNotMatch(source, /useDAGEdgeMode|isAddEdgeMode|toggleAddEdgeMode/)
  assert.doesNotMatch(source, /function handleKeydown\(event\)/)
  assert.doesNotMatch(source, /const duplicated = graph\.value\.getEdges\(\)/)
})

test('orchestration form announces asynchronous save status', async () => {
  const source = await readFile(resolve('src/views/OrchestrationForm.vue'), 'utf8')
  const announcer = await readFile(resolve('../../common-frontend/basic/src/components/StatusAnnouncer.vue'), 'utf8')

  assert.match(source, /class="orchestration-form" :aria-busy="saving"/)
  assert.match(source, /<StatusAnnouncer :label="t\('orchestrator\.orchestrationForm\.statusLabel'\)" :message="formAnnouncement"/)
  assert.match(source, /orchestrator\.orchestrationForm\.savingStatus/)
  assert.match(announcer, /role="status"/)
  assert.match(announcer, /aria-live="polite"/)
})

test('orchestration task panel splitter supports keyboard resizing', async () => {
  const source = await readFile(resolve('src/views/OrchestrationForm.vue'), 'utf8')
  const resizable = await readFile(resolve('../../common-frontend/basic/src/composables/useResizable.js'), 'utf8')

  assert.match(source, /role="separator"/)
  assert.match(source, /aria-orientation="vertical"/)
  assert.match(source, /aria-controls="task-library-panel"/)
  assert.match(source, /:aria-valuenow="Math\.round\(leftPanelWidth\)"/)
  assert.match(source, /@keydown="handleLeftPanelResizeKeydown"/)
  assert.match(source, /\.panel-splitter:focus-visible/)
  assert.match(resizable, /const handleResizeKeydown = \(event, step = 16\)/)
  assert.match(resizable, /key === 'Home'/)
  assert.match(resizable, /key === 'End'/)
  assert.match(resizable, /key === 'ArrowLeft'/)
  assert.match(resizable, /key === 'ArrowRight'/)
})
