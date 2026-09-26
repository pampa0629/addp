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
  assert.equal(option.series[0].tooltip.valueFormatter(12.5, 0), '12.50 元')
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
  assert.equal(option.series[0].tooltip.valueFormatter(125, 0), '125 元 · 高额')
  assert.equal(option.series[0].label.formatter({ dataIndex: 0 }), '125 元')
  assert.equal(option.series[0].itemStyle, undefined)
})

test('bar labels show zero and signed values with field units and locale precision', () => {
  const rows = [{ period: 'A', count: 89, amount: -1234.5 }, { period: 'B', count: 0, amount: 0 }]
  const config = {
    chart_type: 'bar', dimension: 'period', measures: ['count', 'amount'],
    field_presentations: [{ field: 'count', unit: '次', precision: 0 }, { field: 'amount', unit: 'EUR', precision: 2 }],
  }
  const option = buildChartOption(rows, config, 'de-DE', { textColor: 'theme-text' })
  for (const series of option.series) {
    assert.equal(series.label.show, true)
    assert.equal(series.label.position, 'top')
    assert.equal(series.label.color, 'theme-text')
  }
  assert.equal(option.series[0].label.formatter({ dataIndex: 0 }), '89 次')
  assert.equal(option.series[0].label.formatter({ dataIndex: 1 }), '0 次')
  assert.equal(option.series[1].label.formatter({ dataIndex: 0 }), '-1.234,50 EUR')
  assert.deepEqual(option.series[1].data, [-1234.5, 0])
  assert.equal(buildChartOption(rows, { ...config, chart_type: 'line' }).series[0].label, undefined)
})

test('chart geometry may approximate decimal values while labels use exact source digits', () => {
  const value = '9043526590.462176100000000010'
  for (const chart_type of ['bar', 'line', 'pie']) {
    const option = buildChartOption([{ city: '长沙', value }], {
      chart_type, dimension: 'city', measures: ['value'],
      field_presentations: [{ field: 'value', precision: 8, unit: 'm²' }],
    }, 'en-US')
    assert.equal(option.series[0].tooltip.valueFormatter(option.series[0].data[0], 0), '9,043,526,590.46217610 m²')
    if (chart_type === 'bar') {
      assert.equal(option.series[0].label.formatter({ dataIndex: 0 }), '9,043,526,590.46217610 m²')
    }
  }
})

test('enables one theme-controlled selected item without changing numeric series data', () => {
  const option = buildChartOption(
    [{ city: 'A', amount: 2 }, { city: 'B', amount: 3 }],
    { chart_type: 'bar', dimension: 'city', measures: ['amount'] },
    'zh-CN',
    { selectionColor: 'theme-selection-color' },
  )

  assert.deepEqual(option.series[0].data, [2, 3])
  assert.equal(option.series[0].selectedMode, 'single')
  assert.equal(option.series[0].select.itemStyle.color, 'theme-selection-color')
})

test('rejects incomplete or invalid pie data', () => {
  const config = { chart_type: 'pie', dimension: 'city', measures: ['amount'] }
  assert.equal(validateChartResult([{ city: 'A', amount: -1 }], config).reason, 'invalid_measure')
  assert.equal(validateChartResult(Array.from({ length: 21 }, (_, index) => ({ city: index, amount: 1 })), config).reason, 'result_limit')
})

test('axis spacing respects explicit precision without rounding the series', () => {
  for (const precision of [0, 2, 6]) {
    const config = {
      chart_type: 'bar', dimension: 'period', measures: ['value'],
      field_presentations: [{ field: 'value', precision }],
    }
    for (const values of [[0, 0], [0.001, 0.002], [-2, -1], [1, 2]]) {
      const option = buildChartOption(values.map(value => ({ period: 'A', value })), config)
      assert.equal(option.yAxis.minInterval, 10 ** -precision)
      assert.deepEqual(option.series[0].data, values)
    }
  }
  const config = { chart_type: 'line', dimension: 'period', measures: ['a', 'b'], field_presentations: [{ field: 'a', precision: 0 }] }
  assert.equal(buildChartOption([], config).yAxis.minInterval, undefined)
  assert.equal(buildChartOption([], { ...config, measures: ['b'] }).yAxis.minInterval, undefined)
})

test('theme colors reach all chart text and tooltip surfaces', () => {
  const theme = { textColor: 'primary-text', secondaryTextColor: 'secondary-text', borderColor: 'border', splitLineColor: 'grid', backgroundColor: 'surface' }
  for (const chart_type of ['bar', 'line', 'pie']) {
    const option = buildChartOption([{ city: 'A', amount: 1 }], { chart_type, dimension: 'city', measures: ['amount'] }, 'zh-CN', theme)
    assert.equal(option.textStyle.color, theme.textColor)
    assert.equal(option.legend.textStyle.color, theme.textColor)
    assert.equal(option.tooltip.textStyle.color, theme.textColor)
    assert.equal(option.tooltip.backgroundColor, theme.backgroundColor)
    assert.equal(option.tooltip.borderColor, theme.borderColor)
    if (chart_type === 'pie') assert.equal(option.series[0].label.color, theme.textColor)
    else {
      for (const axis of [option.xAxis, option.yAxis]) {
        assert.equal(axis.axisLabel.color, theme.secondaryTextColor)
        assert.equal(axis.nameTextStyle.color, theme.secondaryTextColor)
      }
      assert.equal(option.yAxis.splitLine.lineStyle.color, theme.splitLineColor)
      assert.equal(option.yAxis.nameTextStyle.align, 'left')
    }
  }
})
