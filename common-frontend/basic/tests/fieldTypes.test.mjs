import test from 'node:test'
import assert from 'node:assert/strict'

import { normalizeFieldType } from '../src/utils/fieldTypes.js'

test('normalizes parameterized spatial types to the transfer geometry type', () => {
  assert.equal(normalizeFieldType('GEOMETRY(Polygon, 32650)'), 'geometry')
  assert.equal(normalizeFieldType({ type: 'geography(Point, 4326)' }), 'geometry')
  assert.equal(normalizeFieldType({ type: 'text', is_geometry: true }), 'geometry')
})

test('keeps canonical ordinary and decimal types stable', () => {
  assert.equal(normalizeFieldType('decimal'), 'decimal')
  assert.equal(normalizeFieldType('DECIMAL(20,10)'), 'decimal')
  assert.equal(normalizeFieldType('timestamp'), 'timestamp')
})
