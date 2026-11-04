import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import ElementPlus from 'element-plus'
import common from '../../../common-frontend/basic/src/i18n/zh-cn.json'

const { auth, router, query } = vi.hoisted(() => ({
  auth: { login: vi.fn(), verifyMFA: vi.fn(), selectContext: vi.fn() },
  router: { replace: vi.fn() },
  query: {},
}))
vi.mock('vue-router', () => ({ useRoute: () => ({ query }), useRouter: () => router }))
vi.mock('../src/store/auth', () => ({ useAuthStore: () => auth }))
vi.mock('../src/utils/moduleNavigation', () => ({ navigateAgentRoute: (targetRouter, target) => targetRouter.replace(target) }))

// Load each real module adapter; only its Store boundary is replaced.
const pages = import.meta.glob('../../../*/frontend/src/views/Login.vue')
const translations = import.meta.glob('../../../*/frontend/src/i18n/zh-cn.json', { eager: true, import: 'default' })
const moduleName = path => new URL(path, import.meta.url).pathname.split('/frontend/')[0].split('/').at(-1)
const moduleMessages = Object.fromEntries(Object.entries(translations).map(([path, messages]) => [moduleName(path), messages]))
const defaultRoutes = {
  catalog: '/entries', inference: '/settings/models', meta: '/scan',
  orchestrator: '/orchestrations', portal: '/portal/', workbench: '/applications',
}

beforeEach(() => {
  vi.resetAllMocks()
  delete query.redirect
})

async function submitCredentials(wrapper) {
  await wrapper.find('input[autocomplete=username]').setValue('fixture-user')
  await wrapper.find('input[autocomplete=current-password]').setValue('fixture-password')
  await wrapper.find('form').trigger('submit')
  await flushPromises()
}

for (const [path, load] of Object.entries(pages)) {
  const module = moduleName(path)
  vi.doMock(new URL(`../../../${module}/frontend/src/store/auth.js`, import.meta.url).pathname, () => ({ useAuthStore: () => auth }))
  const Login = (await load()).default
  const messages = { ...common, ...moduleMessages[module] }
  const render = () => mount(Login, { global: { plugins: [ElementPlus, createI18n({ legacy: false, locale: 'zh-cn', messages: { 'zh-cn': messages } })] } })

  describe(`${module} shared login adapter`, () => {
    it('waits for MFA and context selection before restoring the complete original route', async () => {
      query.redirect = '/fixture/resource?filter=active#details'
      auth.login.mockResolvedValue({ next_action: 'verify_mfa', mfa: { challenge_token: 'challenge' } })
      const tenant = { type: 'tenant', tenant_membership_id: 'membership', tenant_id: 'tenant', tenant_name: 'Fixture Tenant' }
      auth.verifyMFA.mockResolvedValue({ next_action: 'select_context', selection: { selection_ticket: 'selection', contexts: [tenant] } })
      auth.selectContext.mockResolvedValue({ next_action: 'session_issued' })
      const wrapper = render()
      try {
        await submitCredentials(wrapper)
        expect(auth.login).toHaveBeenCalledWith('fixture-user', 'fixture-password')
        expect(router.replace).not.toHaveBeenCalled()
        await wrapper.find('input[autocomplete=one-time-code]').setValue('123456')
        await wrapper.find('form').trigger('submit')
        await flushPromises()
        expect(auth.verifyMFA).toHaveBeenCalledWith('challenge', '123456')
        expect(router.replace).not.toHaveBeenCalled()
        await wrapper.findAll('button').find(button => button.text() === common.auth.login.continue).trigger('click')
        await flushPromises()
        expect(auth.selectContext).toHaveBeenCalledWith('selection', tenant)
        expect(router.replace).toHaveBeenCalledExactlyOnceWith('/fixture/resource?filter=active#details')
      } finally { wrapper.unmount() }
    })

    it('rejects external redirects and keeps the module default destination', async () => {
      query.redirect = '//outside.example/path'
      auth.login.mockResolvedValue({ next_action: 'session_issued' })
      const wrapper = render()
      try {
        await submitCredentials(wrapper)
        expect(router.replace).toHaveBeenCalledExactlyOnceWith(defaultRoutes[module] || '/')
      } finally { wrapper.unmount() }
    })
  })
}
