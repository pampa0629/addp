import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import {
  assetDetailReturnTarget,
  resolveCategoryRouteState,
  resolveSearchRouteState
} from '../src/utils/routeState.js'

const readSource = path => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8')

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

test('category route accepts only a positive non-default page', () => {
  assert.deepEqual(resolveCategoryRouteState({ page: '2' }), {
    page: 2,
    query: { page: '2' },
    changed: false
  })
  assert.deepEqual(resolveCategoryRouteState({ page: '0', legacy: 'value' }), {
    page: 1,
    query: {},
    changed: true
  })
})

test('asset detail returns only to Portal history and otherwise uses owner data', () => {
  assert.deepEqual(assetDetailReturnTarget('/portal/search?keyword=test', 9), { history: 'back' })
  assert.deepEqual(assetDetailReturnTarget('/system/iam', 9), {
    history: 'replace',
    location: { name: 'Category', params: { id: '9' } }
  })
  assert.deepEqual(assetDetailReturnTarget('https://example.com/portal/search', null), {
    history: 'replace',
    location: { name: 'Search' }
  })
})

test('Portal exposes only the AssetCategory navigation contract', () => {
  const api = readSource('api/portal.js')
  const router = readSource('router/index.js')
  const detail = readSource('views/AssetDetail.vue')

  assert.match(api, /\/portal\/categories/)
  assert.doesNotMatch(api, /\/portal\/catalogs/)
  assert.match(router, /portal\/categories\/:id/)
  assert.match(detail, /asset\.value\?\.category_id/)
})

test('Portal presents AssetCategory navigation as the Asset Directory', () => {
  const zhCn = JSON.parse(readSource('i18n/zh-cn.json')).portal
  const en = JSON.parse(readSource('i18n/en.json')).portal

  assert.equal(zhCn.category.title, '资产目录')
  assert.equal(zhCn.category.browse, '资产目录浏览')
  assert.equal(en.category.title, 'Asset Directory')
  assert.doesNotMatch(JSON.stringify(en), /Asset Catalog/)
})
