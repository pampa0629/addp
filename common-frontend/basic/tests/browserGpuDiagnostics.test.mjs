import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createBrowserGpuDiagnostics } from '../src/utils/browserGpuDiagnostics.mjs'

const text = '[.WebGL-0x123abc]GL Driver Message (OpenGL, Performance, GL_CLOSE_PATH_NV, High): GPU stall due to ReadPixels'
const message = (value = text, type = 'warning') => ({ type: () => type, text: () => value })
test('GPU diagnostic records observed stages and counts without hiding other warnings', () => {
  const diagnostics = createBrowserGpuDiagnostics('raster-cog:preview')
  assert.equal(diagnostics.record(message()), true)
  assert.equal(diagnostics.record(message(text + ' (this message will no longer repeat)')), true)
  diagnostics.setStage('raster-cog:screenshot')
  assert.equal(diagnostics.record(message()), true)
  for (const other of [message(text, 'error'), message(text + ' unexpected'), message('Canvas2D: Multiple readback operations using getImageData'), message('GPU stall due to ReadPixels')]) {
    assert.equal(diagnostics.record(other), false)
  }
  const snapshot = diagnostics.snapshot()
  assert.deepEqual(snapshot, { count: 3, stages: [
    { source: 'chromium-webgl-driver', stage: 'raster-cog:preview', count: 2 },
    { source: 'chromium-webgl-driver', stage: 'raster-cog:screenshot', count: 1 }
  ] })
  snapshot.stages[0].count = 99
  assert.equal(diagnostics.snapshot().count, 3)
})
