import { computed, nextTick, reactive, ref } from 'vue'
import { createRequiredRule } from '../utils/foundationForm.mjs'
import { isResourceVersionConflict } from '../utils/protectionEnrollment.mjs'

function snapshotExemption(exemption) {
  if (!exemption) return null
  return {
    ...exemption,
    current: exemption.current ? { ...exemption.current } : null
  }
}

export function useProtectionExemptionRevocation({
  getExemption,
  revokeExemption,
  replaceExemption,
  refreshExemptions,
  refreshCollection,
  scheduleAutoRefresh,
  t,
  onReloaded = () => {},
  onAlreadyInactive = () => {},
  onLoadError = () => {},
  onRevoked = () => {},
  onVersionConflict = () => {},
  onSubmitError = () => {}
}) {
  const dialog = ref(false)
  const formRef = ref(null)
  const cancelButton = ref(null)
  const saving = ref(false)
  const reloading = ref(false)
  const conflict = ref(false)
  const exemption = ref(null)
  const form = reactive({ rationale: '' })

  let reloadRequest = 0
  let submitRequest = 0
  let disposed = false

  const rules = computed(() => ({
    rationale: [createRequiredRule(t('security.common.requiredField', {
      name: t('security.exemption.revokeRationale')
    }), { trigger: 'blur', whitespace: true })]
  }))

  function clearSession() {
    dialog.value = false
    exemption.value = null
    form.rationale = ''
    saving.value = false
    reloading.value = false
    conflict.value = false
  }

  function open(source) {
    if (disposed || !source?.id || source.effective_state !== 'active') return false
    reloadRequest += 1
    submitRequest += 1
    clearSession()
    exemption.value = snapshotExemption(source)
    dialog.value = true
    return true
  }

  function close() {
    reloadRequest += 1
    submitRequest += 1
    clearSession()
  }

  function focusCancel() {
    nextTick(() => {
      formRef.value?.clearValidate?.()
      const button = cancelButton.value?.$el || cancelButton.value
      button?.focus?.()
    })
  }

  async function reloadBaseline() {
    const source = exemption.value
    if (disposed || reloading.value || saving.value || !source?.id) return { status: 'unavailable' }
    const request = ++reloadRequest
    reloading.value = true
    try {
      const latest = await getExemption(source.id)
      if (request !== reloadRequest || disposed) return { status: 'stale' }
      replaceExemption(latest)
      if (latest?.effective_state !== 'active') {
        clearSession()
        onAlreadyInactive()
        return { status: 'already_inactive', exemption: latest }
      }
      exemption.value = snapshotExemption(latest)
      form.rationale = ''
      conflict.value = false
      onReloaded()
      focusCancel()
      return { status: 'reloaded', exemption: latest }
    } catch (error) {
      if (request !== reloadRequest || disposed) return { status: 'stale' }
      onLoadError(error)
      return { status: 'failed', error }
    } finally {
      if (request === reloadRequest) reloading.value = false
    }
  }

  async function submit() {
    const source = exemption.value
    if (disposed || saving.value || reloading.value || conflict.value || !source?.id || source.effective_state !== 'active') {
      return { status: 'unavailable' }
    }
    const request = ++submitRequest
    saving.value = true
    const validation = formRef.value?.validate?.()
    const valid = await Promise.resolve(validation).catch(() => false)
    if (request !== submitRequest || disposed) return { status: 'stale' }
    if (!valid) {
      saving.value = false
      return { status: 'invalid' }
    }

    try {
      await revokeExemption(source.id, {
        version: Number(source.version),
        rationale: form.rationale.trim()
      })
      if (request !== submitRequest || disposed) return { status: 'stale' }
      clearSession()
      await Promise.all([
        refreshExemptions(),
        refreshCollection({ background: true })
      ])
      if (disposed) return { status: 'stale' }
      scheduleAutoRefresh({ reset: true })
      onRevoked()
      return { status: 'revoked' }
    } catch (error) {
      if (request !== submitRequest || disposed) return { status: 'stale' }
      if (isResourceVersionConflict(error)) {
        conflict.value = true
        onVersionConflict()
        return { status: 'version_conflict', error }
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
    formRef,
    cancelButton,
    saving,
    reloading,
    conflict,
    exemption,
    form,
    rules,
    open,
    close,
    focusCancel,
    reloadBaseline,
    submit,
    dispose
  }
}
