import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const readSource = (relative) => readFileSync(new URL(relative, import.meta.url), 'utf8')

test('OpenLayers popup pans fully into view with a controlled margin', () => {
  const source = readSource('../src/composables/useOpenLayersMap.js')

  assert.match(source, /autoPan:\s*\{[\s\S]*margin:\s*24[\s\S]*animation:\s*\{\s*duration:\s*0\s*\}/)
})

test('GeoJSON renderer forwards an explicit preserve-view decision', () => {
  const source = readSource('../src/components/GeoJSONResultRenderer.vue')

  assert.match(source, /preserveView:\s*\{\s*type:\s*Boolean,\s*default:\s*false\s*\}/)
  assert.match(source, /:preserve-view="preserveView"/)
})
