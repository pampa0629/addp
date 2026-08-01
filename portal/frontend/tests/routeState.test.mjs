import test from 'node:test'
import assert from 'node:assert/strict'
import {
  assetDetailReturnTarget,
  resolveCatalogRouteState,
  resolveSearchRouteState
} from '../src/utils/routeState.js'

test('search route trims values, removes unknown parameters, and omits page one', () => {
  assert.deepEqual(resolveSearchRouteState({
    keyword: ' Business ',
    type_id: '2',
    page: '1',
    legacy: 'value'
  }), {
    keyword: 'Business',
    typeId: 2,
    page: 1,
    query: { keyword: 'Business', type_id: '2' },
    changed: true
  })
})

test('search route preserves canonical pagination state', () => {
  const query = { keyword: 'Business', type_id: '2', page: '3' }
  assert.deepEqual(resolveSearchRouteState(query), {
    keyword: 'Business',
    typeId: 2,
    page: 3,
    query,
    changed: false
  })
})

test('catalog route accepts only a positive non-default page', () => {
  assert.deepEqual(resolveCatalogRouteState({ page: '2' }), {
    page: 2,
    query: { page: '2' },
    changed: false
  })
  assert.deepEqual(resolveCatalogRouteState({ page: '0', legacy: 'value' }), {
    page: 1,
    query: {},
    changed: true
  })
})

test('asset detail returns only to Portal history and otherwise uses owner data', () => {
  assert.deepEqual(assetDetailReturnTarget('/portal/search?keyword=test', 9), { history: 'back' })
  assert.deepEqual(assetDetailReturnTarget('/system/iam', 9), {
    history: 'replace',
    location: { name: 'Catalog', params: { id: '9' } }
  })
  assert.deepEqual(assetDetailReturnTarget('https://example.com/portal/search', null), {
    history: 'replace',
    location: { name: 'Search' }
  })
})
