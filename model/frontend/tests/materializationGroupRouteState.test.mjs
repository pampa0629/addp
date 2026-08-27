import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildMaterializationGroupRouteQuery,
  resolveMaterializationGroupRouteState
} from '../src/utils/materializationGroupRouteState.js'

test('materialization group route keeps dialog and pagination state', () => {
  assert.deepEqual(buildMaterializationGroupRouteQuery({ mode: 'edit', groupID: 8, page: 3, pageSize: 50 }), {
    group_id: '8', page: '3', page_size: '50'
  })
  assert.deepEqual(resolveMaterializationGroupRouteState({ create: '1' }), {
    mode: 'create', groupID: '', page: 1, pageSize: 20, query: { create: '1' }, changed: false
  })
})

test('materialization group route rejects ambiguous and invalid query values', () => {
  const state = resolveMaterializationGroupRouteState({ create: '1', group_id: '5', page: '-1', page_size: '999' })
  assert.equal(state.mode, 'edit')
  assert.equal(state.groupID, '5')
  assert.equal(state.page, 1)
  assert.equal(state.pageSize, 20)
  assert.deepEqual(state.query, { group_id: '5' })
  assert.equal(state.changed, true)
})
