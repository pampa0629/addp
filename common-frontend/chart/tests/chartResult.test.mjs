import assert from 'node:assert/strict'
import test from 'node:test'
import { buildChartOption, resultSelectionFromChartEvent, validateChartResult } from '../src/chartResult.mjs'

test('builds bounded chart options without aggregating service rows', () => {
  const rows = [{ city: 'A', amount: 2 }, { city: 'B', amount: 3 }]
  const config = { chart_type: 'bar', dimension: 'city', measures: ['amount'] }
  assert.deepEqual(validateChartResult(rows, config, false), { valid: true, reason: '' })
  assert.deepEqual(buildChartOption(rows, config).series[0].data, [2, 3])
  assert.equal(validateChartResult(rows, config, true).reason, 'partial_result')
})

test('maps an ECharts item click to the original result index', () => {
  assert.deepEqual(resultSelectionFromChartEvent({ dataIndex: 1 }, 2), { row_index: 1 })
  assert.equal(resultSelectionFromChartEvent({ dataIndex: 2 }, 2), null)
  assert.equal(resultSelectionFromChartEvent({}, 2), null)
})

test('uses field presentations for chart labels and tooltip values without changing numeric series data', () => {
  const option = buildChartOption(
    [{ occurred_on: '2026-09-06', amount: 12.5 }],
    {
      chart_type: 'bar', dimension: 'occurred_on', measures: ['amount'],
      field_presentations: [
        { field: 'occurred_on', label: '日期', temporal_format: 'date' },
        { field: 'amount', label: '金额', unit: '元', precision: 2 },
      ],
    },
    'zh-CN',
  )

  assert.equal(option.series[0].name, '金额')
  assert.deepEqual(option.series[0].data, [12.5])
  assert.equal(option.series[0].tooltip.valueFormatter(12.5), '12.50 元')
  assert.equal(option.yAxis.name, '金额（元）')
  assert.equal(option.yAxis.axisLabel.formatter(12.5), '12.50')
  assert.equal(option.xAxis.nameLocation, 'middle')
  assert.equal(option.xAxis.nameGap, 32)
  assert.match(option.xAxis.data[0], /2026/)
})

test('adds a controlled state label to chart tooltips without recoloring series data', () => {
  const option = buildChartOption([{ city: 'A', amount: 125 }], {
    chart_type: 'bar', dimension: 'city', measures: ['amount'],
    field_presentations: [{
      field: 'amount', label: '金额', unit: '元', precision: 0,
      state_rules: [{ operator: 'gt', operand: 100, label: '高额', tone: 'warning' }],
    }],
  }, 'zh-CN')

  assert.deepEqual(option.series[0].data, [125])
  assert.equal(option.series[0].tooltip.valueFormatter(125), '125 元 · 高额')
  assert.equal(option.series[0].itemStyle, undefined)
})

test('rejects incomplete or invalid pie data', () => {
  const config = { chart_type: 'pie', dimension: 'city', measures: ['amount'] }
  assert.equal(validateChartResult([{ city: 'A', amount: -1 }], config).reason, 'invalid_measure')
  assert.equal(validateChartResult(Array.from({ length: 21 }, (_, index) => ({ city: index, amount: 1 })), config).reason, 'result_limit')
})
