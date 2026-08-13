export function isDatabaseCDCTask(task) {
  const config = task?.config
  return config?.runtime?.boundary === 'continuous' &&
    config?.load?.mode === 'incremental' &&
    config?.load?.change_detection?.type === 'cdc' &&
    config?.load?.change_detection?.bootstrap === 'initial_snapshot'
}

export function isCDCSchemaBlocked(task) {
	return isDatabaseCDCTask(task) && task?.status === 'blocked'
}

export function continuousStartDisabledReason(task) {
	if (isCDCSchemaBlocked(task)) return 'schema_blocked'
	if (isDatabaseCDCTask(task)) {
		const captureStatus = String(task?.capture?.status || '').toLowerCase()
		if (captureStatus === 'cleanup_failed' || captureStatus === 'cleaning') return 'cleanup_failed'
		if (captureStatus === 'stopped') return 'permanently_stopped'
	}
	if (task?.status === 'running' || task?.desired_state === 'running') return 'running'
	if (!['paused', 'stopped'].includes(task?.desired_state)) return 'invalid_state'
	return null
}

export function getCDCCaptureHealthWarning(task) {
	if (!isDatabaseCDCTask(task) || !task?.capture) return null
	const status = String(task.capture.status || '').toLowerCase()
	const connectorStatus = String(task.capture.connector_status || '').toUpperCase()
	const sourceStatus = String(task.capture.source_status || '').toUpperCase()
	if (status === 'stopped') return null
	if (['failed', 'cleanup_failed'].includes(status) || (status === 'running' && ((connectorStatus && connectorStatus !== 'RUNNING') || (sourceStatus && sourceStatus !== 'ONLINE')))) {
		return { status, connectorStatus: connectorStatus || '-', sourceStatus: sourceStatus || '-' }
	}
	return null
}

export function getCDCSourceRecoveryWarning(task) {
	if (!isDatabaseCDCTask(task) || task?.capture?.source_recovery?.health !== 'critical') return null
	return {
		capturePosition: task.capture.source_recovery.capture_position || '-',
		earliestAvailablePosition: task.capture.source_recovery.earliest_available_position || '-'
	}
}

export function buildCDCStopRequest(taskName, confirmationText) {
  return {
    confirmed: true,
    confirmation_text: String(confirmationText ?? '')
  }
}
