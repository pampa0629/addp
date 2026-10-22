import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { normalizeQueryTimestamp, resolveRuntimeInstanceTime, runtimeInstancePeriods, runtimeInstanceTimeParams, validRuntimeInstanceRange } from '../src/utils/runtimeInstanceTime.mjs'

test('runtime instance time uses one preset catalog and one moving anchor per request', () => {
  const now = Date.parse('2026-10-22T10:00:00Z')
  assert.deepEqual(runtimeInstancePeriods.map(item => item.value), ['15m', '30m', '1h', '6h', '12h', '24h', '7d'])
  for (const preset of runtimeInstancePeriods) {
    const state = resolveRuntimeInstanceTime({ time_basis: 'offline', time_period: preset.value })
    const first = runtimeInstanceTimeParams(state, now), next = runtimeInstanceTimeParams(state, now + 15000)
    assert.equal(first.time_basis, 'offline')
    assert.equal(Date.parse(first.time_to), now)
    assert.equal(Date.parse(first.time_to) - Date.parse(first.time_from), preset.minutes * 60000)
    assert.equal(Date.parse(next.time_from) - Date.parse(first.time_from), 15000)
    assert.deepEqual(state.query, { time_basis: 'offline', time_period: preset.value })
  }
  assert.deepEqual(runtimeInstanceTimeParams(resolveRuntimeInstanceTime()), {})
  assert.deepEqual(runtimeInstanceTimeParams(resolveRuntimeInstanceTime({ time_basis: 'offline' })), { time_basis: 'offline' })
})

test('custom ranges normalize to UTC and remain fixed on refresh', () => {
  const state = resolveRuntimeInstanceTime({ time_period: 'custom', time_from: '2026-10-22T18:00:00+08:00', time_to: '2026-10-22T19:00:00+08:00' })
  assert.deepEqual(state.query, { time_period: 'custom', time_from: '2026-10-22T10:00:00.000Z', time_to: '2026-10-22T11:00:00.000Z' })
  assert.deepEqual(runtimeInstanceTimeParams(state, 0), runtimeInstanceTimeParams(state, Date.now()))
  assert.equal(validRuntimeInstanceRange([new Date(state.from), new Date(state.to)]), true)
  for (const range of [null, [], [new Date()], [new Date(state.to), new Date(state.from)], [new Date('invalid'), new Date()]]) assert.equal(validRuntimeInstanceRange(range), false)
})

test('time normalization rejects invalid, incomplete, duplicated and timezone-free inputs', () => {
  for (const time of [null, [], {}, '', '2026-02-30T00:00:00Z', '2026-10-22T24:00:00Z', '2026-10-22T00:00:00', '2026-10-22']) assert.equal(normalizeQueryTimestamp(time), '')
  for (const input of [
    { time_period: ['1h'] }, { time_period: 'custom' },
    { time_period: 'custom', time_from: '2026-10-22T11:00:00Z', time_to: '2026-10-22T10:00:00Z' },
    { time_period: 'custom', time_from: ['2026-10-22T10:00:00Z'], time_to: '2026-10-22T11:00:00Z' },
    { time_basis: ['offline'], time_period: 'future' }
  ]) assert.deepEqual(resolveRuntimeInstanceTime(input).query, {})
})

test('System and Monitor own only adapters to the same time UI and arithmetic', () => {
  for (const [module, view, utility] of [['system', 'components/ModuleInstances', 'routeState'], ['monitor', 'views/ProcessResources', 'processResources']]) {
    const source = readFileSync(new URL(`../../../${module}/frontend/src/${view}.vue`, import.meta.url), 'utf8')
    assert.match(source, /import RuntimeInstanceTimeFilter from .*common-frontend\/basic\/src\/components\/RuntimeInstanceTimeFilter\.vue/)
    assert.match(source, /<RuntimeInstanceTimeFilter\b/)
    assert.doesNotMatch(source, /<el-date-picker|preset\.minutes|const periods =/)
    const filters = readFileSync(new URL(`../../../${module}/frontend/src/utils/${utility}.js`, import.meta.url), 'utf8')
    assert.match(filters, /resolveRuntimeInstanceTime/)
    assert.doesNotMatch(filters, /MODULE_INSTANCE_PERIODS|function queryTime/)
  }
  const exports = readFileSync(new URL('../src/index.js', import.meta.url), 'utf8')
  assert.match(exports, /RuntimeInstanceTimeFilter/)
  assert.match(exports, /runtimeInstanceTime\.mjs/)
})
