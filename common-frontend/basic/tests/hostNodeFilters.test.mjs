import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { hostNodeKinds, resolveHostNodeFilters } from '../src/utils/hostNodeFilters.mjs'

test('host filters preserve exact System enums including false and omit invalid or repeated values', () => {
  assert.deepEqual(hostNodeKinds, ['physical', 'virtual'])
  for (const node_kind of hostNodeKinds) for (const enabled of ['true', 'false']) {
    assert.deepEqual(resolveHostNodeFilters({ node_kind, enabled }), { nodeKind: node_kind, enabled, query: { node_kind, enabled } })
  }
  for (const value of [undefined, null, '', 'all', 'TRUE', ' physical ', [], ['true'], {}, false, 0]) {
    assert.deepEqual(resolveHostNodeFilters({ node_kind: value, enabled: value }), { nodeKind: '', enabled: '', query: {} })
  }
})

test('host filter selection, normalization and bilingual labels have one shared owner', () => {
  for (const [module, view, utility] of [['system', 'HostNodes', 'routeState'], ['monitor', 'NodeResources', 'nodeResources']]) {
    const viewSource = readFileSync(new URL(`../../../${module}/frontend/src/views/${view}.vue`, import.meta.url), 'utf8')
    assert.match(viewSource, /import HostNodeFilters from .*common-frontend\/basic\/src\/components\/HostNodeFilters.vue/)
    assert.match(viewSource, /<HostNodeFilters /)
    assert.doesNotMatch(viewSource, /<el-select :model-value="state.nodeKind"/)
    const utilitySource = readFileSync(new URL(`../../../${module}/frontend/src/utils/${utility}.js`, import.meta.url), 'utf8')
    assert.match(utilitySource, /resolveHostNodeFilters/)
    assert.doesNotMatch(utilitySource, /\['physical', 'virtual'\]\.includes/)
  }
  const component = readFileSync(new URL('../src/components/HostNodeFilters.vue', import.meta.url), 'utf8')
  assert.doesNotMatch(component, /useAuthStore|localStorage|axios|system\//)
  for (const lang of ['zh-cn', 'en']) {
    const shared = JSON.parse(readFileSync(new URL(`../src/i18n/${lang}.json`, import.meta.url))).common.hostNodeFilters
    for (const key of ['kind', 'state', 'allKinds', 'allStates']) assert.ok(shared[key])
    for (const kind of hostNodeKinds) assert.ok(shared.kinds[kind])
    const system = JSON.parse(readFileSync(new URL(`../../../system/frontend/src/i18n/${lang}.json`, import.meta.url))).system.hostNodes
    assert.equal(system.allKinds, undefined)
    assert.equal(system.allStates, undefined)
  }
})
