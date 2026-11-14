export const ALERT_RULE_TYPES = [
  'last_terminal_failed',
  'last_terminal_timeout',
  'consecutive_failures'
]

export const CDC_RULE_TYPES = ['cdc_consumer_stalled', 'cdc_capture_delayed', 'cdc_backlog_high']

export function alertRuleTargetKey(target) {
  return [target.module, target.task_type, target.source_task_id].join('\u0000')
}

export function alertRuleRouteKey(route) {
  return `${route.channel}:${route.destination_id}`
}

export function parseAlertRuleRouteKey(value) {
  const separator = value.indexOf(':')
  return {
    channel: value.slice(0, separator),
    destination_id: Number(value.slice(separator + 1))
  }
}

export function buildAlertRulePayload(form, target) {
  return {
    name: form.name.trim(),
    module: target.module,
    task_type: target.task_type,
    source_task_id: target.source_task_id,
    source_task_name: target.source_task_name || '',
    rule_type: form.rule_type,
    failure_threshold: form.rule_type === 'consecutive_failures' ? form.failure_threshold : 1,
    ...(CDC_RULE_TYPES.includes(form.rule_type) ? { backlog_threshold: form.rule_type === 'cdc_backlog_high' ? form.backlog_threshold : '' } : {}),
    severity: CDC_RULE_TYPES.includes(form.rule_type) ? 'warning' : form.severity,
    enabled: form.enabled,
    routes: form.routes.map(parseAlertRuleRouteKey)
  }
}
