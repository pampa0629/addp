import assert from 'node:assert/strict'
import test from 'node:test'
import { buildChartOption, validateChartResult } from '../src/chartResult.mjs'

test('builds bounded chart options without aggregating service rows', () => {
  const rows = [{ city: 'A', amount: 2 }, { city: 'B', amount: 3 }]
  const config = { chart_type: 'bar', dimension: 'city', measures: ['amount'] }
  assert.deepEqual(validateChartResult(rows, config, false), { valid: true, reason: '' })
  assert.deepEqual(buildChartOption(rows, config).series[0].data, [2, 3])
  assert.equal(validateChartResult(rows, config, true).reason, 'partial_result')
})

test('rejects incomplete or invalid pie data', () => {
  const config = { chart_type: 'pie', dimension: 'city', measures: ['amount'] }
  assert.equal(validateChartResult([{ city: 'A', amount: -1 }], config).reason, 'invalid_measure')
  assert.equal(validateChartResult(Array.from({ length: 21 }, (_, index) => ({ city: index, amount: 1 })), config).reason, 'result_limit')
})
