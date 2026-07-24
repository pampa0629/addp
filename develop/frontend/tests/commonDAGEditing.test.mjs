import assert from 'node:assert/strict'

import {
  applyDAGNodePositions,
  calculateDAGFitViewport,
  clampDAGZoom,
  cloneDAGValue,
  cloneDAGNodeForPaste,
  createDAGHistoryStore,
  normalizeDAGEditorLayout,
  restoreDAGViewport
} from '../../../common-frontend/dag/src/utils/editing.js'

assert.deepEqual(cloneDAGValue({ id: 'node', draw: () => 'ignored' }), { id: 'node' })
assert.equal(clampDAGZoom(0.1), 0.5)
assert.equal(clampDAGZoom(1.2), 1.2)
assert.equal(clampDAGZoom(4), 1.5)

assert.deepEqual(calculateDAGFitViewport({
  width: 800,
  height: 600,
  padding: 32,
  bbox: { x: 100, y: 100, width: 100, height: 50 }
}), {
  zoom: 1.5,
  center: { x: 400, y: 300 },
  translate: { x: 250, y: 175 }
})

assert.deepEqual(calculateDAGFitViewport({
  width: 400,
  height: 300,
  padding: [20, 30],
  bbox: { x: -100, y: -50, width: 2000, height: 1000 }
}), {
  zoom: 0.5,
  center: { x: 200, y: 150 },
  translate: { x: -700, y: -300 }
})

assert.equal(calculateDAGFitViewport({
  width: 400,
  height: 300,
  padding: 32,
  bbox: { x: 0, y: 0, width: 0, height: 100 }
}), null)

const history = createDAGHistoryStore({ mergeWindow: 400 })
history.reset({ value: '' })
history.record({ value: 'a' }, { mergeKey: 'params:node-1', now: 100 })
history.record({ value: 'ab' }, { mergeKey: 'params:node-1', now: 300 })
assert.equal(history.size(), 2)
assert.deepEqual(history.undo(), { value: '' })
assert.deepEqual(history.redo(), { value: 'ab' })

history.undo()
history.record({ value: 'new branch' }, { now: 800 })
assert.equal(history.canRedo(), false)

const copied = cloneDAGNodeForPaste({
  id: 'source',
  x: 10,
  y: 20,
  params: { distance: 100 }
}, { id: 'copy', offset: 24 })
assert.deepEqual(copied, {
  id: 'copy',
  x: 34,
  y: 44,
  params: { distance: 100 }
})
assert.equal(Object.hasOwn(copied, 'edges'), false)

const layout = normalizeDAGEditorLayout({
  nodes: {
    valid: { x: '12', y: 24 },
    invalid: { x: 'nope', y: 10 }
  },
  viewport: {
    zoom: 1.5,
    translate_x: -20,
    translate_y: 30
  }
})
assert.deepEqual(layout, {
  nodes: { valid: { x: 12, y: 24 } },
  viewport: { zoom: 1.5, translate_x: -20, translate_y: 30 }
})
assert.deepEqual(
  applyDAGNodePositions([{ id: 'valid' }, { id: 'other', x: 1, y: 2 }], layout),
  [{ id: 'valid', x: 12, y: 24 }, { id: 'other', x: 1, y: 2 }]
)

const initialViewportMatrix = [1, 0, 0, 0, 1, 0, 120, 80, 1]
let viewportMatrix = initialViewportMatrix
let viewportPaintCount = 0
const viewportGraph = {
  zoomTo(zoom) {
    viewportMatrix[0] = zoom
    viewportMatrix[4] = zoom
    viewportMatrix[6] = -999
    viewportMatrix[7] = -888
  },
  getGroup() {
    return {
      getMatrix: () => viewportMatrix,
      setMatrix: matrix => { viewportMatrix = matrix }
    }
  },
  paint() {
    viewportPaintCount += 1
  }
}
restoreDAGViewport(viewportGraph, layout)
assert.deepEqual(viewportMatrix, [1.5, 0, 0, 0, 1.5, 0, -20, 30, 1])
assert.notEqual(viewportMatrix, initialViewportMatrix)
assert.equal(viewportPaintCount, 1)

console.log('common DAG editing tests passed')
