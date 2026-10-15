import { computed, ref, watch } from 'vue'

// Presentation identity only, never a credential or permission source. Retain
// the settled key while AuthContext is replaced, not its usable permissions.
export function useEngineAuthorizationSession(auth, engine) {
  const authorizationPending = computed(() => Boolean(auth.authContextLoadPromise))
  const authorizationReady = computed(() => Boolean(auth.authContext) && !authorizationPending.value)
  const authorizationKey = ref('')
  watch(() => {
    if (authorizationPending.value) return null
    const context = auth.authContext
    if (!context) return ''
    return JSON.stringify([
      engine.value?.id, engine.value?.lifecycle_state, engine.value?.connection_status,
      context.principal, context.context, context.client,
      context.authorization?.authorization_version, [...auth.permissions].sort()
    ])
  }, key => { if (key !== null) authorizationKey.value = key }, { immediate: true })
  return { authorizationPending, authorizationReady, authorizationKey }
}
