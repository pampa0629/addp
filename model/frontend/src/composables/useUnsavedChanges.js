import { computed, onBeforeUnmount, onMounted, ref, toValue } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { snapshotUnsavedState } from '../utils/modelDetailState'

export function useUnsavedChanges({ state, t, additionalDirty = false }) {
  const savedSnapshot = ref('')
  const ready = ref(false)
  const currentSnapshot = computed(() => snapshotUnsavedState(toValue(state)))
  const isDirty = computed(() => ready.value && (savedSnapshot.value !== currentSnapshot.value || toValue(additionalDirty)))

  const markSaved = () => {
    savedSnapshot.value = currentSnapshot.value
    ready.value = true
  }

  const confirmDiscardState = async dirty => {
    if (!dirty) return true
    try {
      await ElMessageBox.confirm(
        t('model.common.unsaved_confirm'),
        t('model.common.unsaved_title'),
        {
          type: 'warning',
          customClass: 'addp-message-box',
          confirmButtonText: t('model.common.leave'),
          cancelButtonText: t('model.common.continue_editing')
        }
      )
      return true
    } catch {
      return false
    }
  }

  const confirmDiscardChanges = () => confirmDiscardState(isDirty.value)

  const handleBeforeUnload = event => {
    if (!isDirty.value) return
    event.preventDefault()
    event.returnValue = ''
  }

  onBeforeRouteLeave(confirmDiscardChanges)
  onBeforeRouteUpdate((to, from) => {
    if (String(to.params.id || '') === String(from.params.id || '')) return true
    return confirmDiscardChanges()
  })

  onMounted(() => window.addEventListener('beforeunload', handleBeforeUnload))
  onBeforeUnmount(() => window.removeEventListener('beforeunload', handleBeforeUnload))

  return { isDirty, markSaved, confirmDiscardChanges, confirmDiscardState }
}
