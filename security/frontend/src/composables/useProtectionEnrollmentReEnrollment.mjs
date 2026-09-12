import { nextTick, ref } from 'vue'
import {
  isProtectionEnrollmentAlreadyActive,
  isResourceVersionConflict
} from '../utils/protectionEnrollment.mjs'

export function useProtectionEnrollmentReEnrollment({
  getEnrollment,
  createReEnrollment,
  replaceEnrollment,
  showCurrentCollection,
  refreshCollection,
  scheduleAutoRefresh,
  onUnavailable = () => {},
  onReloaded = () => {},
  onLoadError = () => {},
  onCreated = () => {},
  onVersionConflict = () => {},
  onAlreadyActive = () => {},
  onSubmitError = () => {}
}) {
  const dialog = ref(false)
  const dialogRef = ref(null)
  const saving = ref(false)
  const reloading = ref(false)
  const conflict = ref(false)
  const source = ref(null)

  let reloadRequest = 0
  let submitRequest = 0
  let disposed = false

  function focusDialogPrimary() {
    nextTick(() => {
      dialogRef.value?.focusPrimary?.()
    })
  }

  function clearSession() {
    dialog.value = false
    source.value = null
    saving.value = false
    reloading.value = false
    conflict.value = false
  }

  function open(row) {
    if (disposed || !row?.id || row.state !== 'released') return false
    reloadRequest += 1
    submitRequest += 1
    source.value = row
    saving.value = false
    reloading.value = false
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
    if (disposed || reloading.value || saving.value || !source.value?.id) return { status: 'unavailable' }
    const request = ++reloadRequest
    const enrollmentID = source.value.id
    reloading.value = true
    try {
      const latest = await getEnrollment(enrollmentID)
      if (request !== reloadRequest || disposed) return { status: 'stale' }
      replaceEnrollment(latest)
      if (latest?.state !== 'released') {
        clearSession()
        await refreshCollection({ background: true })
        if (!disposed) onUnavailable()
        return { status: 'unavailable_state', enrollment: latest }
      }
      source.value = latest
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
    if (disposed || saving.value || reloading.value || conflict.value || !source.value?.id) {
      return { status: 'unavailable' }
    }
    const request = ++submitRequest
    const releasedEnrollment = source.value
    saving.value = true
    try {
      const result = await createReEnrollment(releasedEnrollment.id, {
        version: Number(releasedEnrollment.version)
      })
      if (request !== submitRequest || disposed) return { status: 'stale' }
      const sourceVersion = Number(result?.source_enrollment_version)
      if (sourceVersion > 0) replaceEnrollment({ ...releasedEnrollment, version: sourceVersion })
      clearSession()
      showCurrentCollection(result?.enrollment)
      await refreshCollection()
      if (disposed) return { status: 'stale' }
      scheduleAutoRefresh({ reset: true })
      onCreated()
      return { status: 'created', result }
    } catch (error) {
      if (request !== submitRequest || disposed) return { status: 'stale' }
      if (isResourceVersionConflict(error)) {
        conflict.value = true
        onVersionConflict()
        return { status: 'version_conflict', error }
      }
      if (isProtectionEnrollmentAlreadyActive(error)) {
        clearSession()
        showCurrentCollection()
        await refreshCollection({ background: true })
        if (disposed) return { status: 'stale' }
        scheduleAutoRefresh({ reset: true })
        onAlreadyActive()
        return { status: 'already_active', error }
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
    source,
    open,
    close,
    reloadBaseline,
    submit,
    dispose
  }
}
