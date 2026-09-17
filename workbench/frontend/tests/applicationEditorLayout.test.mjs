import test from 'node:test'
import assert from 'node:assert/strict'
import { arrangePlacements, applicationQueryContext } from '../src/utils/applicationEditorLayout.mjs'

const placements = [
  { component_id: 'a', x: 0, y: 0, width: 6, height: 4 },
  { component_id: 'b', x: 6, y: 0, width: 6, height: 8 },
  { component_id: 'c', x: 0, y: 8, width: 12, height: 6 },
]
function assertDisjoint(items) {
  for (const a of items) {
    assert.ok(a.x >= 0 && a.x + a.width <= 12 && a.y >= 0)
    for (const b of items) if (a !== b) assert.ok(a.x + a.width <= b.x || b.x + b.width <= a.x || a.y + a.height <= b.y || b.y + b.height <= a.y)
  }
}
test('resizing and quick layouts pack unequal component heights without overlaps', () => {
  const original = structuredClone(placements)
  for (const change of [{ componentID: 'a', width: 12 }, { componentID: 'a', height: 12 }, { columns: 1 }, { columns: 2 }]) {
    const result = arrangePlacements(placements, change)
    assertDisjoint(result)
    assert.deepEqual(result.map(p => p.component_id), ['a', 'b', 'c'])
  }
  assert.deepEqual(placements, original)
  assert.equal(arrangePlacements(placements, { columns: 2 })[2].y, 8)
})
test('drag and keyboard order actions preserve component identities and sizes', () => {
  assert.deepEqual(arrangePlacements(placements, { componentID: 'c', beforeID: 'a' }).map(p => p.component_id), ['c', 'a', 'b'])
  assert.deepEqual(arrangePlacements(placements, { componentID: 'a', direction: 1 }).map(p => p.component_id), ['b', 'a', 'c'])
  assert.deepEqual(arrangePlacements(placements, { componentID: 'a', beforeID: 'c' }).map(p => p.component_id), ['b', 'a', 'c'])
  assert.deepEqual(arrangePlacements(placements, { componentID: 'a', direction: -1 }), placements)
})
test('presentation-only changes keep preview results while query changes invalidate them', () => {
  const snapshot = { components: [{ id: 'a', title: 'Before', renderer_config: { columns: ['value'] }, query_template: { select: ['value'] } }], parameters: [{ key: 'id', default_value: '1' }], parameter_bindings: [], page: { placements } }
  const original = applicationQueryContext(snapshot)
  snapshot.parameters[0] = { default_value: '1', key: 'id' }
  assert.equal(applicationQueryContext(snapshot), original)
  snapshot.components[0].title = 'After'
  snapshot.components[0].renderer_config.precision = 2
  snapshot.page.placements = arrangePlacements(placements, { columns: 1 })
  assert.equal(applicationQueryContext(snapshot), original)
  snapshot.parameters[0].default_value = '2'
  assert.notEqual(applicationQueryContext(snapshot), original)
})
