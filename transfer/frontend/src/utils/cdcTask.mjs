export function isPostgreSQLCDCTask(task) {
  const config = task?.config
  return config?.runtime?.boundary === 'continuous' &&
    config?.load?.mode === 'incremental' &&
    config?.load?.change_detection?.type === 'cdc' &&
    config?.load?.change_detection?.bootstrap === 'initial_snapshot'
}

export function isCDCSchemaBlocked(task) {
	return isPostgreSQLCDCTask(task) && task?.status === 'blocked'
}

export function getCDCCaptureHealthWarning(task) {
	if (!isPostgreSQLCDCTask(task) || !task?.capture) return null
	const status = String(task.capture.status || '').toLowerCase()
	const connectorStatus = String(task.capture.connector_status || '').toUpperCase()
	if (status === 'stopped') return null
	if (['failed', 'cleanup_failed'].includes(status) || (status === 'running' && connectorStatus && connectorStatus !== 'RUNNING')) {
		return { status, connectorStatus: connectorStatus || '-' }
	}
	return null
}

export function buildCDCStopRequest(taskName, confirmationText) {
  return {
    confirmed: true,
    confirmation_text: String(confirmationText ?? '')
  }
}
