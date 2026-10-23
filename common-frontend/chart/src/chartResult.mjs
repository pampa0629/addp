import { fieldPresentationFor, fieldPresentationLabel, formatFieldPresentationValue, formatFieldPresentationValueWithState } from '../../basic/src/utils/fieldPresentation.mjs'

const CHART_TYPES = new Set(['bar', 'line', 'pie'])

export function validateChartResult(rows, config, hasMore = false, valueAxes = [], maxRows) {
  if (hasMore) return { valid: false, reason: 'partial_result' }
  if (!Array.isArray(rows) || !config || !CHART_TYPES.has(config.chart_type)) {
    return { valid: false, reason: 'invalid_config' }
  }
  if (maxRows !== undefined && (config.chart_type === 'pie' || !Number.isSafeInteger(maxRows) || maxRows < 1 || maxRows > 1000)) return { valid: false, reason: 'invalid_config' }
  const limit = maxRows ?? (config.chart_type === 'pie' ? 20 : config.chart_type === 'line' ? 1000 : 500)
  if (rows.length > limit) return { valid: false, reason: 'result_limit' }
  if (!config.dimension || !Array.isArray(config.measures) || config.measures.length === 0) {
    return { valid: false, reason: 'invalid_config' }
  }
  if (config.chart_type === 'pie' && config.measures.length !== 1) {
    return { valid: false, reason: 'invalid_config' }
  }
  if (!validValueAxes(config, valueAxes)) return { valid: false, reason: 'invalid_config' }
  for (const row of rows) {
    for (const measure of config.measures) {
      if (row?.[measure] === null && config.chart_type !== 'pie') continue
      if (row?.[measure] === null) return { valid: false, reason: 'invalid_measure' }
      const value = Number(row?.[measure])
      if (!Number.isFinite(value) || (config.chart_type === 'pie' && value < 0)) {
        return { valid: false, reason: 'invalid_measure' }
      }
    }
  }
  return { valid: true, reason: '' }
}

function validValueAxes(config, axes) {
  if (!Array.isArray(axes) || axes.length > 2) return false
  if (!axes.length) return true
  if (config.chart_type === 'pie') return false
  const seen = new Set(), presentations = config.field_presentations || []
  for (const axis of axes) {
    if (!axis || Object.keys(axis).some(key => !['measures', 'label', 'min', 'max'].includes(key)) || !Array.isArray(axis.measures) || !axis.measures.length || (axis.label !== undefined && (typeof axis.label !== 'string' || !axis.label.trim()))) return false
    if (['min', 'max'].some(key => axis[key] !== undefined && !Number.isFinite(axis[key])) || (axis.min !== undefined && axis.max !== undefined && axis.min >= axis.max)) return false
    const first = fieldPresentationFor(axis.measures[0], presentations)
    for (const measure of axis.measures) {
      const presentation = fieldPresentationFor(measure, presentations)
      if (!config.measures.includes(measure) || seen.has(measure) || presentation?.unit !== first?.unit || presentation?.precision !== first?.precision) return false
      seen.add(measure)
    }
  }
  return seen.size === config.measures.length
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
        tooltip: { valueFormatter: (_value, dataIndex) => formatFieldPresentationValueWithState(rows[dataIndex]?.[measure], measurePresentation, locale) },
      }]
    }
  }
  const valueAxes = options.valueAxes || []
  const makeAxis = (measures, axis = {}, index = 0) => {
    const presentation = fieldPresentationFor(measures[0], presentations)
    const labeled = measures.length === 1 || Boolean(axis.label)
    return {
      ...axisStyle,
      type: 'value',
      nameTextStyle: { ...axisStyle.nameTextStyle, align: index ? 'right' : 'left' },
      splitLine: { show: index === 0, lineStyle: { color: options.splitLineColor } },
      ...(valueAxes.length ? { position: index ? 'right' : 'left' } : {}),
      ...(Number.isInteger(presentation?.precision) && labeled ? { minInterval: 10 ** -presentation.precision } : {}),
      ...(labeled ? {
        name: axisTitle(measures[0], axis.label ? [{ field: measures[0], label: axis.label }] : presentations, presentation, locale),
        axisLabel: { ...axisStyle.axisLabel, formatter: value => formatFieldPresentationValue(value, { ...presentation, unit: '' }, locale) },
      } : {}),
      ...(axis.min !== undefined ? { min: axis.min } : {}),
      ...(axis.max !== undefined ? { max: axis.max } : {}),
    }
  }
  return {
    textStyle,
    tooltip: { ...tooltipStyle, trigger: 'axis' },
    legend,
    grid: { left: 24, right: 24, top: 32, bottom: options.zoomable ? 112 : 64, containLabel: true },
    ...(options.zoomable ? { dataZoom: [{
      type: 'slider', filterMode: 'none', left: 24, right: 24, bottom: 32, height: 18, showDetail: false,
      start: options.zoomStart ?? 0, end: options.zoomEnd ?? 100,
      backgroundColor: options.backgroundColor, borderColor: options.borderColor, fillerColor: options.splitLineColor,
      handleStyle: { color: options.secondaryTextColor, borderColor: options.borderColor },
      moveHandleStyle: { color: options.secondaryTextColor },
      dataBackground: { lineStyle: { color: options.secondaryTextColor }, areaStyle: { color: options.backgroundColor } },
      selectedDataBackground: { lineStyle: { color: options.textColor }, areaStyle: { color: options.backgroundColor } },
    }] } : {}),
    xAxis: {
      ...axisStyle,
      type: 'category',
      name: fieldPresentationLabel(config.dimension, presentations),
      nameLocation: 'middle',
      nameGap: 32,
      data: labels,
    },
    yAxis: valueAxes.length ? valueAxes.map((axis, index) => makeAxis(axis.measures, axis, index)) : makeAxis(config.measures),
    series: config.measures.map((measure) => {
      const presentation = fieldPresentationFor(measure, presentations)
      return {
        name: fieldPresentationLabel(measure, presentations),
        type: config.chart_type,
        ...(valueAxes.length ? { yAxisIndex: valueAxes.findIndex(axis => axis.measures.includes(measure)) } : {}),
        ...selection,
        ...(config.chart_type === 'bar' ? {
          label: {
            show: options.showValueLabels !== false,
            position: 'top',
            ...textStyle,
            formatter: ({ dataIndex }) => formatFieldPresentationValue(rows[dataIndex]?.[measure], presentation, locale),
          },
        } : {}),
        connectNulls: false,
        data: rows.map((row) => row?.[measure] === null ? null : Number(row?.[measure])),
        tooltip: { valueFormatter: (_value, dataIndex) => formatFieldPresentationValueWithState(rows[dataIndex]?.[measure], presentation, locale) },
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
