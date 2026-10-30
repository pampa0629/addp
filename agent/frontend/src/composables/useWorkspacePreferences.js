import { computed, ref, watch } from 'vue'

export const defaultPreferences = () => ({ replyDetail: 'standard', expandDetails: false, sendShortcut: 'ctrl-enter' })
export function normalizePreferences(value) {
  return {
    replyDetail: ['standard', 'concise', 'detailed'].includes(value?.replyDetail) ? value.replyDetail : 'standard',
    expandDetails: value?.expandDetails === true,
    sendShortcut: value?.sendShortcut === 'enter' ? 'enter' : 'ctrl-enter'
  }
}
export function preferenceStorageKey(userId, tenantId) {
  if (!/^[1-9]\d*$/.test(String(userId)) || !/^[1-9]\d*$/.test(String(tenantId))) return null
  return `addp:agent:preferences:v1:${userId}:${tenantId}`
}
export function useWorkspacePreferences(auth) {
  const preferences = ref(defaultPreferences())
  const key = computed(() => preferenceStorageKey(auth.user?.id, auth.authContext?.context?.tenant_id))
  watch(key, value => {
    preferences.value = defaultPreferences()
    if (!value) return
    try { preferences.value = normalizePreferences(JSON.parse(localStorage.getItem(value))) }
    catch { /* Invalid local presentation preferences have no authority. */ }
  }, { immediate: true })
  function save(value) {
    if (!key.value) return false
    const next = normalizePreferences(value)
    try { localStorage.setItem(key.value, JSON.stringify(next)) } catch { return false }
    preferences.value = next
    return true
  }
  return { preferences, save, scopeKey: key }
}
