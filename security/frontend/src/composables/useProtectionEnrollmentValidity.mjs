import { reactive, ref } from 'vue'
import { isResourceVersionConflict } from '../utils/protectionEnrollment.mjs'

export function protectionValidityPayload(form) {
  if (form.validity_mode === 'permanent') return { validity_mode: 'permanent', expires_at: null }
  const deadline = new Date(form.expires_at)
  if (form.validity_mode !== 'until' || !form.expires_at || !Number.isFinite(deadline.getTime()) || deadline.getTime() <= Date.now()) return null
  return { validity_mode: 'until', expires_at: deadline.toISOString() }
}

export function useProtectionEnrollmentValidity({ getEnrollment, updateValidity, replaceEnrollment, onSaved, onInvalid, onError, scheduleAutoRefresh }) {
  const dialog = ref(false)
  const saving = ref(false)
  const reloading = ref(false)
  const conflict = ref(false)
  const enrollment = ref(null)
  const form = reactive({ validity_mode: 'permanent', expires_at: null })
  let revision = 0
  let disposed = false
  function assign(row) {
    enrollment.value = row
    form.validity_mode = row.validity_mode
    form.expires_at = row.expires_at ? new Date(row.expires_at) : null
    conflict.value = false
  }
  function open(row) {
    if (disposed || saving.value || !row?.id || ['released', 'releasing'].includes(row.state)) return false
    revision += 1
    assign(row)
    dialog.value = true
    return true
  }
  function close(value = false) {
    if (saving.value || reloading.value) return
    revision += 1
    dialog.value = value
  }
  async function reload() {
    if (disposed || saving.value || reloading.value || !dialog.value) return
    const current = ++revision
    reloading.value = true
    try {
      const latest = await getEnrollment(enrollment.value.id)
      if (disposed || current !== revision) return
      replaceEnrollment(latest)
      if (['released', 'releasing'].includes(latest.state)) { dialog.value = false; return }
      assign(latest)
    } catch (error) { if (!disposed && current === revision) onError(error) }
    finally { if (current === revision) reloading.value = false }
  }
  async function submit() {
    if (disposed || saving.value || reloading.value || conflict.value || !dialog.value) return
    const payload = protectionValidityPayload(form)
    if (!payload) { onInvalid(); return }
    const current = ++revision
    saving.value = true
    try {
      const latest = await updateValidity(enrollment.value.id, { version: enrollment.value.version, ...payload })
      if (disposed || current !== revision) return
      replaceEnrollment(latest)
      dialog.value = false
      onSaved()
      scheduleAutoRefresh({ reset: true })
    } catch (error) {
      if (disposed || current !== revision) return
      if (isResourceVersionConflict(error)) conflict.value = true
      else onError(error)
    } finally { if (current === revision) saving.value = false }
  }
  function dispose() { disposed = true; revision += 1 }
  return { dialog, saving, reloading, conflict, enrollment, form, open, close, reload, submit, dispose }
}
