// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import StandardDefinitionSummary from '../src/components/StandardDefinitionSummary.vue'

const mocks = vi.hoisted(() => ({ auth: null, read: vi.fn(), open: vi.fn() }))
vi.mock('../src/store/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('../src/api/client', () => ({ default: { get: mocks.read } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
vi.mock('@common-ui', async () => ({
  ...(await import('../../../common-frontend/basic/src/authorization/consoleRouteAccess.js')),
  openConsoleRoute: mocks.open
}))

let app, root
afterEach(() => { app?.unmount(); root?.remove(); vi.clearAllMocks() })
const flush = async () => { await nextTick(); await Promise.resolve(); await nextTick() }

describe('Standard source link permissions in the rendered summary', () => {
  it('keeps permitted facts visible and reacts to complete entry permissions and their removal', async () => {
    mocks.auth = reactive({ contextType: 'tenant', permissions: ['standard.element.read'],
      hasPermission(permission) { return this.permissions.includes(permission) } })
    mocks.read.mockResolvedValue({ current_revision: { definition: 'Effective owner definition', status: 'published' } })
    root = document.createElement('div'); document.body.appendChild(root)
    app = createApp(StandardDefinitionSummary, { source: {
      source_module: 'standard', source_type: 'element', source_identity: '3', source_status: 'active'
    } })
    app.component('el-button', { setup: (_, { attrs, slots }) => () => h('button', attrs, slots.default?.()) })
    app.component('el-tag', { setup: (_, { slots }) => () => h('span', slots.default?.()) })
    app.mount(root); await flush()
    expect(root.textContent).toContain('Effective owner definition')
    expect(root.querySelector('button')).toBeNull()

    mocks.auth.permissions.push('standard.domain.read'); await flush()
    const previousButton = root.querySelector('button')
    expect(previousButton?.textContent).toBe('catalog.standardDefinition.open')
    previousButton.click()
    expect(mocks.open).toHaveBeenCalledExactlyOnceWith('/standard/elements/3')

    // The click guard must also reject a stale rendered button before Vue flushes.
    mocks.auth.permissions = ['standard.element.read']
    previousButton.click(); await flush()
    expect(mocks.open).toHaveBeenCalledTimes(1)
    expect(root.querySelector('button')).toBeNull()
    expect(root.textContent).toContain('Effective owner definition')
  })
})
