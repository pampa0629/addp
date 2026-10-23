import assert from 'node:assert/strict'
import test from 'node:test'
import { buildChartOption, resultSelectionFromChartEvent, validateChartResult } from '../src/chartResult.mjs'

test('nullable Cartesian observations remain gaps and never become zero', () => {
  const rows = [{ at: 'A', value: 0 }, { at: 'B', value: null }, { at: 'C', value: 3 }]
  for (const chart_type of ['line', 'bar']) {
    const config = { chart_type, dimension: 'at', measures: ['value'] }
    assert.equal(validateChartResult(rows, config).valid, true)
    const option = buildChartOption(rows, config)
    assert.deepEqual(option.series[0].data, [0, null, 3])
    assert.equal(option.series[0].connectNulls, false)
    assert.equal(option.series[0].tooltip.valueFormatter(null, 1), '—')
  }
  assert.equal(validateChartResult(rows, { chart_type: 'pie', dimension: 'at', measures: ['value'] }).reason, 'invalid_measure')
})

test('line supports the full bounded 1000-point resource grid while other chart bounds hold', () => {
  const rows = Array.from({ length: 1000 }, (_, index) => ({ at: index, value: index % 2 ? null : index }))
  const config = { chart_type: 'line', dimension: 'at', measures: ['value'] }
  assert.equal(validateChartResult(rows, config).valid, true)
  assert.equal(validateChartResult([...rows, rows[0]], config).reason, 'result_limit')
  assert.equal(validateChartResult(rows.slice(0, 501), { ...config, chart_type: 'bar' }).reason, 'result_limit')
})

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


test('explicit capacity and percent axes retain units, values, null gaps and independent scales', () => {
  const rows = [{ at: 'A', total: 64, available: null, used: 25 }, { at: 'B', total: null, available: 0, used: 0 }]
  const config = { chart_type: 'line', dimension: 'at', measures: ['total', 'available', 'used'], field_presentations: [
    { field: 'total', label: '内存总量', unit: 'GiB', precision: 2 },
    { field: 'available', label: '可用内存', unit: 'GiB', precision: 2 },
    { field: 'used', label: '内存使用率', unit: '%', precision: 2 },
  ] }
  const valueAxes = [{ measures: ['total', 'available'], label: '内存容量', min: 0 }, { measures: ['used'], min: 0, max: 100 }]
  assert.equal(validateChartResult(rows, config, false, valueAxes).valid, true)
  for (const locale of ['zh-CN', 'en']) {
    const option = buildChartOption(rows, config, locale, { valueAxes, secondaryTextColor: 'theme-secondary' })
    assert.deepEqual(option.yAxis.map(axis => [axis.position, axis.min, axis.max]), [['left', 0, undefined], ['right', 0, 100]])
    assert.equal(option.yAxis[0].name, locale === 'en' ? '内存容量 (GiB)' : '内存容量（GiB）')
    assert.equal(option.yAxis[1].name, locale === 'en' ? '内存使用率 (%)' : '内存使用率（%）')
    assert.equal(option.yAxis[1].axisLabel.formatter(25), '25.00')
    assert.equal(option.yAxis[1].nameTextStyle.color, 'theme-secondary')
    assert.equal(option.yAxis[1].splitLine.show, false)
    assert.deepEqual(option.series.map(line => line.yAxisIndex), [0, 0, 1])
    assert.deepEqual(option.series.map(line => line.data), [[64, null], [null, 0], [25, 0]])
    assert.ok(option.series.every(line => line.connectNulls === false))
    assert.equal(option.series[0].tooltip.valueFormatter(64, 0), '64.00 GiB')
    assert.equal(option.series[2].tooltip.valueFormatter(25, 0), '25.00 %')
  }
  for (const valueAxes of [null, [{ measures: ['total'] }], [{ measures: ['total', 'available', 'used'] }],
    [{ measures: ['total', 'available'] }, { measures: ['used', 'used'] }],
    [{ measures: ['total', 'available'] }, { measures: ['used'], min: 100, max: 0 }],
    [{ measures: ['total', 'available'] }, { measures: ['used'], max: Infinity }],
    [{ measures: ['total', 'available'] }, { measures: ['used'], label: '' }],
    [{ measures: ['total', 'available'] }, { measures: ['used'], arbitrary: true }],
    [{ measures: ['total'] }, { measures: ['available'] }, { measures: ['used'] }]]) {
    assert.equal(validateChartResult(rows, config, false, valueAxes).reason, 'invalid_config')
  }
  assert.equal(validateChartResult([], { chart_type: 'pie', dimension: 'at', measures: ['used'] }, false, [{ measures: ['used'] }]).valid, false)
})


