const cleanString = (value) => String(value || '').trim()

const sleep = (ms) => new Promise(resolve => setTimeout(resolve, Math.max(0, Number(ms || 0))))

const executionPayload = (response) => {
  const payload = response?.data || response || {}
  return payload?.data || payload
}

const terminalStatuses = new Set(['success', 'failed', 'timeout', 'cancelled', 'canceled'])
const failedStatuses = new Set(['failed', 'timeout', 'cancelled', 'canceled'])

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
