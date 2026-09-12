import { computed, nextTick, reactive, ref, unref } from 'vue'
import { createRequiredRule } from '../utils/foundationForm.mjs'
import { buildFindingReviewPayload } from '../utils/protectionEnrollment.mjs'

export function useProtectionFindingReview({
  sensitiveTypes,
  loadDefinitions,
  reviewFinding,
  resolveContext,
  continueAfterReview,
  t,
  onDefinitionsError = () => {},
  onSubmitError = () => {},
  onContinuationError = () => {},
  onSaved = () => {},
  onContinued = () => {}
}) {
  const dialog = ref(false)
  const formRef = ref(null)
  const finding = ref(null)
  const saving = ref(false)
  const basisExpanded = ref([])
  const form = reactive({ decision: 'confirm', sensitiveDataTypeID: '', securityGradeID: '', rationale: '' })

  let context = 'resource'
  let openRequest = 0
  let submitRequest = 0
  let disposed = false

  function requiredFieldRule(labelKey, options = {}) {
    return createRequiredRule(t('security.common.requiredField', { name: t(labelKey) }), options)
  }

  const rules = computed(() => ({
    decision: [requiredFieldRule('security.finding.decision')],
    sensitiveDataTypeID: [requiredFieldRule('security.finding.sensitiveDataType')],
    securityGradeID: [requiredFieldRule('security.finding.securityGrade')],
    rationale: [requiredFieldRule('security.finding.rationale', { trigger: 'blur', whitespace: true })]
  }))
  const rationalePlaceholder = computed(() => t(`security.finding.rationalePlaceholders.${form.decision}`))

  function defaultGradeID(typeID) {
    const selectedType = unref(sensitiveTypes).find(item => String(item.id) === String(typeID))
    return String(selectedType?.default_security_grade_id || '')
  }

  function focusRationale() {
    nextTick(() => {
      formRef.value?.focusRationale?.()
    })
  }

  function prepare(nextFinding, initialDecision = 'confirm', reviewContext = context) {
    finding.value = nextFinding
    basisExpanded.value = []
    form.decision = initialDecision
    form.sensitiveDataTypeID = String(nextFinding.sensitive_data_type_id)
    form.securityGradeID = defaultGradeID(nextFinding.sensitive_data_type_id)
    form.rationale = ''
    context = reviewContext
    dialog.value = true
    focusRationale()
  }

  async function open(nextFinding, initialDecision = 'confirm') {
    if (disposed || !nextFinding?.id) return false
    const request = ++openRequest
    const reviewContext = resolveContext()
    try {
      await loadDefinitions()
      if (request !== openRequest || disposed) return false
      prepare(nextFinding, initialDecision, reviewContext)
      return true
    } catch (error) {
      if (request === openRequest && !disposed) onDefinitionsError(error)
      return false
    }
  }

  function applyDefaultGrade(typeID) {
    form.securityGradeID = defaultGradeID(typeID)
  }

  function clearSession() {
    dialog.value = false
    finding.value = null
    basisExpanded.value = []
    form.decision = 'confirm'
    form.sensitiveDataTypeID = ''
    form.securityGradeID = ''
    form.rationale = ''
  }

  function close() {
    openRequest += 1
    submitRequest += 1
    saving.value = false
    clearSession()
  }

  async function submit() {
    if (disposed || saving.value || !finding.value?.id) return { status: 'unavailable' }
    const request = ++submitRequest
    const reviewedFinding = finding.value
    const reviewContext = context
    saving.value = true
    const validation = formRef.value?.validate?.()
    const valid = await Promise.resolve(validation).catch(() => false)
    if (request !== submitRequest || disposed) return { status: 'stale' }
    if (!valid) {
      saving.value = false
      return { status: 'invalid' }
    }
    try {
      await reviewFinding(reviewedFinding.id, buildFindingReviewPayload(form))
    } catch (error) {
      if (request === submitRequest && !disposed) onSubmitError(error)
      if (request === submitRequest) saving.value = false
      return { status: request === submitRequest && !disposed ? 'failed' : 'stale' }
    }
    if (request !== submitRequest || disposed) return { status: 'stale' }

    try {
      const nextFinding = await continueAfterReview(reviewedFinding, reviewContext)
      if (request !== submitRequest || disposed) return { status: 'stale' }
      if (nextFinding) {
        prepare(nextFinding, 'confirm', reviewContext)
        onContinued()
        return { status: 'continued', finding: nextFinding }
      }
      clearSession()
      onSaved()
      return { status: 'saved' }
    } catch (error) {
      if (request !== submitRequest || disposed) return { status: 'stale' }
      clearSession()
      onContinuationError(error)
      return { status: 'continuation_failed', error }
    } finally {
      if (request === submitRequest) saving.value = false
    }
  }

  function dispose() {
    disposed = true
    openRequest += 1
    submitRequest += 1
    saving.value = false
  }

  return {
    dialog,
    formRef,
    finding,
    saving,
    basisExpanded,
    form,
    rules,
    rationalePlaceholder,
    open,
    close,
    focusRationale,
    applyDefaultGrade,
    submit,
    dispose
  }
}
