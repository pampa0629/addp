import { computed, nextTick, reactive, ref } from 'vue'
import { createRequiredRule } from '../utils/foundationForm.mjs'
import { buildAssessmentRevisionPayload, isResourceVersionConflict } from '../utils/protectionEnrollment.mjs'

function snapshotAssessment(assessment) {
  if (!assessment) return null
  return {
    ...assessment,
    current: assessment.current ? { ...assessment.current } : null
  }
}

export function useProtectionAssessmentChange({
  loadDefinitions,
  activeGradesForType,
  defaultGradeID,
  getAssessment,
  reviseAssessment,
  revokeAssessment,
  replaceAssessment,
  refreshCollection,
  refreshGovernance,
  scheduleAutoRefresh,
  t,
  onDefinitionsError = () => {},
  onRevisionReloaded = () => {},
  onRevokeReloaded = () => {},
  onAlreadyRevoked = () => {},
  onLoadError = () => {},
  onRevised = () => {},
  onRevoked = () => {},
  onRevisionConflict = () => {},
  onRevokeConflict = () => {},
  onSubmitError = () => {}
}) {
  const revisionDialog = ref(false)
  const revisionFormRef = ref(null)
  const revisionRationaleInput = ref(null)
  const revisionSaving = ref(false)
  const revisionReloading = ref(false)
  const revisionConflict = ref(false)
  const revisionAssessment = ref(null)
  const revisionForm = reactive({ sensitiveDataTypeID: '', securityGradeID: '', rationale: '' })

  const revokeDialog = ref(false)
  const revokeFormRef = ref(null)
  const revokeCancelButton = ref(null)
  const revokeSaving = ref(false)
  const revokeReloading = ref(false)
  const revokeConflict = ref(false)
  const revokeAssessmentTarget = ref(null)
  const revokeForm = reactive({ rationale: '' })

  let revisionReloadRequest = 0
  let revisionSubmitRequest = 0
  let revokeReloadRequest = 0
  let revokeSubmitRequest = 0
  let disposed = false

  function requiredFieldRule(labelKey, options = {}) {
    return createRequiredRule(t('security.common.requiredField', { name: t(labelKey) }), options)
  }

  const revisionRules = computed(() => ({
    sensitiveDataTypeID: [requiredFieldRule('security.finding.sensitiveDataType')],
    securityGradeID: [requiredFieldRule('security.finding.securityGrade')],
    rationale: [requiredFieldRule('security.assessment.revisionRationale', { trigger: 'blur', whitespace: true })]
  }))
  const revokeRules = computed(() => ({
    rationale: [requiredFieldRule('security.assessment.revokeRationale', { trigger: 'blur', whitespace: true })]
  }))

  function applyRevisionDefaultGrade(typeID) {
    revisionForm.securityGradeID = String(defaultGradeID(typeID) || '')
  }

  function applyRevisionBaseline(assessment) {
    revisionAssessment.value = snapshotAssessment(assessment)
    revisionForm.sensitiveDataTypeID = String(assessment?.current?.sensitive_data_type_id || '')
    revisionForm.securityGradeID = String(assessment?.current?.security_grade_id || '')
    revisionForm.rationale = ''
    const activeGrades = activeGradesForType(revisionForm.sensitiveDataTypeID)
    if (!activeGrades.some(item => String(item.id) === revisionForm.securityGradeID)) {
      applyRevisionDefaultGrade(revisionForm.sensitiveDataTypeID)
    }
  }

  function clearRevisionSession() {
    revisionDialog.value = false
    revisionAssessment.value = null
    revisionForm.sensitiveDataTypeID = ''
    revisionForm.securityGradeID = ''
    revisionForm.rationale = ''
    revisionSaving.value = false
    revisionReloading.value = false
    revisionConflict.value = false
  }

  async function openRevision(assessment) {
    if (disposed || !assessment?.id) return { status: 'unavailable' }
    const request = ++revisionReloadRequest
    const source = snapshotAssessment(assessment)
    revisionSubmitRequest += 1
    clearRevisionSession()
    try {
      await loadDefinitions()
      if (request !== revisionReloadRequest || disposed) return { status: 'stale' }
      applyRevisionBaseline(source)
      revisionDialog.value = true
      return { status: 'opened' }
    } catch (error) {
      if (request !== revisionReloadRequest || disposed) return { status: 'stale' }
      onDefinitionsError(error)
      return { status: 'failed', error }
    }
  }

  function closeRevision() {
    revisionReloadRequest += 1
    revisionSubmitRequest += 1
    clearRevisionSession()
  }

  function focusRevisionRationale() {
    nextTick(() => {
      revisionFormRef.value?.clearValidate?.()
      revisionRationaleInput.value?.focus?.()
    })
  }

  async function reloadRevisionBaseline() {
    const assessment = revisionAssessment.value
    if (disposed || revisionReloading.value || revisionSaving.value || !assessment?.id) {
      return { status: 'unavailable' }
    }
    const request = ++revisionReloadRequest
    revisionReloading.value = true
    try {
      const latest = await getAssessment(assessment.id)
      if (request !== revisionReloadRequest || disposed) return { status: 'stale' }
      replaceAssessment(latest)
      applyRevisionBaseline(latest)
      revisionConflict.value = false
      onRevisionReloaded()
      focusRevisionRationale()
      return { status: 'reloaded', assessment: latest }
    } catch (error) {
      if (request !== revisionReloadRequest || disposed) return { status: 'stale' }
      onLoadError(error)
      return { status: 'failed', error }
    } finally {
      if (request === revisionReloadRequest) revisionReloading.value = false
    }
  }

  async function submitRevision() {
    if (disposed || revisionSaving.value || revisionReloading.value || revisionConflict.value || !revisionAssessment.value?.id) {
      return { status: 'unavailable' }
    }
    const request = ++revisionSubmitRequest
    const assessment = revisionAssessment.value
    revisionSaving.value = true
    const validation = revisionFormRef.value?.validate?.()
    const valid = await Promise.resolve(validation).catch(() => false)
    if (request !== revisionSubmitRequest || disposed) return { status: 'stale' }
    if (!valid) {
      revisionSaving.value = false
      return { status: 'invalid' }
    }

    try {
      const revised = await reviseAssessment(assessment.id, buildAssessmentRevisionPayload({
        version: assessment.version,
        ...revisionForm
      }))
      if (request !== revisionSubmitRequest || disposed) return { status: 'stale' }
      replaceAssessment(revised)
      clearRevisionSession()
      await refreshCollection({ background: true })
      await refreshGovernance()
      if (disposed) return { status: 'stale' }
      scheduleAutoRefresh({ reset: true })
      onRevised()
      return { status: 'revised', assessment: revised }
    } catch (error) {
      if (request !== revisionSubmitRequest || disposed) return { status: 'stale' }
      if (isResourceVersionConflict(error)) {
        revisionConflict.value = true
        onRevisionConflict()
        return { status: 'version_conflict', error }
      }
      onSubmitError(error)
      return { status: 'failed', error }
    } finally {
      if (request === revisionSubmitRequest) revisionSaving.value = false
    }
  }

  function clearRevokeSession() {
    revokeDialog.value = false
    revokeAssessmentTarget.value = null
    revokeForm.rationale = ''
    revokeSaving.value = false
    revokeReloading.value = false
    revokeConflict.value = false
  }

  function openRevoke(assessment) {
    if (disposed || !assessment?.id || assessment.current?.conclusion !== 'sensitive') return false
    revokeReloadRequest += 1
    revokeSubmitRequest += 1
    clearRevokeSession()
    revokeAssessmentTarget.value = snapshotAssessment(assessment)
    revokeDialog.value = true
    return true
  }

  function closeRevoke() {
    revokeReloadRequest += 1
    revokeSubmitRequest += 1
    clearRevokeSession()
  }

  function focusRevokeCancel() {
    nextTick(() => {
      revokeFormRef.value?.clearValidate?.()
      const button = revokeCancelButton.value?.$el || revokeCancelButton.value
      button?.focus?.()
    })
  }

  async function reloadRevokeBaseline() {
    const assessment = revokeAssessmentTarget.value
    if (disposed || revokeReloading.value || revokeSaving.value || !assessment?.id) {
      return { status: 'unavailable' }
    }
    const request = ++revokeReloadRequest
    revokeReloading.value = true
    try {
      const latest = await getAssessment(assessment.id)
      if (request !== revokeReloadRequest || disposed) return { status: 'stale' }
      replaceAssessment(latest)
      if (latest?.current?.conclusion !== 'sensitive') {
        clearRevokeSession()
        onAlreadyRevoked()
        return { status: 'already_revoked', assessment: latest }
      }
      revokeAssessmentTarget.value = snapshotAssessment(latest)
      revokeForm.rationale = ''
      revokeConflict.value = false
      onRevokeReloaded()
      focusRevokeCancel()
      return { status: 'reloaded', assessment: latest }
    } catch (error) {
      if (request !== revokeReloadRequest || disposed) return { status: 'stale' }
      onLoadError(error)
      return { status: 'failed', error }
    } finally {
      if (request === revokeReloadRequest) revokeReloading.value = false
    }
  }

  async function submitRevoke() {
    const assessment = revokeAssessmentTarget.value
    if (disposed || revokeSaving.value || revokeReloading.value || revokeConflict.value || !assessment?.id || assessment.current?.conclusion !== 'sensitive') {
      return { status: 'unavailable' }
    }
    const request = ++revokeSubmitRequest
    revokeSaving.value = true
    const validation = revokeFormRef.value?.validate?.()
    const valid = await Promise.resolve(validation).catch(() => false)
    if (request !== revokeSubmitRequest || disposed) return { status: 'stale' }
    if (!valid) {
      revokeSaving.value = false
      return { status: 'invalid' }
    }

    try {
      await revokeAssessment(assessment.id, {
        version: Number(assessment.version),
        rationale: revokeForm.rationale.trim()
      })
      if (request !== revokeSubmitRequest || disposed) return { status: 'stale' }
      clearRevokeSession()
      await refreshCollection({ background: true })
      await refreshGovernance()
      if (disposed) return { status: 'stale' }
      scheduleAutoRefresh({ reset: true })
      onRevoked()
      return { status: 'revoked' }
    } catch (error) {
      if (request !== revokeSubmitRequest || disposed) return { status: 'stale' }
      if (isResourceVersionConflict(error)) {
        revokeConflict.value = true
        onRevokeConflict()
        return { status: 'version_conflict', error }
      }
      onSubmitError(error)
      return { status: 'failed', error }
    } finally {
      if (request === revokeSubmitRequest) revokeSaving.value = false
    }
  }

  function dispose() {
    disposed = true
    revisionReloadRequest += 1
    revisionSubmitRequest += 1
    revokeReloadRequest += 1
    revokeSubmitRequest += 1
    revisionSaving.value = false
    revisionReloading.value = false
    revokeSaving.value = false
    revokeReloading.value = false
  }

  return {
    revisionDialog,
    revisionFormRef,
    revisionRationaleInput,
    revisionSaving,
    revisionReloading,
    revisionConflict,
    revisionAssessment,
    revisionForm,
    revisionRules,
    openRevision,
    closeRevision,
    focusRevisionRationale,
    applyRevisionDefaultGrade,
    reloadRevisionBaseline,
    submitRevision,
    revokeDialog,
    revokeFormRef,
    revokeCancelButton,
    revokeSaving,
    revokeReloading,
    revokeConflict,
    revokeAssessment: revokeAssessmentTarget,
    revokeForm,
    revokeRules,
    openRevoke,
    closeRevoke,
    focusRevokeCancel,
    reloadRevokeBaseline,
    submitRevoke,
    dispose
  }
}
