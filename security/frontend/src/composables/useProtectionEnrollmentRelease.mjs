import { computed, nextTick, reactive, ref } from 'vue'
import { createRequiredRule } from '../utils/foundationForm.mjs'
import {
  isNoSupportedFindingsReleaseUnavailable,
  isResourceVersionConflict,
  isZeroFindingDiscovery
} from '../utils/protectionEnrollment.mjs'

export function useProtectionEnrollmentRelease({
  getEnrollment,
  releaseEnrollment,
  replaceEnrollment,
  refreshCollection,
  scheduleAutoRefresh,
  t,
  onAlreadyReleasing = () => {},
  onNoSupportedFindingsUnavailable = () => {},
  onReloaded = () => {},
  onLoadError = () => {},
  onStarted = () => {},
  onVersionConflict = () => {},
  onSubmitError = () => {}
}) {
  const dialog = ref(false)
  const dialogRef = ref(null)
  const saving = ref(false)
  const reloading = ref(false)
  const conflict = ref(false)
  const enrollment = ref(null)
  const basis = ref('manual')
  const form = reactive({ reason: '' })

  let reloadRequest = 0
  let submitRequest = 0
  let disposed = false

  const rules = computed(() => ({
    reason: [createRequiredRule(t('security.common.requiredField', {
      name: t('security.enrollment.releaseReasonLabel')
    }), { trigger: 'blur', whitespace: true })]
  }))

  function focusDialogPrimary() {
    nextTick(() => {
      dialogRef.value?.focusPrimary?.()
    })
  }

  function clearSession() {
    dialog.value = false
    enrollment.value = null
    basis.value = 'manual'
    form.reason = ''
    conflict.value = false
    reloading.value = false
    saving.value = false
  }

  function open(row, releaseBasis = 'manual') {
    if (disposed || !row?.id || ['releasing', 'released'].includes(row.state)) return false
    reloadRequest += 1
    submitRequest += 1
    enrollment.value = row
    basis.value = releaseBasis === 'no_supported_findings' ? 'no_supported_findings' : 'manual'
    form.reason = ''
    conflict.value = false
    dialog.value = true
    return true
  }

  function close() {
    reloadRequest += 1
    submitRequest += 1
    clearSession()
  }

  async function reloadBaseline() {
    if (disposed || reloading.value || saving.value || !enrollment.value?.id) return { status: 'unavailable' }
    const request = ++reloadRequest
    const enrollmentID = enrollment.value.id
    reloading.value = true
    try {
      const latest = await getEnrollment(enrollmentID)
      if (request !== reloadRequest || disposed) return { status: 'stale' }
      replaceEnrollment(latest)
      if (['releasing', 'released'].includes(latest?.state)) {
        clearSession()
        await refreshCollection({ background: true })
        if (!disposed) onAlreadyReleasing()
        return { status: 'already_releasing', enrollment: latest }
      }
      if (basis.value === 'no_supported_findings' && !isZeroFindingDiscovery(latest)) {
        clearSession()
        await refreshCollection({ background: true })
        if (!disposed) onNoSupportedFindingsUnavailable()
        return { status: 'no_supported_findings_unavailable', enrollment: latest }
      }
      enrollment.value = latest
      form.reason = ''
      conflict.value = false
      onReloaded()
      focusDialogPrimary()
      return { status: 'reloaded', enrollment: latest }
    } catch (error) {
      if (request !== reloadRequest || disposed) return { status: 'stale' }
      onLoadError(error)
      return { status: 'failed', error }
    } finally {
      if (request === reloadRequest) reloading.value = false
    }
  }

  async function submit() {
    if (disposed || saving.value || reloading.value || conflict.value || !enrollment.value?.id) return { status: 'unavailable' }
    const request = ++submitRequest
    const source = enrollment.value
    const releaseBasis = basis.value
    saving.value = true
    const validation = dialogRef.value?.validate?.()
    const valid = await Promise.resolve(validation).catch(() => false)
    if (request !== submitRequest || disposed) return { status: 'stale' }
    if (!valid) {
      saving.value = false
      return { status: 'invalid' }
    }

    try {
      const released = await releaseEnrollment(source.id, {
        version: Number(source.version),
        basis: releaseBasis,
        reason: form.reason.trim()
      })
      if (request !== submitRequest || disposed) return { status: 'stale' }
      replaceEnrollment(released)
      clearSession()
      await refreshCollection()
      scheduleAutoRefresh({ reset: true })
      if (!disposed) onStarted()
      return { status: 'started', enrollment: released }
    } catch (error) {
      if (request !== submitRequest || disposed) return { status: 'stale' }
      if (isResourceVersionConflict(error)) {
        conflict.value = true
        onVersionConflict()
        return { status: 'version_conflict', error }
      }
      if (isNoSupportedFindingsReleaseUnavailable(error)) {
        clearSession()
        await refreshCollection({ background: true })
        if (!disposed) onNoSupportedFindingsUnavailable()
        return { status: 'no_supported_findings_unavailable', error }
      }
      onSubmitError(error)
      return { status: 'failed', error }
    } finally {
      if (request === submitRequest) saving.value = false
    }
  }

  function dispose() {
    disposed = true
    reloadRequest += 1
    submitRequest += 1
    saving.value = false
    reloading.value = false
  }

  return {
    dialog,
    dialogRef,
    saving,
    reloading,
    conflict,
    enrollment,
    basis,
    form,
    rules,
    open,
    close,
    reloadBaseline,
    submit,
    dispose
  }
}
