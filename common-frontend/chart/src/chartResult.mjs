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

export function buildChartOption(rows, config) {
  const labels = rows.map((row) => String(row?.[config.dimension] ?? ''))
  if (config.chart_type === 'pie') {
    const measure = config.measures[0]
    return {
      tooltip: { trigger: 'item' },
      legend: { type: 'scroll', bottom: 0 },
      series: [{
        type: 'pie',
        radius: ['35%', '70%'],
        data: rows.map((row, index) => ({ name: labels[index], value: Number(row[measure]) }))
      }]
    }
  }
  return {
    tooltip: { trigger: 'axis' },
    legend: { type: 'scroll', bottom: 0 },
    grid: { left: 24, right: 24, top: 32, bottom: 64, containLabel: true },
    xAxis: { type: 'category', data: labels },
    yAxis: { type: 'value' },
    series: config.measures.map((measure) => ({
      name: measure,
      type: config.chart_type,
      data: rows.map((row) => Number(row?.[measure]))
    }))
  }
}
