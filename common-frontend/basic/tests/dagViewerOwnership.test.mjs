import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

test('DAG web component adapts attributes to the unique shared renderer and stylesheet', () => {
  const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
  const adapter = read('../../dag/src/web-components/dag-viewer-wc.js')
  const viewer = read('../../dag/src/components/DAGViewer.vue')
  const node = read('../../dag/src/nodes/MultiPortNode.js')
  assert.match(adapter, /import DAGViewer from/)
  assert.match(adapter, /h\(DAGViewer,/)
  assert.match(adapter, /dag-viewer\.css\?inline/)
  assert.match(viewer, /style src="\.\/dag-viewer\.css"/)
  assert.doesNotMatch(adapter, /G6|ResizeObserver|MutationObserver|setTimeout/)
  assert.doesNotMatch(viewer + node, /#[\da-f]{3,8}\b/i)
})
