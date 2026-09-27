import { describe, expect, it, vi } from 'vitest'
import { createAuthGuard } from '@common-ui'

function setup(permissions) {
  const router = { hasRoute: () => false, addRoute: vi.fn() }
  const store = {
    initializeSession: vi.fn(async () => {}),
    sessionStatus: 'authenticated',
    isAuthenticated: true,
    contextType: 'tenant',
    permissions
  }
  const guard = createAuthGuard(store, { router, moduleName: 'Service' })
  return { guard, router }
}

describe('module page authorization', () => {
  it('shows a clear denied page for a deep link without read permission', async () => {
    const { guard, router } = setup(['service.external_registration.read'])
    const next = vi.fn()
    await guard({ path: '/query-services', name: 'QueryServiceList', matched: [{ meta: { requiresAuth: true } }] }, null, next)
    expect(router.addRoute).toHaveBeenCalledWith(expect.objectContaining({ name: 'AccessDenied' }))
    expect(next).toHaveBeenCalledWith({ name: 'AccessDenied', replace: true })
  })

  it('lets an authorized page through without granting its create action', async () => {
    const { guard } = setup(['service.definition.read'])
    const next = vi.fn()
    await guard({ path: '/query-services', name: 'QueryServiceList', matched: [{ meta: { requiresAuth: true } }] }, null, next)
    expect(next).toHaveBeenCalledWith()
    next.mockClear()
    await guard({ path: '/query-services/create', name: 'QueryServiceCreate', matched: [{ meta: { requiresAuth: true } }] }, null, next)
    expect(next).toHaveBeenCalledWith({ name: 'AccessDenied', replace: true })
  })

  it('applies the same page rules to Portal direct links', async () => {
    const router = { hasRoute: () => false, addRoute: vi.fn() }
    const store = {
      initializeSession: vi.fn(async () => {}),
      sessionStatus: 'authenticated', isAuthenticated: true,
      contextType: 'tenant', permissions: ['asset.entry.read']
    }
    const guard = createAuthGuard(store, { router, moduleName: 'Portal' })
    const next = vi.fn()
    await guard({ path: '/portal/assets/8', name: 'AssetDetail', matched: [{ meta: { requiresAuth: true } }] }, null, next)
    expect(next).toHaveBeenCalledWith()
    next.mockClear()
    await guard({ path: '/portal/my/applications', name: 'MyApplications', matched: [{ meta: { requiresAuth: true } }] }, null, next)
    expect(next).toHaveBeenCalledWith({ name: 'AccessDenied', replace: true })
  })
})
