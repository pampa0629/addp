import { computed, onScopeDispose, ref, shallowRef, watch } from 'vue'
import { taskAPI } from '@/api/tasks'
import { projectTransferConfigCheckReport } from '@common-ui'

export const taskConfigCheckPermissions = ['transfer.task.create', 'meta.catalog.read', 'system.engine.read']

// Reports belong to the exact submitted request and current user context.
// This check never creates or starts a task; saving keeps its existing behavior.
export function useTaskConfigCheck(request, authStore) {
  const allowed = computed(() => authStore.authContext?.principal?.type === 'user' && authStore.contextType === 'tenant' &&
    taskConfigCheckPermissions.every(permission => authStore.hasPermission(permission)))
  const report = shallowRef(null)
  const error = ref('')
  const checking = ref(false)
  let generation = 0
  const binding = computed(() => JSON.stringify([
    request(), allowed.value, authStore.authContext?.principal, authStore.authContext?.context
  ]))

  function invalidate() {
    generation += 1
    report.value = null
    error.value = ''
    checking.value = false
  }
  watch(binding, invalidate, { flush: 'sync' })
  onScopeDispose(invalidate)

  async function check() {
    if (!allowed.value || checking.value) return
    const submitted = JSON.parse(JSON.stringify(request()))
    const current = ++generation
    report.value = null
    error.value = ''
    checking.value = true
    try {
      const result = await taskAPI.checkConfig(submitted)
      if (current !== generation) return
      const projection = projectTransferConfigCheckReport(result)
      if (!projection) {
        error.value = 'invalidResponse'
        return
      }
      report.value = projection
    } catch (failure) {
      if (current !== generation) return
      const status = failure.response?.status
      error.value = [400, 401, 403, 502].includes(status) ? `http${status}` : 'requestFailed'
    } finally {
      if (current === generation) checking.value = false
    }
  }

  return { allowed, report, error, checking, check }
}
