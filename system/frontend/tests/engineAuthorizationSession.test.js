import { effectScope, nextTick, reactive, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { useEngineAuthorizationSession } from '../src/utils/engineAuthorizationSession'

const context = () => ({ principal: { id: '31', principal_type: 'user' }, context: { type: 'tenant', tenant_id: '1', tenant_membership_id: '8' },
  authorization: { authorization_version: '7' }, client: { client_id: 'addp-web' } })
function fixture() {
  const auth = reactive({ authContext: context(), authContextLoadPromise: null, permissions: ['read', 'grant'] })
  const engine = ref({ id: 2, lifecycle_state: 'active', connection_status: 'online' })
  const scope = effectScope()
  return { auth, engine, scope, ...scope.run(() => useEngineAuthorizationSession(auth, engine)) }
}
describe('engine authorization presentation boundary', () => {
  it('suspends without preserving usable permissions and resumes only the settled identity', async () => {
    const state = fixture(), key = state.authorizationKey.value
    state.auth.authContext = null; state.auth.permissions = []; state.auth.authContextLoadPromise = Promise.resolve()
    await nextTick()
    expect(state.authorizationReady.value).toBe(false)
    expect(state.authorizationKey.value).toBe(key)
    expect(state.auth.permissions).toEqual([])
    state.auth.authContext = { ...context(), token: { issued_at: 'new-time' } }; state.auth.permissions = ['grant', 'read']; state.auth.authContextLoadPromise = null
    await nextTick()
    expect(state.authorizationReady.value).toBe(true)
    expect(state.authorizationKey.value).toBe(key)
    state.scope.stop()
  })
  it.each(['principal', 'tenant', 'membership', 'client', 'version', 'permission', 'engine', 'availability'])('changes the boundary for a real %s change', async kind => {
    const state = fixture(), key = state.authorizationKey.value
    if (kind === 'principal') state.auth.authContext.principal.id = '33'
    if (kind === 'membership') state.auth.authContext.context.tenant_membership_id = '9'
    if (kind === 'tenant') state.auth.authContext.context.tenant_id = '2'
    if (kind === 'client') state.auth.authContext.client.client_id = 'other'
    if (kind === 'version') state.auth.authContext.authorization.authorization_version = '8'
    if (kind === 'permission') state.auth.permissions = ['read']
    if (kind === 'engine') state.engine.value.id = 3
    if (kind === 'availability') state.engine.value.connection_status = 'offline'
    await nextTick()
    expect(state.authorizationKey.value).not.toBe(key)
    state.scope.stop()
  })
  it('does not restore a draft after failed authentication or logout', async () => {
    const state = fixture()
    state.auth.authContext = null; state.auth.authContextLoadPromise = Promise.resolve()
    await nextTick()
    state.auth.authContextLoadPromise = null
    await nextTick()
    expect(state.authorizationReady.value).toBe(false)
    expect(state.authorizationKey.value).toBe('')
    state.scope.stop()
  })
})
