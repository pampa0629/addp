import { computed, ref } from 'vue'
import { cloneDAGValue, createDAGHistoryStore } from '../utils/editing.js'

export function useDAGHistory({ capture, restore, limit = 50, mergeWindow = 400 }) {
  const store = createDAGHistoryStore({ limit, mergeWindow })
  const revision = ref(0)
  const isRestoring = ref(false)

  const canUndo = computed(() => {
    revision.value
    return store.canUndo()
  })
  const canRedo = computed(() => {
    revision.value
    return store.canRedo()
  })

  function reset(snapshot = capture()) {
    store.reset(snapshot)
    revision.value += 1
  }

  function record({ mergeKey = null } = {}) {
    if (isRestoring.value) return false
    const changed = store.record(capture(), { mergeKey })
    if (changed) revision.value += 1
    return changed
  }

  function undo() {
    return restoreFromHistory(store.undo())
  }

  function redo() {
    return restoreFromHistory(store.redo())
  }

  function restoreFromHistory(snapshot) {
    if (snapshot === null) return false
    isRestoring.value = true
    try {
      restore(cloneDAGValue(snapshot))
      revision.value += 1
      return true
    } finally {
      isRestoring.value = false
    }
  }

  return {
    canUndo,
    canRedo,
    isRestoring,
    reset,
    record,
    undo,
    redo
  }
}
