import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ElementPlus from 'element-plus'
import A2UISurface from '../src/a2ui/A2UISurface.vue'
import SharedReport from '../../basic/src/components/TransferConfigCheckReport.vue'
import zh from '../../basic/src/i18n/zh-cn.json'
import en from '../../basic/src/i18n/en.json'

const report = status => ({
  status, observed_at: '2026-10-12T00:00:00Z', checked: ['request_shape'],
  not_checked: ['business_rows'], runtime_checks_required: ['per_value_encoding'],
  diagnostics: [{ status, code: 'source_type_unknown', check: 'source_assignment', config_path: '/config/transforms/0/fields/0', source_field: 'members', target_field: 'members', physical_type: 'TEXT[]' }]
})
const operations = value => [
  { version: 'v0.9', createSurface: { surfaceId: 'surface:formal-check', catalogId: 'addp.catalog/v1' } },
  { version: 'v0.9', updateComponents: { surfaceId: 'surface:formal-check', components: [{ id: 'root', component: 'TransferConfigCheckReport', report: value }] } }
]
const i18n = () => createI18n({ legacy: false, locale: 'zh-cn', messages: { 'zh-cn': zh, en } })
// ElTag's mounted hook requires the real Transition/BaseTransition subtree.
// VTU's default Transition stub has no child component; keep real widgets here.
const globals = locale => ({ plugins: [ElementPlus, locale], stubs: { transition: false } })

describe('fixed formal configuration check report', () => {
  it.each(['supported', 'unsupported', 'unknown'])('renders owner %s without any interaction or write action', async status => {
    const locale = i18n()
    const wrapper = mount(A2UISurface, { props: { operations: operations(report(status)) }, global: globals(locale) })
    await vi.waitFor(() => expect(wrapper.findComponent(SharedReport).exists()).toBe(true))
    expect(wrapper.get('[data-testid="task-config-check-report"]').text()).toContain(zh.transferConfigCheck.status[status])
    expect(wrapper.text()).toContain('业务行数据')
    expect(wrapper.text()).toContain('逐值编码')
    expect(wrapper.findAll('button')).toHaveLength(0)
    expect(wrapper.emitted('action')).toBeUndefined()
    locale.global.locale.value = 'en'
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain(en.transferConfigCheck.status[status])
    expect(wrapper.text()).toContain('Runtime checks still required')
    wrapper.unmount()
  })

  it('preserves every diagnostic as escaped text and keeps details collapsible', async () => {
    const value = report('unknown')
    value.diagnostics[0].source_field = '<img src=x onerror=alert(1)>'
    const wrapper = mount(SharedReport, { props: { report: value }, global: globals(i18n()) })
    expect(wrapper.find('img').exists()).toBe(false)
    await vi.waitFor(() => expect(wrapper.text()).toContain('<img src=x onerror=alert(1)>'))
    expect(wrapper.findAll('details')).toHaveLength(4)
    expect(wrapper.findAll('details').every(detail => detail.attributes('open') === undefined)).toBe(true)
    wrapper.unmount()
  })

  it.each([
    value => ({ ...value, principal_id: 'private' }),
    value => ({ ...value, binding_hash: 'private' }),
    value => ({ ...value, url: 'https://untrusted.example' }),
    value => ({ ...value, action: 'create' }),
    value => ({ ...value, status: 'passed' }),
    value => ({ ...value, diagnostics: Array(513).fill(value.diagnostics[0]) })
  ])('rejects identity, actions and invalid or oversized reports %#', async mutate => {
    const wrapper = mount(A2UISurface, { props: { operations: operations(mutate(report('unknown'))) }, global: globals(i18n()) })
    await vi.waitFor(() => expect(wrapper.emitted('error')).toHaveLength(1))
    expect(wrapper.findComponent(SharedReport).exists()).toBe(false)
    expect(wrapper.emitted('action')).toBeUndefined()
    wrapper.unmount()
  })
})
