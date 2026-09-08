const targetAssessmentId = target => String(target?.assessment_id || '').trim()

export const hasAccessGrantActivated = (previousTargets = [], nextTargets = []) => {
  const pendingAssessmentIds = new Set(
    previousTargets
      .filter(target => target?.access_request?.state === 'pending' && !target?.active_exemption_id)
      .map(targetAssessmentId)
      .filter(Boolean)
  )

  return nextTargets.some(target => (
    Boolean(target?.active_exemption_id) && pendingAssessmentIds.has(targetAssessmentId(target))
  ))
}
