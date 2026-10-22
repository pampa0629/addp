import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { normalizeRuntimeInstanceStopReason, runtimeInstanceStopReasonLabelKey, runtimeInstanceStopReasons } from '../src/utils/runtimeInstanceStopReason.mjs'

test('System stop reasons have one bounded catalog and reject unknown or duplicate route values', () => {
  assert.deepEqual(runtimeInstanceStopReasons, ['graceful', 'lease_expired'])
  for (const reason of runtimeInstanceStopReasons) {
    assert.equal(normalizeRuntimeInstanceStopReason(` ${reason} `), reason)
    assert.equal(runtimeInstanceStopReasonLabelKey(reason), `common.runtimeInstanceStopReason.reasons.${reason}`)
  }
  for (const value of [undefined, null, '', [], ['graceful'], {}, 'unexpected', 'GRACEFUL']) {
    assert.equal(normalizeRuntimeInstanceStopReason(value), '')
    assert.equal(runtimeInstanceStopReasonLabelKey(value), '')
  }
})

test('System and Monitor share the only reason selector, normalization and bilingual presentation', () => {
  for (const [module, view, utility] of [['system', 'components/ModuleInstances', 'routeState'], ['monitor', 'views/ProcessResources', 'processResources']]) {
    const source = readFileSync(new URL(`../../../${module}/frontend/src/${view}.vue`, import.meta.url), 'utf8')
    assert.match(source, /import RuntimeInstanceStopReasonSelect from .*common-frontend\/basic\/src\/components\/RuntimeInstanceStopReasonSelect\.vue/)
    assert.match(source, /<RuntimeInstanceStopReasonSelect\b/)
    assert.match(source, /runtimeInstanceStopReasonLabelKey/)
    assert.doesNotMatch(source, /<el-select v-model="stopReason"|system\.module\.stopReasons/)
    const filters = readFileSync(new URL(`../../../${module}/frontend/src/utils/${utility}.js`, import.meta.url), 'utf8')
    assert.match(filters, /normalizeRuntimeInstanceStopReason/)
    assert.doesNotMatch(filters, /MODULE_INSTANCE_STOP_REASONS/)
  }
  const overview = readFileSync(new URL('../../../system/frontend/src/views/Modules.vue', import.meta.url), 'utf8')
  assert.match(overview, /runtimeInstanceStopReasonLabelKey/)
  const logs = readFileSync(new URL('../../../system/frontend/src/components/ModuleRuntimeLogs.vue', import.meta.url), 'utf8')
  assert.match(logs, /common\.runtimeInstanceStopReason\.leaseExpiredHint/)
  assert.doesNotMatch(logs, /system\.module\.instances\.leaseExpiredHint/)
  for (const language of ['zh-cn', 'en']) {
    const shared = JSON.parse(readFileSync(new URL(`../src/i18n/${language}.json`, import.meta.url), 'utf8')).common.runtimeInstanceStopReason
    for (const reason of runtimeInstanceStopReasons) assert.equal(typeof shared.reasons[reason], 'string')
    assert.ok(shared.label && shared.all && shared.leaseExpiredHint)
    const system = JSON.parse(readFileSync(new URL(`../../../system/frontend/src/i18n/${language}.json`, import.meta.url), 'utf8')).system.module
    assert.equal(system.stopReasons, undefined)
    assert.equal(system.instances.stopReason, undefined)
  }
  const exports = readFileSync(new URL('../src/index.js', import.meta.url), 'utf8')
  assert.match(exports, /RuntimeInstanceStopReasonSelect/)
  assert.match(exports, /runtimeInstanceStopReason\.mjs/)
})
