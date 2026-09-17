import { fieldPresentationFor, fieldPresentationLabel, formatFieldPresentationValue, formatFieldPresentationValueWithState } from '../../basic/src/utils/fieldPresentation.mjs'

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

export function buildChartOption(rows, config, locale = 'zh-CN', options = {}) {
  const presentations = config.field_presentations || []
  const dimensionPresentation = fieldPresentationFor(config.dimension, presentations)
  const labels = rows.map((row) => formatFieldPresentationValue(row?.[config.dimension], dimensionPresentation, locale, ''))
  const selection = chartSelectionOption(options)
  const textStyle = { color: options.textColor }
  const tooltipStyle = { textStyle, backgroundColor: options.backgroundColor, borderColor: options.borderColor }
  const legend = { type: 'scroll', bottom: 0, textStyle, pageTextStyle: textStyle, pageIconColor: options.textColor }
  const axisStyle = {
    axisLabel: { color: options.secondaryTextColor },
    nameTextStyle: { color: options.secondaryTextColor },
    axisLine: { lineStyle: { color: options.borderColor } },
    axisTick: { lineStyle: { color: options.borderColor } },
  }
  if (config.chart_type === 'pie') {
    const measure = config.measures[0]
    const measurePresentation = fieldPresentationFor(measure, presentations)
    return {
      textStyle,
      tooltip: { ...tooltipStyle, trigger: 'item' },
      legend,
      series: [{
        name: fieldPresentationLabel(measure, presentations),
        type: 'pie',
        radius: ['35%', '70%'],
        ...selection,
        label: textStyle,
        data: rows.map((row, index) => ({ name: labels[index], value: Number(row[measure]) })),
        tooltip: { valueFormatter: (value) => formatFieldPresentationValueWithState(value, measurePresentation, locale) },
      }]
    }
  }
  const primaryMeasurePresentation = config.measures.length === 1 ? fieldPresentationFor(config.measures[0], presentations) : null
  const primaryMeasureAxisPresentation = primaryMeasurePresentation
    ? { ...primaryMeasurePresentation, unit: '' }
    : null
  return {
    textStyle,
    tooltip: { ...tooltipStyle, trigger: 'axis' },
    legend,
    grid: { left: 24, right: 24, top: 32, bottom: 64, containLabel: true },
    xAxis: {
      ...axisStyle,
      type: 'category',
      name: fieldPresentationLabel(config.dimension, presentations),
      nameLocation: 'middle',
      nameGap: 32,
      data: labels,
    },
    yAxis: {
      ...axisStyle,
      type: 'value',
      nameTextStyle: { ...axisStyle.nameTextStyle, align: 'left' },
      splitLine: { lineStyle: { color: options.splitLineColor } },
      ...(Number.isInteger(primaryMeasurePresentation?.precision)
        ? { minInterval: 10 ** -primaryMeasurePresentation.precision }
        : {}),
      ...(config.measures.length === 1 ? {
        name: axisTitle(config.measures[0], presentations, primaryMeasurePresentation, locale),
        axisLabel: { ...axisStyle.axisLabel, formatter: (value) => formatFieldPresentationValue(value, primaryMeasureAxisPresentation, locale) },
      } : {}),
    },
    series: config.measures.map((measure) => {
      const presentation = fieldPresentationFor(measure, presentations)
      return {
        name: fieldPresentationLabel(measure, presentations),
        type: config.chart_type,
        ...selection,
        data: rows.map((row) => Number(row?.[measure])),
        tooltip: { valueFormatter: (value) => formatFieldPresentationValueWithState(value, presentation, locale) },
      }
    })
  }
}

function chartSelectionOption(options) {
  const selectionColor = String(options?.selectionColor || '').trim()
  return {
    selectedMode: 'single',
    ...(selectionColor ? { select: { itemStyle: { color: selectionColor } } } : {}),
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