test('a host can declare a bounded dense Cartesian grid without changing default chart limits', () => {
  const rows = Array.from({ length: 1000 }, (_, at) => ({ at, receive: at === 1 ? null : 0, transmit: at }))
  const config = { chart_type: 'bar', dimension: 'at', measures: ['receive', 'transmit'] }
  assert.equal(validateChartResult(rows, config).reason, 'result_limit')
  assert.equal(validateChartResult(rows, config, false, [], 1000).valid, true)
  assert.equal(validateChartResult([...rows, rows[0]], config, false, [], 1000).reason, 'result_limit')
  assert.equal(validateChartResult(rows.slice(0, 101), config, false, [], 100).reason, 'result_limit')
  assert.equal(validateChartResult(rows, config, true, [], 1000).reason, 'partial_result')
  for (const limit of [0, -1, 1.5, 1001, Infinity, NaN, '1000', null]) {
    assert.equal(validateChartResult([], config, false, [], limit).reason, 'invalid_config')
  }
  assert.equal(validateChartResult([], { ...config, chart_type: 'pie', measures: ['receive'] }, false, [], 1000).reason, 'invalid_config')
})

test('dense grouped bars keep exact values, gaps, units and tooltips when persistent labels are hidden', () => {
  const rows = [{ at: 'A', receive: 0, transmit: 2 }, { at: 'B', receive: null, transmit: 1 }]
  const config = { chart_type: 'bar', dimension: 'at', measures: ['receive', 'transmit'], field_presentations: [
    { field: 'receive', label: 'Receive', unit: 'MiB/s', precision: 2 },
    { field: 'transmit', label: 'Transmit', unit: 'MiB/s', precision: 2 },
  ] }
  const valueAxes = [{ measures: config.measures, label: 'Network rate', min: 0 }]
  const option = buildChartOption(rows, config, 'en', { valueAxes, showValueLabels: false })
  assert.equal(option.yAxis[0].name, 'Network rate (MiB/s)')
  assert.equal(option.yAxis[0].min, 0)
  assert.deepEqual(option.series.map(series => series.data), [[0, null], [2, 1]])
  for (const series of option.series) {
    assert.equal(series.type, 'bar')
    assert.equal(series.label.show, false)
    assert.equal(series.stack, undefined)
    assert.equal(series.yAxisIndex, 0)
  }
  assert.equal(option.series[0].tooltip.valueFormatter(0, 0), '0.00 MiB/s')
  assert.equal(option.series[0].tooltip.valueFormatter(null, 1), '—')
  assert.equal(option.series[1].tooltip.valueFormatter(2, 0), '2.00 MiB/s')
  assert.equal(buildChartOption(rows, config).series[0].label.show, true)
})


test('optional zoom changes only the viewport, retains complete rows and follows theme colors', () => {
  const rows = Array.from({ length: 984 }, (_, at) => ({ at, value: at === 1 ? null : at }))
  const config = { chart_type: 'bar', dimension: 'at', measures: ['value'] }
  const theme = { zoomable: true, zoomStart: 95, zoomEnd: 100, secondaryTextColor: 'secondary', borderColor: 'border', backgroundColor: 'surface', splitLineColor: 'grid' }
  const option = buildChartOption(rows, config, 'en', theme)
  assert.equal(option.dataZoom[0].filterMode, 'none')
  assert.deepEqual([option.dataZoom[0].start, option.dataZoom[0].end], [95, 100])
  assert.deepEqual(option.series[0].data, rows.map(row => row.value))
  assert.equal(option.dataZoom[0].handleStyle.color, 'secondary')
  assert.equal(option.dataZoom[0].backgroundColor, 'surface')
  assert.equal(option.grid.bottom, 112)
  const plain = buildChartOption(rows, config)
  assert.equal(plain.dataZoom, undefined)
  assert.equal(plain.grid.bottom, 64)
})
