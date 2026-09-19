import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { buildStandardElementRevisionLocation, buildStandardElementRevisionRoute } from '../src/utils/standardElementRoute.mjs'

test('standard frozen references share one local and public route contract', () => {
  assert.deepEqual(buildStandardElementRevisionLocation(51, 5102), { path: '/elements/51', query: { revision_id: '5102' } })
  assert.equal(buildStandardElementRevisionRoute('51', '5102'), '/standard/elements/51?revision_id=5102')
})

test('standard reference route rejects missing, ambiguous or injected identities', () => {
  for (const value of [null, undefined, '', 0, -1, '01', '1&revision_id=2', ['1'], '1.2', '1e2']) {
    assert.throws(() => buildStandardElementRevisionRoute(value, 1))
    assert.throws(() => buildStandardElementRevisionRoute(1, value))
  }
})

test('Model and Standard consume the shared route contract without copying a revision route', () => {
  const read = path => readFileSync(new URL(`../../../${path}`, import.meta.url), 'utf8')
  const link = read('model/frontend/src/components/FrozenElementLink.vue')
  assert.match(link, /openConsoleRoute\(buildStandardElementRevisionRoute\(/)
  assert.doesNotMatch(link, /\/standard\/elements\//)
  for (const view of ['EntityDetail', 'LogicalTableDetail']) {
    assert.match(read(`model/frontend/src/views/${view}.vue`), /<FrozenElementLink /)
  }
  const standard = read('standard/frontend/src/views/ElementDetail.vue')
  assert.match(standard, /buildStandardElementRevisionLocation\(element\.value\.id, item\.id\)/)
})
