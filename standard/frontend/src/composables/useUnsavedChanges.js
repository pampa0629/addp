import { computed, onBeforeUnmount, onMounted, ref, toValue } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { ElMessageBox } from 'element-plus'

export const snapshotUnsavedState = (state) => JSON.stringify(state ?? null)

export function useUnsavedChanges({ state, t }) {
  const savedSnapshot = ref('')
  const ready = ref(false)
  const currentSnapshot = computed(() => snapshotUnsavedState(toValue(state)))
  const isDirty = computed(() => ready.value && savedSnapshot.value !== currentSnapshot.value)

  const markSaved = () => {
    savedSnapshot.value = currentSnapshot.value
    ready.value = true
  }

  const confirmUnsavedRouteChange = async () => {
    if (!isDirty.value) return true
    try {
      await ElMessageBox.confirm(
        t('standard.common.unsavedConfirm'),
        t('standard.common.unsavedTitle'),
        {
          type: 'warning',
          customClass: 'addp-message-box',
          confirmButtonText: t('standard.common.leave'),
          cancelButtonText: t('standard.common.continueEditing')
        }
      )
      return true
    } catch {
      return false
    }
  }

  const handleBeforeUnload = (event) => {
    if (!isDirty.value) return
    event.preventDefault()
    event.returnValue = ''
  }

  onBeforeRouteLeave(confirmUnsavedRouteChange)
  onBeforeRouteUpdate((to, from) => {
    if (String(to.params.id || '') === String(from.params.id || '')) return true
    return confirmUnsavedRouteChange()
  })

  onMounted(() => window.addEventListener('beforeunload', handleBeforeUnload))
  onBeforeUnmount(() => window.removeEventListener('beforeunload', handleBeforeUnload))

  return { isDirty, markSaved }
}
