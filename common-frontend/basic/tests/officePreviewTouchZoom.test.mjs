import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

import {
  getTouchDistance,
  resolvePinchZoom,
  resolveWheelZoom
} from '../src/lib/office/pinchZoom.js'

const repositoryRoot = resolve(fileURLToPath(new URL('../../..', import.meta.url)))

test('Office preview pinch zoom uses the distance ratio within the toolbar limits', () => {
  assert.equal(getTouchDistance([
    { clientX: 0, clientY: 0 },
    { clientX: 30, clientY: 40 }
  ]), 50)
  assert.equal(resolvePinchZoom(1, 100, 150, 0.5, 2), 1.5)
  assert.equal(resolvePinchZoom(1.5, 100, 200, 0.5, 2), 2)
  assert.equal(resolvePinchZoom(0.75, 100, 20, 0.5, 2), 0.5)
})

test('Office preview trackpad pinch maps modified wheel deltas to bounded zoom', () => {
  assert.equal(resolveWheelZoom(1, -10, 0.5, 2), 1.11)
  assert.equal(resolveWheelZoom(1, 10, 0.5, 2), 0.9)
  assert.equal(resolveWheelZoom(2, -20, 0.5, 2), 2)
  assert.equal(resolveWheelZoom(0.5, 20, 0.5, 2), 0.5)
})

test('Office preview only captures two-finger zoom while its own preview is fullscreen', () => {
  const officePreview = readFileSync(
    resolve(repositoryRoot, 'common-frontend/basic/src/components/previews/OfficePreview.vue'),
    'utf8'
  )

  assert.match(officePreview, /@touchstart="startPinchZoom"/)
  assert.match(officePreview, /@touchmove="updatePinchZoom"/)
  assert.match(officePreview, /@wheel="handleFullscreenWheelZoom"/)
  assert.match(officePreview, /document\.fullscreenElement !== fullscreenHost\.value/)
  assert.match(officePreview, /event\.touches\.length !== 2/)
  assert.match(officePreview, /!event\.ctrlKey && !event\.metaKey/)
  assert.match(officePreview, /\.office-preview:fullscreen \.office-scroll\s*{[^}]*touch-action: pan-x pan-y/s)
})
