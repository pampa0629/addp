const REPORT_KEYS = ['status', 'observed_at', 'checked', 'not_checked', 'runtime_checks_required', 'diagnostics']
const DIAGNOSTIC_KEYS = ['status', 'code', 'check', 'config_path', 'source_field', 'target_field', 'physical_type']
const STATUSES = ['supported', 'unsupported', 'unknown']
const object = value => value !== null && typeof value === 'object' && !Array.isArray(value)
const text = (value, max, min = 0) => {
  if (typeof value !== 'string') return false
  const length = Array.from(value).length // Same Unicode code-point limits as JSON Schema.
  return length >= min && length <= max
}

// One bounded presentation contract for the wizard and A2UI; no type inference.
export function isTransferConfigCheckReport(value) {
  if (!object(value) || Object.keys(value).length !== REPORT_KEYS.length ||
      !REPORT_KEYS.every(key => Object.hasOwn(value, key)) ||
      !STATUSES.includes(value.status) || !text(value.observed_at, 64, 1)) return false
  for (const key of ['checked', 'not_checked', 'runtime_checks_required']) {
    if (!Array.isArray(value[key]) || value[key].length > 128 || !value[key].every(code => text(code, 256, 1))) return false
  }
  if (!Array.isArray(value.diagnostics) || value.diagnostics.length > 512 ||
      !value.diagnostics.every(item => object(item) && Object.keys(item).every(key => DIAGNOSTIC_KEYS.includes(key)) &&
        STATUSES.includes(item.status) && text(item.code, 256, 1) && text(item.check, 256, 1) && text(item.config_path, 4096) &&
        ['source_field', 'target_field', 'physical_type'].every(key => !Object.hasOwn(item, key) || text(item[key], 4096)))) return false
  try { return new TextEncoder().encode(JSON.stringify(value)).length <= 128 * 1024 } catch { return false }
}

export function projectTransferConfigCheckReport(report) {
  if (!object(report) || report.schema_version !== 'addp.transfer_task_config_check/v1') return null
  const projected = Object.fromEntries(REPORT_KEYS.map(key => [key, report[key]]))
  return isTransferConfigCheckReport(projected) ? JSON.parse(JSON.stringify(projected)) : null
}
