import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'vitest'

test('探索页使用聚合概览和统一展开契约', () => {
  const browserSource = readFileSync(
    new URL('../src/views/GraphBrowser.vue', import.meta.url),
    'utf8',
  )
  const apiSource = readFileSync(
    new URL('../src/api/browse.js', import.meta.url),
    'utf8',
  )

  assert.match(apiSource, /getBrowseSnapshot\(graphId/)
  assert.doesNotMatch(apiSource, /getOverview\(/)
  assert.doesNotMatch(apiSource, /getSchema\(/)
  assert.doesNotMatch(apiSource, /getStats\(/)
  assert.match(apiSource, /expandTarget\(graphId, target, depth/)
  assert.match(apiSource, /node_limit:\s*nodeLimit/)
  assert.match(apiSource, /relationship_limit:\s*relationshipLimit/)
  assert.match(browserSource, /browseAPI\.getBrowseSnapshot\(graphId\.value/)
  assert.match(browserSource, /node\.kind === 'aggregate'/)
  assert.match(browserSource, /replaceSubgraph\(snapshot\?\.overview\)/)
  assert.match(browserSource, /createLatestOperationController\(\)/)
  assert.match(browserSource, /t\('graph\.browser\.totalCount'\)/)
  assert.match(browserSource, /t\('graph\.browser\.overviewCount'/)
})

test('画布使用局部增量布局和语义缩放', () => {
  const canvasSource = readFileSync(
    new URL('../src/components/GraphCanvas.vue', import.meta.url),
    'utf8',
  )
  const layoutPolicySource = readFileSync(
    new URL('../src/utils/graphLayoutPolicy.js', import.meta.url),
    'utf8',
  )

  assert.match(canvasSource, /function applyIncrementalPositions/)
  assert.match(canvasSource, /props\.expansionAnchorId/)
  assert.match(layoutPolicySource, /relayoutAtChangeData:\s*false/)
  assert.match(canvasSource, /graphInstance\.on\('viewportchange'/)
  assert.match(canvasSource, /function syncSemanticZoom/)
  assert.match(canvasSource, /edge\.getContainer\(\).*hide/s)
  assert.match(canvasSource, /minZoom:\s*0\.01/)
})

test('跳数切换只对选中的实体节点立即生效', () => {
  const browserSource = readFileSync(
    new URL('../src/views/GraphBrowser.vue', import.meta.url),
    'utf8',
  )
  const expandHandlerSource = browserSource.slice(
    browserSource.indexOf('function handleExpand(targetValue)'),
    browserSource.indexOf('function handleExpandDepthChange()'),
  )

  assert.match(browserSource, /:disabled="!canExpandSelectedNode"/)
  assert.match(browserSource, /@change="handleExpandDepthChange"/)
  assert.match(browserSource, /function handleExpandDepthChange\(\)/)
  assert.match(browserSource, /handleExpand\(selectedNodeId\.value\)/)
  assert.match(expandHandlerSource, /replaceSubgraph\(result\)/)
  assert.doesNotMatch(expandHandlerSource, /mergeSubgraph\(result\)/)
  assert.match(expandHandlerSource, /await nextTick\(\)/)
  assert.match(expandHandlerSource, /canvasRef\.value\?\.fitView\(\)/)
})

test('选中节点只做强调，不压暗多跳子图', () => {
  const canvasSource = readFileSync(
    new URL('../src/components/GraphCanvas.vue', import.meta.url),
    'utf8',
  )
  const interactionStateSource = canvasSource.slice(
    canvasSource.indexOf('function syncInteractionStates'),
    canvasSource.indexOf('function syncSemanticZoom'),
  )

  assert.match(interactionStateSource, /setItemState\(props\.selectedNodeId, 'selected'/)
  assert.doesNotMatch(interactionStateSource, /'dimmed'/)
})

test('手工切换布局时暂停复杂渲染并合并快速切换', () => {
  const canvasSource = readFileSync(
    new URL('../src/components/GraphCanvas.vue', import.meta.url),
    'utf8',
  )

  assert.match(canvasSource, /graphInstance\.on\('beforelayout'/)
  assert.match(canvasSource, /function suspendSemanticRendering/)
  assert.match(canvasSource, /requestAnimationFrame\(runScheduledLayout\)/)
  assert.match(canvasSource, /layoutCompletionReason === 'layout'/)
})
