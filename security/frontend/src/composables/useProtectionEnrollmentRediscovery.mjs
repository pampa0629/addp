import { nextTick, ref } from 'vue'
import {
  discoveryRefreshMarker,
  isDiscoveryExecutionInProgress,
  isResourceVersionConflict
} from '../utils/protectionEnrollment.mjs'

const REDISCOVERABLE_STATES = new Set(['enrolling', 'active'])

export function useProtectionEnrollmentRediscovery({
  getEnrollment,
  createDiscoveryExecution,
  replaceEnrollment,
  watchDiscovery,
  refreshCollection,
  scheduleAutoRefresh,
  openExecution,
  onUnavailable = () => {},
  onReloaded = () => {},
  onLoadError = () => {},
  onCreated = () => {},
  onVersionConflict = () => {},
  onExecutionInProgress = () => {},
  onSubmitError = () => {}
}) {
  const dialog = ref(false)
  const dialogRef = ref(null)
  const saving = ref(false)
  const reloading = ref(false)
  const conflict = ref(false)
  const enrollment = ref(null)

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
    enrollment.value = null
    saving.value = false
    reloading.value = false
    conflict.value = false
  }

  function open(row) {
    if (disposed || !row?.id || !REDISCOVERABLE_STATES.has(row.state)) return false
    reloadRequest += 1
    submitRequest += 1
    enrollment.value = row
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
    if (disposed || reloading.value || saving.value || !enrollment.value?.id) return { status: 'unavailable' }
    const request = ++reloadRequest
    const enrollmentID = enrollment.value.id
    reloading.value = true
    try {
      const latest = await getEnrollment(enrollmentID)
      if (request !== reloadRequest || disposed) return { status: 'stale' }
      replaceEnrollment(latest)
      if (!REDISCOVERABLE_STATES.has(latest?.state)) {
        clearSession()
        await refreshCollection({ background: true })
        if (!disposed) onUnavailable()
        return { status: 'unavailable_state', enrollment: latest }
      }
      enrollment.value = latest
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
    if (disposed || saving.value || reloading.value || conflict.value || !enrollment.value?.id) {
      return { status: 'unavailable' }
    }
    const request = ++submitRequest
    const source = enrollment.value
    const baselineMarker = discoveryRefreshMarker(source)
    saving.value = true
    try {
      const execution = await createDiscoveryExecution(source.id, { version: Number(source.version) })
      if (request !== submitRequest || disposed) return { status: 'stale' }
      const enrollmentVersion = Number(execution?.enrollment_version)
      if (enrollmentVersion > 0) replaceEnrollment({ ...source, version: enrollmentVersion })
      watchDiscovery(source.id, baselineMarker)
      clearSession()
      await refreshCollection({ background: true, syncFindings: true })
      if (disposed) return { status: 'stale' }
      scheduleAutoRefresh({ reset: true })
      onCreated()
      if (execution?.execution_id) {
        try {
          await openExecution(execution.execution_id)
        } catch (error) {
          if (disposed) return { status: 'stale' }
          onSubmitError(error)
          return { status: 'monitor_open_failed', execution, error }
        }
      }
      return { status: 'created', execution }
    } catch (error) {
      if (request !== submitRequest || disposed) return { status: 'stale' }
      if (isResourceVersionConflict(error)) {
        conflict.value = true
        onVersionConflict()
        return { status: 'version_conflict', error }
      }
      if (isDiscoveryExecutionInProgress(error)) {
        watchDiscovery(source.id, baselineMarker)
        clearSession()
        await refreshCollection({ background: true })
        if (disposed) return { status: 'stale' }
        scheduleAutoRefresh({ reset: true })
        onExecutionInProgress()
        return { status: 'execution_in_progress', error }
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
    open,
    close,
    reloadBaseline,
    submit,
    dispose
  }
}
