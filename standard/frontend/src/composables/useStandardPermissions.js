import { computed } from 'vue'
import { useAuthStore } from '../store/auth'
import { buildStandardPermission } from '../utils/standardPermissions'

export function useStandardPermissions(resource) {
  const authStore = useAuthStore()
  const has = (action) => computed(() => authStore.hasPermission(buildStandardPermission(resource, action)))

  return {
    canCreate: has('create'),
    canUpdate: has('update'),
    canDelete: has('delete'),
    canApprove: has('approve'),
    canPublish: has('publish'),
    canOffline: has('offline')
  }
}
