import { computed, onBeforeUnmount, ref } from 'vue'
import { isActiveExecution } from '../utils/executionPresentation'

export function useExecutionTracking(api) {
  const execution = ref(null)
  const refreshFailed = ref(false)
  const refreshing = ref(false)
  const active = computed(() => isActiveExecution(execution.value))
  let generation = 0
  let timer
  let request
  let recordId

  function stop() {
    generation++
    clearTimeout(timer)
    request?.abort()
    request = null
    refreshing.value = false
  }
  function clear() {
    stop()
    recordId = null
    execution.value = null
    refreshFailed.value = false
  }
  async function refresh() {
    if (!recordId || refreshing.value) return
    clearTimeout(timer)
    const currentGeneration = generation
    const controller = new AbortController()
    request = controller
    refreshing.value = true
    try {
      const next = await api.getExecution(recordId, { signal: controller.signal })
      if (currentGeneration !== generation) return
      if (!next?.status || next.execution_id !== execution.value?.execution_id) throw new Error('Unexpected execution response')
      execution.value = next
      refreshFailed.value = false
      if (isActiveExecution(next)) timer = setTimeout(refresh, 2000)
    } catch (error) {
      if (currentGeneration === generation && !controller.signal.aborted) refreshFailed.value = true
    } finally {
      if (currentGeneration === generation) refreshing.value = false
    }
  }
  function start(result) {
    clear()
    execution.value = result
    recordId = result.id
    if (!recordId) { refreshFailed.value = true; return }
    void refresh()
  }
  onBeforeUnmount(stop)
  return { execution, active, refreshFailed, refreshing, start, clear, refresh }
}
