const targetAssessmentId = target => String(target?.assessment_id || '').trim()

export const isAccessGrantSynchronizing = target => Boolean(target?.active_exemption_id) && target.projection_acknowledged === false

export const isAccessGrantSynchronized = target => Boolean(target?.active_exemption_id) && target.projection_acknowledged === true

export const hasAccessGrantSynchronized = (previousTargets = [], nextTargets = []) => {
  const waitingAssessmentIds = new Set(
    previousTargets
      .filter(target => (target?.access_request?.state === 'pending' && !target?.active_exemption_id) || isAccessGrantSynchronizing(target))
      .map(targetAssessmentId)
      .filter(Boolean)
  )

  return nextTargets.some(target => (
    isAccessGrantSynchronized(target) && waitingAssessmentIds.has(targetAssessmentId(target))
  ))
}
