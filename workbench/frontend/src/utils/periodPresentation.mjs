import { formatFieldPresentationValue } from '../../../../common-frontend/basic/src/utils/fieldPresentation.mjs'

// Parameters belong to the successful query, never the editable form's current values.
export function resolvePeriodPresentation(config, parameters, locale, t) {
  const summaries = new Set()
  const fieldPresentations = (config.field_presentations || []).map(presentation => {
    if (presentation.temporal_format !== 'period') return presentation
    const binding = presentation.period || {}
    const grain = parameters?.[binding.grain_parameter]
    const start = parameters?.[binding.start_parameter]
    const end = parameters?.[binding.end_parameter]
    const ready = ['total', 'month'].includes(grain) && typeof start === 'string' && typeof end === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(start) && /^\d{4}-\d{2}-\d{2}$/.test(end) && start < end
    if (ready) {
      const format = value => formatFieldPresentationValue(value, { temporal_format: 'date' }, locale)
      summaries.add(t('workbench.periodRange', { start: format(start), end: format(end) }))
    }
    return {
      ...presentation,
      period_context: ready ? { grain, total_label: t('workbench.periodTotal') } : null,
    }
  })
  return { config: { ...config, field_presentations: fieldPresentations }, summaries: [...summaries] }
}

// Only explicit display intent and the completed query may switch renderers.
export function periodValueConfig(config) {
  const dimension = config.field_presentations?.find(item => item.field === config.dimension)
  if (config.total_as_value !== true || dimension?.temporal_format !== 'period' || dimension.period_context?.grain !== 'total') return null
  return { items: (config.measures || []).map(field => {
    const presentation = config.field_presentations?.find(item => item.field === field) || {}
    return { field, label: presentation.label, unit: presentation.unit, precision: presentation.precision, state_rules: presentation.state_rules }
  }) }
}
