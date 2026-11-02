import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import ElementPlus from 'element-plus'
import common from '../../../common-frontend/basic/src/i18n/zh-cn.json'
import agent from '../src/i18n/zh-cn.json'
import Login from '../src/views/Login.vue'

const { auth, navigate, query } = vi.hoisted(() => ({ auth: { login: vi.fn(), verifyMFA: vi.fn(), selectContext: vi.fn() }, navigate: vi.fn(), query: {} }))
vi.mock('../src/store/auth', () => ({ useAuthStore: () => auth }))
vi.mock('../src/utils/moduleNavigation', () => ({ navigateAgentRoute: navigate }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query }), useRouter: () => ({}) }))
const render = () => mount(Login, { global: { plugins: [ElementPlus, createI18n({ legacy: false, locale: 'zh-cn', messages: { 'zh-cn': { ...common, ...agent } } })] } })
async function credentials(wrapper) {
  await wrapper.findAll('input')[0].setValue('reader')
  await wrapper.findAll('input')[1].setValue('password')
  await wrapper.find('form').trigger('submit')
  await flushPromises()
}
describe('Agent shared login flow', () => {
  beforeEach(() => { vi.resetAllMocks(); delete query.redirect })
  it('waits for MFA and tenant context before navigating to the original conversation', async () => {
    query.redirect = '/sessions/61?view=summary#reply-4'
    auth.login.mockResolvedValue({ next_action: 'verify_mfa', mfa: { challenge_token: 'challenge' } })
    auth.verifyMFA.mockResolvedValue({ next_action: 'select_context', selection: { selection_ticket: 'selection', contexts: [{ type: 'tenant', tenant_membership_id: '4', tenant_id: '7', tenant_name: '验收租户' }] } })
    auth.selectContext.mockResolvedValue({ next_action: 'session_issued' })
    const wrapper = render()
    await credentials(wrapper)
    expect(navigate).not.toHaveBeenCalled()
    await wrapper.find('input').setValue('123456')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(auth.verifyMFA).toHaveBeenCalledWith('challenge', '123456')
    expect(navigate).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('验收租户')
    await wrapper.findAll('button').find(button => button.text() === common.auth.login.continue).trigger('click')
    await flushPromises()
    expect(auth.selectContext).toHaveBeenCalledWith('selection', expect.objectContaining({ tenant_membership_id: '4' }))
    expect(navigate).toHaveBeenCalledWith({}, '/sessions/61?view=summary#reply-4')
    wrapper.unmount()
  })
  it.each(['https://outside.example/sessions/61', '//outside.example', '/\\outside.example', ['/sessions/61', '/'], { path: '/sessions/61' }, '/login', '/LOGIN', '/login/', '/Login/?redirect=/sessions/61#reply', '/sessions/../LOGIN/'])('rejects unsafe, ambiguous or looping redirect %j', async redirect => {
    query.redirect = redirect
    auth.login.mockResolvedValue({ next_action: 'session_issued' })
    const wrapper = render()
    await credentials(wrapper)
    expect(navigate).toHaveBeenCalledWith({}, '/')
    wrapper.unmount()
  })
  it.each([undefined, '/'])('keeps the capability home when redirect is %j', async redirect => {
    query.redirect = redirect
    auth.login.mockResolvedValue({ next_action: 'session_issued' })
    const wrapper = render()
    await credentials(wrapper)
    expect(navigate).toHaveBeenCalledWith({}, '/')
    wrapper.unmount()
  })
})
