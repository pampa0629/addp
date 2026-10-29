import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { buildMonitorEngineResourcesLocation, buildMonitorEngineResourcesRoute } from '../src/utils/engineResourceRoute.mjs'

test('engine monitoring local and Console navigation share one encoded route', () => {
  const query = { deployment_id: '10000000-0000-4000-8000-000000000001', search: 'Infra & Business/测试', range: '5m' }
  const location = buildMonitorEngineResourcesLocation(query)
  assert.deepEqual(location, { path: '/engine-resources', query })
  assert.notEqual(location.query, query)
  const url = new URL(buildMonitorEngineResourcesRoute(query), 'https://console.example')
  assert.equal(url.pathname, `/monitor${location.path}`)
  assert.deepEqual(Object.fromEntries(url.searchParams), query)
  assert.equal(buildMonitorEngineResourcesRoute(), '/monitor/engine-resources')
})

test('engine route refuses ambiguous query shapes', () => {
  for (const query of [null, [], 'id', { deployment_id: undefined }, { range: ['5m'] }, { page: 1 }]) {
    assert.throws(() => buildMonitorEngineResourcesLocation(query), TypeError)
    assert.throws(() => buildMonitorEngineResourcesRoute(query), TypeError)
  }
})

test('System and Monitor consume the shared engine route without copying its path', () => {
  const read = path => readFileSync(new URL(`../../../${path}`, import.meta.url), 'utf8')
  const system = read('system/frontend/src/views/EngineDeployments.vue')
  assert.match(system, /openConsoleRoute\(buildMonitorEngineResourcesRoute\(/)
  assert.doesNotMatch(system, /navigateConsoleModuleRoute|['"]\/engine-resources/)
  const monitor = read('monitor/frontend/src/views/EngineResources.vue')
  assert.match(monitor, /buildMonitorEngineResourcesLocation\(resolveEngineRoute\(query\)\.query\)/)
  assert.doesNotMatch(monitor, /path: ['"]\/engine-resources/)
})
