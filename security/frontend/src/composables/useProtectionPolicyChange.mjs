import { computed, nextTick, reactive, ref } from 'vue'
import { createRequiredRule } from '../utils/foundationForm.mjs'
import { isResourceVersionConflict } from '../utils/protectionEnrollment.mjs'

export function useProtectionPolicyChange({
  policyForAssessment,
  stricterEffects,
  getPolicy,
  getAssessment,
  listBaselines,
  applyLatestContext,
  createPolicy,
  updatePolicy,
  revokePolicy,
  refreshCollection,
  refreshGovernance,
  scheduleAutoRefresh,
  t,
  onAlreadyStrictest = () => {},
  onEditReloaded = () => {},
  onRestoreReloaded = () => {},
  onAlreadyRestored = () => {},
  onLoadError = () => {},
  onSaved = () => {},
  onRestored = () => {},
  onEditConflict = () => {},
  onRestoreConflict = () => {},
  onSubmitError = () => {}
}) {
  const editDialog = ref(false)
  const editFormRef = ref(null)
  const editSaving = ref(false)
  const editReloading = ref(false)
  const editConflict = ref(false)
  const editAssessment = ref(null)
  const editPolicy = ref(null)
  const editForm = reactive({ effect: '', rationale: '' })

  const restoreDialog = ref(false)
  const restoreFormRef = ref(null)
  const restoreCancelButton = ref(null)
  const restoreSaving = ref(false)
  const restoreReloading = ref(false)
  const restoreConflict = ref(false)
  const restoreAssessment = ref(null)
  const restorePolicy = ref(null)
  const restoreForm = reactive({ rationale: '' })

  let editReloadRequest = 0
  let editSubmitRequest = 0
  let restoreReloadRequest = 0
  let restoreSubmitRequest = 0
  let disposed = false

  function requiredFieldRule(labelKey, options = {}) {
    return createRequiredRule(t('security.common.requiredField', { name: t(labelKey) }), options)
  }

  const editRules = computed(() => ({
    effect: [requiredFieldRule('security.policy.effect')],
    rationale: [requiredFieldRule('security.policy.rationale', { trigger: 'blur', whitespace: true })]
  }))
  const restoreRules = computed(() => ({
    rationale: [requiredFieldRule('security.policy.restoreRationale', { trigger: 'blur', whitespace: true })]
  }))
  const editEffects = computed(() => editAssessment.value ? stricterEffects(editAssessment.value) : [])

  async function fetchLatestContext(assessment, policy) {
    const [latestPolicy, latestAssessment, latestBaselines] = await Promise.all([
      getPolicy(policy.id),
      getAssessment(assessment.id),
      listBaselines()
    ])
    return { policy: latestPolicy, assessment: latestAssessment, baselines: latestBaselines }
  }

  function applyEditBaseline(assessment, policy) {
    const effects = stricterEffects(assessment)
    if (!effects.length) {
      onAlreadyStrictest()
      return false
    }
    editAssessment.value = assessment
    editPolicy.value = policy || null
    editForm.effect = policy?.state === 'active' && effects.includes(policy.current?.effect)
      ? policy.current.effect
      : effects[0]
    editForm.rationale = policy?.state === 'active' ? String(policy.current?.rationale || '') : ''
    return true
  }

  function clearEditSession() {
    editDialog.value = false
    editAssessment.value = null
    editPolicy.value = null
    editForm.effect = ''
    editForm.rationale = ''
    editSaving.value = false
    editReloading.value = false
    editConflict.value = false
  }

  function openEdit(assessment) {
    if (disposed || !assessment?.id) return false
    editReloadRequest += 1
    editSubmitRequest += 1
    clearEditSession()
    if (!applyEditBaseline(assessment, policyForAssessment(assessment))) return false
    editDialog.value = true
    return true
  }

  function closeEdit() {
    editReloadRequest += 1
    editSubmitRequest += 1
    clearEditSession()
  }

  function clearEditValidation() {
    nextTick(() => editFormRef.value?.clearValidate?.())
  }

  async function reloadEditBaseline() {
    const assessment = editAssessment.value
    const policy = editPolicy.value
    if (disposed || editReloading.value || editSaving.value || !assessment?.id || !policy?.id) {
      return { status: 'unavailable' }
    }
    const request = ++editReloadRequest
    editReloading.value = true
    try {
      const context = await fetchLatestContext(assessment, policy)
      if (request !== editReloadRequest || disposed) return { status: 'stale' }
      applyLatestContext(context)
      if (!applyEditBaseline(context.assessment, context.policy)) {
        clearEditSession()
        return { status: 'already_strictest', context }
      }
      editConflict.value = false
      onEditReloaded()
      return { status: 'reloaded', context }
    } catch (error) {
      if (request !== editReloadRequest || disposed) return { status: 'stale' }
      onLoadError(error)
      return { status: 'failed', error }
    } finally {
      if (request === editReloadRequest) editReloading.value = false
    }
  }

  async function submitEdit() {
    if (disposed || editSaving.value || editReloading.value || editConflict.value || !editAssessment.value?.id) {
      return { status: 'unavailable' }
    }
    const request = ++editSubmitRequest
    editSaving.value = true
    const validation = editFormRef.value?.validate?.()
    const valid = await Promise.resolve(validation).catch(() => false)
    if (request !== editSubmitRequest || disposed) return { status: 'stale' }
    if (!valid) {
      editSaving.value = false
      return { status: 'invalid' }
    }

    const assessment = editAssessment.value
    const existingPolicy = editPolicy.value
    try {
      if (existingPolicy) {
        await updatePolicy(existingPolicy.id, {
          version: Number(existingPolicy.version),
          effect: editForm.effect,
          rationale: editForm.rationale.trim()
        })
      } else {
        await createPolicy({
          assessment_id: assessment.id,
          consumer_owner: 'manager',
          action: 'preview',
          effect: editForm.effect,
          rationale: editForm.rationale.trim()
        })
      }
      if (request !== editSubmitRequest || disposed) return { status: 'stale' }
      clearEditSession()
      await refreshCollection({ background: true })
      await refreshGovernance()
      if (disposed) return { status: 'stale' }
      scheduleAutoRefresh({ reset: true })
      onSaved()
      return { status: 'saved', operation: existingPolicy ? 'updated' : 'created' }
    } catch (error) {
      if (request !== editSubmitRequest || disposed) return { status: 'stale' }
      if (existingPolicy && isResourceVersionConflict(error)) {
        editConflict.value = true
        onEditConflict()
        return { status: 'version_conflict', error }
      }
      onSubmitError(error)
      return { status: 'failed', error }
    } finally {
      if (request === editSubmitRequest) editSaving.value = false
    }
  }

  function focusRestoreCancel() {
    nextTick(() => {
      restoreFormRef.value?.clearValidate?.()
      const button = restoreCancelButton.value?.$el || restoreCancelButton.value
      button?.focus?.()
    })
  }

  function clearRestoreSession() {
    restoreDialog.value = false
    restoreAssessment.value = null
    restorePolicy.value = null
    restoreForm.rationale = ''
    restoreSaving.value = false
    restoreReloading.value = false
    restoreConflict.value = false
  }

  function openRestore(assessment) {
    const policy = policyForAssessment(assessment)
    if (disposed || !assessment?.id || !policy?.id || policy.state !== 'active') return false
    restoreReloadRequest += 1
    restoreSubmitRequest += 1
    clearRestoreSession()
    restoreAssessment.value = assessment
    restorePolicy.value = policy
    restoreDialog.value = true
    return true
  }

  function closeRestore() {
    restoreReloadRequest += 1
    restoreSubmitRequest += 1
    clearRestoreSession()
  }

  async function reloadRestoreBaseline() {
    const assessment = restoreAssessment.value
    const policy = restorePolicy.value
    if (disposed || restoreReloading.value || restoreSaving.value || !assessment?.id || !policy?.id) {
      return { status: 'unavailable' }
    }
    const request = ++restoreReloadRequest
    restoreReloading.value = true
    try {
      const context = await fetchLatestContext(assessment, policy)
      if (request !== restoreReloadRequest || disposed) return { status: 'stale' }
      applyLatestContext(context)
      if (context.policy?.state !== 'active' || context.assessment?.current?.conclusion !== 'sensitive') {
        clearRestoreSession()
        onAlreadyRestored()
        return { status: 'already_restored', context }
      }
      restoreAssessment.value = context.assessment
      restorePolicy.value = context.policy
      restoreForm.rationale = ''
      restoreConflict.value = false
      onRestoreReloaded()
      focusRestoreCancel()
      return { status: 'reloaded', context }
    } catch (error) {
      if (request !== restoreReloadRequest || disposed) return { status: 'stale' }
      onLoadError(error)
      return { status: 'failed', error }
    } finally {
      if (request === restoreReloadRequest) restoreReloading.value = false
    }
  }

  async function submitRestore() {
    const policy = restorePolicy.value
    if (disposed || restoreSaving.value || restoreReloading.value || restoreConflict.value || !policy?.id || policy.state !== 'active') {
      return { status: 'unavailable' }
    }
    const request = ++restoreSubmitRequest
    restoreSaving.value = true
    const validation = restoreFormRef.value?.validate?.()
    const valid = await Promise.resolve(validation).catch(() => false)
    if (request !== restoreSubmitRequest || disposed) return { status: 'stale' }
    if (!valid) {
      restoreSaving.value = false
      return { status: 'invalid' }
    }

    try {
      await revokePolicy(policy.id, {
        version: Number(policy.version),
        rationale: restoreForm.rationale.trim()
      })
      if (request !== restoreSubmitRequest || disposed) return { status: 'stale' }
      clearRestoreSession()
      await refreshCollection({ background: true })
      await refreshGovernance()
      if (disposed) return { status: 'stale' }
      scheduleAutoRefresh({ reset: true })
      onRestored()
      return { status: 'restored' }
    } catch (error) {
      if (request !== restoreSubmitRequest || disposed) return { status: 'stale' }
      if (isResourceVersionConflict(error)) {
        restoreConflict.value = true
        onRestoreConflict()
        return { status: 'version_conflict', error }
      }
      onSubmitError(error)
      return { status: 'failed', error }
    } finally {
      if (request === restoreSubmitRequest) restoreSaving.value = false
    }
  }

  function dispose() {
    disposed = true
    editReloadRequest += 1
    editSubmitRequest += 1
    restoreReloadRequest += 1
    restoreSubmitRequest += 1
    editSaving.value = false
    editReloading.value = false
    restoreSaving.value = false
    restoreReloading.value = false
  }

  return {
    editDialog,
    editFormRef,
    editSaving,
    editReloading,
    editConflict,
    editAssessment,
    editForm,
    editRules,
    editEffects,
    openEdit,
    closeEdit,
    clearEditValidation,
    reloadEditBaseline,
    submitEdit,
    restoreDialog,
    restoreFormRef,
    restoreCancelButton,
    restoreSaving,
    restoreReloading,
    restoreConflict,
    restoreAssessment,
    restoreForm,
    restoreRules,
    openRestore,
    closeRestore,
    focusRestoreCancel,
    reloadRestoreBaseline,
    submitRestore,
    dispose
  }
}
