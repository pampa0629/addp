const cleanString = (value) => String(value || '').trim()

const positiveNumber = (value) => {
  const parsed = Number(value || 0)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
}

const cleanExtent = (value) => {
  if (!Array.isArray(value) || value.length !== 4) return []
  const extent = value.map(Number)
  return extent.every(Number.isFinite) ? extent : []
}

const firstExtent = (...values) => {
  for (const value of values) {
    const extent = cleanExtent(value)
    if (extent.length === 4) return extent
  }
  return []
}

const sleep = (ms) => new Promise(resolve => setTimeout(resolve, Math.max(0, Number(ms || 0))))

const executionPayload = (response) => {
  const payload = response?.data || response || {}
  return payload?.data || payload
}

const terminalStatuses = new Set(['success', 'failed', 'timeout', 'cancelled', 'canceled'])
const failedStatuses = new Set(['failed', 'timeout', 'cancelled', 'canceled'])

export function shouldShowRasterCOGGenerationAction(status = {}) {
  if (!status || status.can_use_quick_view) return false
  const reason = cleanString(status.unavailable_reason)
  const rasterAction = cleanString(status.raster?.recommended_action)
  return [
    'requires_cog_generation',
    'requires_managed_cog',
    'client_render_budget_exceeded'
  ].includes(reason) || ['create_cog', 'create_managed_cog'].includes(rasterAction)
}

export function buildRasterCOGTaskPayload(target = {}, status = {}, name = '') {
  const raster = status?.raster || {}
  const quickView = status?.quick_view || {}
  const sourceEngineID = positiveNumber(status?.source_engine_id || target.engineId)
  const locator = cleanString(status?.locator || target.locator)
  const itemID = positiveNumber(target.itemID || status?.item_id)
  const itemFingerprint = cleanString(status?.item_fingerprint || target.itemFingerprint)
  const extent = firstExtent(raster.extent, quickView.extent, target.extent)
  const sourceSRID = positiveNumber(raster.source_srid || quickView.source_srid || target.sourceSRID)
  const extentSRID = positiveNumber(raster.extent_srid || quickView.extent_srid || target.extentSRID || sourceSRID)

  return {
    name: cleanString(name) || 'Generate COG',
    enabled: true,
    config: {
      target: {
        source_engine_id: sourceEngineID,
        locator,
        ...(itemID ? { item_id: itemID } : {}),
        ...(itemFingerprint ? { item_fingerprint: itemFingerprint } : {})
      },
      raster: {
        source_profile: cleanString(raster.profile) || 'unknown',
        source_size_bytes: positiveNumber(raster.size_bytes),
        width: positiveNumber(raster.width),
        height: positiveNumber(raster.height),
        band_count: positiveNumber(raster.band_count),
        ...(sourceSRID ? { source_srid: sourceSRID } : {}),
        ...(cleanString(raster.source_crs) ? { source_crs: cleanString(raster.source_crs) } : {}),
        ...(extent.length === 4 ? { extent } : {}),
        ...(extentSRID ? { extent_srid: extentSRID } : {})
      },
      cog: {
        compression: 'DEFLATE',
        blocksize: 512,
        overview_resampling: 'NEAREST'
      }
    }
  }
}

export async function waitForRasterCOGExecution(executionID, fetchExecutionStatus, options = {}) {
  const id = cleanString(executionID)
  if (!id || typeof fetchExecutionStatus !== 'function') {
    return { completed: false, success: false, status: '' }
  }

  const maxAttempts = Math.max(1, Number(options.maxAttempts || 45))
  const intervalMs = Math.max(0, Number(options.intervalMs ?? 2000))
  const initialDelayMs = Math.max(0, Number(options.initialDelayMs ?? intervalMs))

  if (initialDelayMs > 0) {
    await sleep(initialDelayMs)
  }

  let lastExecution = null
  for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
    const execution = executionPayload(await fetchExecutionStatus(id))
    lastExecution = execution
    const status = cleanString(execution?.status).toLowerCase()
    if (terminalStatuses.has(status)) {
      return {
        completed: true,
        success: status === 'success',
        failed: failedStatuses.has(status),
        status,
        execution
      }
    }
    if (attempt < maxAttempts) {
      await sleep(intervalMs)
    }
  }

  return {
    completed: false,
    success: false,
    failed: false,
    status: cleanString(lastExecution?.status).toLowerCase(),
    execution: lastExecution
  }
}
