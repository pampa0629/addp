import { fieldPresentationFor, fieldPresentationLabel, formatFieldPresentationValue } from '../../basic/src/utils/fieldPresentation.mjs'

const CHART_TYPES = new Set(['bar', 'line', 'pie'])

export function validateChartResult(rows, config, hasMore = false) {
  if (hasMore) return { valid: false, reason: 'partial_result' }
  if (!Array.isArray(rows) || !config || !CHART_TYPES.has(config.chart_type)) {
    return { valid: false, reason: 'invalid_config' }
  }
  const limit = config.chart_type === 'pie' ? 20 : 500
  if (rows.length > limit) return { valid: false, reason: 'result_limit' }
  if (!config.dimension || !Array.isArray(config.measures) || config.measures.length === 0) {
    return { valid: false, reason: 'invalid_config' }
  }
  if (config.chart_type === 'pie' && config.measures.length !== 1) {
    return { valid: false, reason: 'invalid_config' }
  }
  for (const row of rows) {
    for (const measure of config.measures) {
      const value = Number(row?.[measure])
      if (!Number.isFinite(value) || (config.chart_type === 'pie' && value < 0)) {
        return { valid: false, reason: 'invalid_measure' }
      }
    }
  }
  return { valid: true, reason: '' }
}

export function buildChartOption(rows, config, locale = 'zh-CN') {
  const presentations = config.field_presentations || []
  const dimensionPresentation = fieldPresentationFor(config.dimension, presentations)
  const labels = rows.map((row) => formatFieldPresentationValue(row?.[config.dimension], dimensionPresentation, locale, ''))
  if (config.chart_type === 'pie') {
    const measure = config.measures[0]
    const measurePresentation = fieldPresentationFor(measure, presentations)
    return {
      tooltip: { trigger: 'item' },
      legend: { type: 'scroll', bottom: 0 },
      series: [{
        name: fieldPresentationLabel(measure, presentations),
        type: 'pie',
        radius: ['35%', '70%'],
        data: rows.map((row, index) => ({ name: labels[index], value: Number(row[measure]) })),
        tooltip: { valueFormatter: (value) => formatFieldPresentationValue(value, measurePresentation, locale) },
      }]
    }
  }
  const primaryMeasurePresentation = config.measures.length === 1 ? fieldPresentationFor(config.measures[0], presentations) : null
  const primaryMeasureAxisPresentation = primaryMeasurePresentation
    ? { ...primaryMeasurePresentation, unit: '' }
    : null
  return {
    tooltip: { trigger: 'axis' },
    legend: { type: 'scroll', bottom: 0 },
    grid: { left: 24, right: 24, top: 32, bottom: 64, containLabel: true },
    xAxis: {
      type: 'category',
      name: fieldPresentationLabel(config.dimension, presentations),
      nameLocation: 'middle',
      nameGap: 32,
      data: labels,
    },
    yAxis: {
      type: 'value',
      ...(config.measures.length === 1 ? {
        name: axisTitle(config.measures[0], presentations, primaryMeasurePresentation, locale),
        axisLabel: { formatter: (value) => formatFieldPresentationValue(value, primaryMeasureAxisPresentation, locale) },
      } : {}),
    },
    series: config.measures.map((measure) => {
      const presentation = fieldPresentationFor(measure, presentations)
      return {
        name: fieldPresentationLabel(measure, presentations),
        type: config.chart_type,
        data: rows.map((row) => Number(row?.[measure])),
        tooltip: { valueFormatter: (value) => formatFieldPresentationValue(value, presentation, locale) },
      }
    })
  }
}

function axisTitle(field, presentations, presentation, locale) {
  const label = fieldPresentationLabel(field, presentations)
  const unit = String(presentation?.unit || '').trim()
  if (!unit) return label
  return String(locale || '').toLowerCase().startsWith('zh')
    ? `${label}（${unit}）`
    : `${label} (${unit})`
}

export function resultSelectionFromChartEvent(event, rowCount) {
  const rowIndex = event?.dataIndex
  return Number.isInteger(rowIndex) && rowIndex >= 0 && rowIndex < rowCount
    ? { row_index: rowIndex }
    : null
}
