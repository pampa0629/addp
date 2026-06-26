const cleanString = (value) => String(value || '').trim()

const positiveNumber = (value) => {
  const parsed = Number(value || 0)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
}

export function rasterCOGExecutionStatusTagType(status) {
  const value = cleanString(status).toLowerCase()
  if (value === 'success') return 'success'
  if (['failed', 'timeout', 'cancelled', 'canceled'].includes(value)) return 'danger'
  if (['pending', 'running'].includes(value)) return 'warning'
  return 'info'
}

export function rasterCOGResultStatusTagType(status) {
  const value = cleanString(status).toLowerCase()
  if (value === 'ready') return 'success'
  if (value === 'failed') return 'danger'
  if (['building', 'stale'].includes(value)) return 'warning'
  return 'info'
}

export function rasterCOGLastExecutionStatus(task) {
  return cleanString(
    task?.last_execution_status ||
      task?.lastExecutionStatus ||
      task?.execution_status ||
      task?.last_execution?.status
  )
}

export function rasterCOGResourcePathFromLocator(locator, parseLocator) {
  const value = cleanString(locator)
  if (!value) return ''
  const parsed = parseLocator?.(value)
  if (Array.isArray(parsed?.path) && parsed.path.length > 0) {
    return parsed.path.join('/')
  }
  return value
}

export function rasterCOGTaskResource(task, parseLocator) {
  const target = task?.target || task?.config?.target || {}
  return rasterCOGResourcePathFromLocator(target.locator, parseLocator) || '-'
}

export function rasterCOGSourcePath(result, parseLocator) {
  return rasterCOGResourcePathFromLocator(result?.locator, parseLocator) || '-'
}

export function rasterCOGRasterSize(width, height) {
  const w = positiveNumber(width)
  const h = positiveNumber(height)
  return w && h ? `${w} x ${h}` : '-'
}

export function rasterCOGExtentLabel(extent) {
  if (!Array.isArray(extent) || extent.length !== 4) return '-'
  const values = extent.map(Number)
  if (!values.every(Number.isFinite)) return '-'
  return values.map((value) => Number(value.toFixed(6))).join(', ')
}
