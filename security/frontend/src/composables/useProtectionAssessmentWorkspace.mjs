import { computed, nextTick, reactive, ref } from 'vue'
import { createRequiredRule } from '../utils/foundationForm.mjs'

function snapshotEnrollment(enrollment) {
  return enrollment ? { ...enrollment } : null
}

export function useProtectionAssessmentWorkspace({
  getEnrollment,
  loadComponents,
  loadDefinitions,
  refreshAssessments,
  createAssessment,
  getAssessment,
  refreshCollection,
  refreshGovernance,
  scheduleAutoRefresh,
  defaultGradeID,
  t,
  onComponentsLoadError = () => {},
  onDesignationSaved = () => {},
  onDesignationError = () => {},
  onHistoryLoadError = () => {}
}) {
  const designationDialog = ref(false)
  const designationFormRef = ref(null)
  const designationRationaleInput = ref(null)
  const designationSaving = ref(false)
  const componentsLoading = ref(false)
  const componentOptions = ref([])
  const designationEnrollment = ref(null)
  const designationForm = reactive({ componentKey: '', sensitiveDataTypeID: '', securityGradeID: '', rationale: '' })

  const historyDialog = ref(false)
  const historyLoading = ref(false)
  const history = ref(null)

  let designationLoadRequest = 0
  let designationSubmitRequest = 0
  let historyRequest = 0
  let disposed = false

  function requiredFieldRule(labelKey, options = {}) {
    return createRequiredRule(t('security.common.requiredField', { name: t(labelKey) }), options)
  }

  const designationRules = computed(() => ({
    componentKey: [requiredFieldRule('security.assessment.component')],
    sensitiveDataTypeID: [requiredFieldRule('security.finding.sensitiveDataType')],
    securityGradeID: [requiredFieldRule('security.finding.securityGrade')],
    rationale: [requiredFieldRule('security.assessment.rationale', { trigger: 'blur', whitespace: true })]
  }))

  function resetDesignationForm() {
    designationForm.componentKey = ''
    designationForm.sensitiveDataTypeID = ''
    designationForm.securityGradeID = ''
    designationForm.rationale = ''
  }

  function clearDesignationSession() {
    designationDialog.value = false
    designationEnrollment.value = null
    componentOptions.value = []
    componentsLoading.value = false
    designationSaving.value = false
    resetDesignationForm()
  }

  async function openDesignation() {
    const enrollment = snapshotEnrollment(getEnrollment())
    if (disposed || !enrollment?.id) return { status: 'unavailable' }
    const request = ++designationLoadRequest
    designationSubmitRequest += 1
    clearDesignationSession()
    designationEnrollment.value = enrollment
    designationDialog.value = true
    componentsLoading.value = true
    try {
      const [response] = await Promise.all([
        loadComponents(enrollment.id),
        loadDefinitions(),
        refreshAssessments()
      ])
      if (request !== designationLoadRequest || disposed) return { status: 'stale' }
      componentOptions.value = Array.isArray(response?.data) ? response.data : []
      return { status: 'opened', components: componentOptions.value }
    } catch (error) {
      if (request !== designationLoadRequest || disposed) return { status: 'stale' }
      onComponentsLoadError(error)
      return { status: 'failed', error }
    } finally {
      if (request === designationLoadRequest) componentsLoading.value = false
    }
  }

  function closeDesignation() {
    designationLoadRequest += 1
    designationSubmitRequest += 1
    clearDesignationSession()
  }

  function focusDesignationRationale() {
    nextTick(() => {
      designationFormRef.value?.clearValidate?.()
      designationRationaleInput.value?.focus?.()
    })
  }

  function applyDesignationDefaultGrade(typeID) {
    designationForm.securityGradeID = String(defaultGradeID(typeID) || '')
  }

  async function submitDesignation() {
    const enrollment = designationEnrollment.value
    if (disposed || designationSaving.value || !enrollment?.id) {
      return { status: 'unavailable' }
    }
    const request = ++designationSubmitRequest
    designationSaving.value = true
    const validation = designationFormRef.value?.validate?.()
    const valid = await Promise.resolve(validation).catch(() => false)
    if (request !== designationSubmitRequest || disposed) return { status: 'stale' }
    if (!valid) {
      designationSaving.value = false
      return { status: 'invalid' }
    }

    try {
      const created = await createAssessment({
        enrollment_id: enrollment.id,
        enrollment_version: Number(enrollment.version),
        component_key: designationForm.componentKey,
        sensitive_data_type_id: Number(designationForm.sensitiveDataTypeID),
        security_grade_id: Number(designationForm.securityGradeID),
        rationale: designationForm.rationale.trim()
      })
      if (request !== designationSubmitRequest || disposed) return { status: 'stale' }
      clearDesignationSession()
      await refreshCollection({ background: true })
      await refreshGovernance()
      if (disposed) return { status: 'stale' }
      scheduleAutoRefresh({ reset: true })
      onDesignationSaved()
      return { status: 'designated', assessment: created }
    } catch (error) {
      if (request !== designationSubmitRequest || disposed) return { status: 'stale' }
      onDesignationError(error)
      return { status: 'failed', error }
    } finally {
      if (request === designationSubmitRequest) designationSaving.value = false
    }
  }

  async function openHistory(assessment) {
    if (disposed || !assessment?.id) return { status: 'unavailable' }
    const request = ++historyRequest
    history.value = null
    historyDialog.value = true
    historyLoading.value = true
    try {
      const detail = await getAssessment(assessment.id)
      if (request !== historyRequest || disposed) return { status: 'stale' }
      history.value = detail
      return { status: 'loaded', assessment: detail }
    } catch (error) {
      if (request !== historyRequest || disposed) return { status: 'stale' }
      historyDialog.value = false
      onHistoryLoadError(error)
      return { status: 'failed', error }
    } finally {
      if (request === historyRequest) historyLoading.value = false
    }
  }

  function closeHistory() {
    historyRequest += 1
    historyDialog.value = false
    history.value = null
    historyLoading.value = false
  }

  function isCurrentHistoryRevision(revision) {
    return Number(revision?.revision) === Number(history.value?.current_revision)
  }

  function dispose() {
    disposed = true
    designationLoadRequest += 1
    designationSubmitRequest += 1
    historyRequest += 1
    designationSaving.value = false
    componentsLoading.value = false
    historyLoading.value = false
  }

  return {
    designationDialog,
    designationFormRef,
    designationRationaleInput,
    designationSaving,
    componentsLoading,
    componentOptions,
    designationEnrollment,
    designationForm,
    designationRules,
    openDesignation,
    closeDesignation,
    focusDesignationRationale,
    applyDesignationDefaultGrade,
    submitDesignation,
    historyDialog,
    historyLoading,
    history,
    openHistory,
    closeHistory,
    isCurrentHistoryRevision,
    dispose
  }
}
