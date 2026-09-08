import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const mapLayersSource = readFileSync(new URL('../src/config/mapLayers.js', import.meta.url), 'utf8')

test('OpenStreetMap raster layer uses the canonical tile endpoint', () => {
  assert.match(mapLayersSource, /https:\/\/tile\.openstreetmap\.org\/\{z\}\/\{x\}\/\{y\}\.png/)
  assert.doesNotMatch(mapLayersSource, /\{a-c\}\.tile\.openstreetmap\.org/)
})
